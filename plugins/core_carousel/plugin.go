// Package core_carousel shows carousels at positions of the template. A
// carousel combines news (pages published with the option "as news"), own
// slides and the latest items of external RSS/Atom feeds (fetched in the
// background with the SSRF protection of netguard).
//
// Configuring carousels needs core.carousel.manage, publishing news
// core.carousel.publish. News are only shown to people who may view the
// page.
package core_carousel

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	nethtml "golang.org/x/net/html"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// Plugin is the carousel plugin.
type Plugin struct{ host *sdk.Host }

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Carousel is the configuration of a carousel.
type Carousel struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Position  string   `json:"position"`
	Audience  string   `json:"audience"` // all | guests | users
	Autoplay  int      `json:"autoplay"` // seconds, 0 = off
	MaxSlides int      `json:"max_slides"`
	Feeds     []string `json:"feeds"`
	FeedItems int      `json:"feed_items"`
	Enabled   bool     `json:"enabled"`
	Order     int      `json:"order"`
}

// Slide is one slide as shown.
type Slide struct {
	ID        int64      `json:"id,omitempty"`
	Kind      string     `json:"kind"` // manual | news | feed
	ElementID *int64     `json:"element_id,omitempty"`
	Title     string     `json:"title"`
	Text      string     `json:"text"`
	Link      string     `json:"link"`
	Image     string     `json:"image"`
	ImageAlt  string     `json:"image_alt"`
	StartsAt  *time.Time `json:"starts_at,omitempty"`
	EndsAt    *time.Time `json:"ends_at,omitempty"`
	Date      *time.Time `json:"date,omitempty"`   // news: published, feed: item date
	Source    string     `json:"source,omitempty"` // feed: host of the feed
}

const carCols = `id, name, position, audience, autoplay, max_slides, feeds, feed_items, enabled, position_order`

func scanCarousel(r pgx.CollectableRow) (Carousel, error) {
	var c Carousel
	err := r.Scan(&c.ID, &c.Name, &c.Position, &c.Audience, &c.Autoplay, &c.MaxSlides, &c.Feeds, &c.FeedItems, &c.Enabled, &c.Order)
	return c, err
}

func manage(ctx context.Context) error { return perm.Require(ctx, "core.carousel.manage") }

var posRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)

func (c *Carousel) validate() error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || utf8.RuneCountInString(c.Name) > 100 {
		return apperr.BadRequest("a name (max. 100 characters) is required")
	}
	if !posRe.MatchString(c.Position) {
		return apperr.BadRequest("invalid position")
	}
	switch c.Audience {
	case "all", "guests", "users":
	case "":
		c.Audience = "all"
	default:
		return apperr.BadRequest("audience must be all, guests or users")
	}
	if c.Autoplay != 0 && (c.Autoplay < 3 || c.Autoplay > 60) {
		return apperr.BadRequest("automatic rotation: 3 to 60 seconds (0 = off)")
	}
	if c.MaxSlides <= 0 || c.MaxSlides > 30 {
		c.MaxSlides = 10
	}
	if c.FeedItems <= 0 || c.FeedItems > 20 {
		c.FeedItems = 5
	}
	feeds := []string{}
	for _, f := range c.Feeds {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		u, err := url.Parse(f)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			return apperr.BadRequest("feed %q: use an http(s) address", f)
		}
		feeds = append(feeds, u.String())
	}
	if len(feeds) > 10 {
		return apperr.BadRequest("at most 10 feeds per carousel")
	}
	c.Feeds = feeds
	return nil
}

func (s *Slide) validate() error {
	s.Title, s.Text, s.Link = strings.TrimSpace(s.Title), strings.TrimSpace(s.Text), strings.TrimSpace(s.Link)
	if s.Title == "" || utf8.RuneCountInString(s.Title) > 200 || utf8.RuneCountInString(s.Text) > 600 {
		return apperr.BadRequest("title (max. 200) and text (max. 600 characters) are required")
	}
	if s.Link != "" && !safeLink(s.Link) {
		return apperr.BadRequest("the link must be a path on this site or an http(s) address")
	}
	if s.Image != "" && !safeLink(s.Image) {
		return apperr.BadRequest("invalid image address")
	}
	if s.Image != "" && strings.TrimSpace(s.ImageAlt) == "" {
		return apperr.BadRequest("an image needs an alternative text")
	}
	if s.StartsAt != nil && s.EndsAt != nil && s.EndsAt.Before(*s.StartsAt) {
		return apperr.BadRequest("the end is before the start")
	}
	return nil
}

func safeLink(s string) bool {
	if strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//") {
		return true
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// Setup implements sdk.SetupPlugin.
func (p *Plugin) Setup(host *sdk.Host) error {
	p.host = host
	host.Content.RegisterPublishOption(host.Manifest.ID, publishOption{p})
	host.RegisterWorker("feeds", 10*time.Minute, p.fetchAll)

	// ---------------------------------------------------------------- public
	// GET /position/{position}?lang= – carousels of a position with the slides the caller may see
	host.Handle("GET", "/position/{position}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return p.atPosition(r.Context(), r.PathValue("position"), r.URL.Query().Get("lang"))
	})
	// GET /targets – carousels news can be published to (for the publish dialog)
	host.Handle("GET", "/targets", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		if !perm.Has(ctx, "core.carousel.publish") && !perm.Has(ctx, "core.carousel.manage") {
			return []any{}, nil
		}
		rows, err := host.DB.Query(ctx, `SELECT `+carCols+` FROM plugin_carousels WHERE enabled ORDER BY position, position_order, id`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, scanCarousel)
	})

	// ---------------------------------------------------------------- administration
	host.Handle("GET", "/carousels", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := manage(r.Context()); err != nil {
			return nil, err
		}
		rows, err := host.DB.Query(r.Context(), `SELECT `+carCols+` FROM plugin_carousels ORDER BY position, position_order, id`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, scanCarousel)
	})
	save := func(r *http.Request, id int64) (any, error) {
		ctx := r.Context()
		if err := manage(ctx); err != nil {
			return nil, err
		}
		var c Carousel
		if err := h.Decode(r, &c); err != nil {
			return nil, err
		}
		if err := c.validate(); err != nil {
			return nil, err
		}
		var err error
		if id == 0 {
			err = host.DB.QueryRow(ctx, `INSERT INTO plugin_carousels(name, position, audience, autoplay, max_slides, feeds, feed_items, enabled, position_order)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, c.Name, c.Position, c.Audience, c.Autoplay, c.MaxSlides, c.Feeds, c.FeedItems, c.Enabled, c.Order).Scan(&c.ID)
		} else {
			c.ID = id
			var tag interface{ RowsAffected() int64 }
			tag, err = host.DB.Exec(ctx, `UPDATE plugin_carousels SET name=$2, position=$3, audience=$4, autoplay=$5, max_slides=$6, feeds=$7, feed_items=$8, enabled=$9,
				position_order=$10, updated_at=now() WHERE id=$1`, id, c.Name, c.Position, c.Audience, c.Autoplay, c.MaxSlides, c.Feeds, c.FeedItems, c.Enabled, c.Order)
			if err == nil && tag.RowsAffected() == 0 {
				err = apperr.NotFound("carousel")
			}
			if err == nil {
				_, err = host.DB.Exec(ctx, `DELETE FROM plugin_carousel_feed_items WHERE carousel_id=$1 AND NOT (feed = ANY($2))`, id, c.Feeds)
			}
		}
		if err != nil {
			return nil, err
		}
		_ = audit.Log(ctx, host.DB, "carousel.save", "carousel", c.ID, nil, c)
		if len(c.Feeds) > 0 {
			go func() { _ = p.fetchCarousel(context.WithoutCancel(ctx), c) }()
		}
		return c, nil
	}
	host.Handle("POST", "/carousels", func(w http.ResponseWriter, r *http.Request) (any, error) { return save(r, 0) })
	host.Handle("PUT", "/carousels/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return save(r, id)
	})
	host.Handle("DELETE", "/carousels/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		if err := manage(ctx); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if _, err := host.DB.Exec(ctx, `DELETE FROM plugin_carousels WHERE id=$1`, id); err != nil {
			return nil, err
		}
		_ = audit.Log(ctx, host.DB, "carousel.delete", "carousel", id, nil, nil)
		return h.NoContent{}, nil
	})
	// GET /carousels/{id}/slides – all slides (also future and expired ones) and the feed state
	host.Handle("GET", "/carousels/{id}/slides", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		if err := manage(ctx); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		slides, err := p.slides(ctx, id, false)
		if err != nil {
			return nil, err
		}
		type state struct {
			Feed      string     `json:"feed"`
			FetchedAt *time.Time `json:"fetched_at"`
			Error     string     `json:"error"`
			Items     int        `json:"items"`
		}
		rows, err := host.DB.Query(ctx, `SELECT s.feed, s.fetched_at, s.error, (SELECT count(*) FROM plugin_carousel_feed_items i WHERE i.carousel_id=s.carousel_id AND i.feed=s.feed)
			FROM plugin_carousel_feed_state s WHERE s.carousel_id=$1 ORDER BY s.feed`, id)
		if err != nil {
			return nil, err
		}
		feeds, err := pgx.CollectRows(rows, pgx.RowToStructByPos[state])
		if err != nil {
			return nil, err
		}
		return map[string]any{"slides": slides, "feeds": feeds}, nil
	})
	saveSlide := func(r *http.Request, carousel, id int64) (any, error) {
		ctx := r.Context()
		if err := manage(ctx); err != nil {
			return nil, err
		}
		var s Slide
		if err := h.Decode(r, &s); err != nil {
			return nil, err
		}
		if id != 0 {
			var kind string
			if err := host.DB.QueryRow(ctx, `SELECT kind FROM plugin_carousel_slides WHERE id=$1 AND carousel_id=$2`, id, carousel).Scan(&kind); err != nil {
				return nil, apperr.NotFound("slide")
			}
			s.Kind = kind
		} else {
			s.Kind = "manual"
		}
		if s.Kind == "news" {
			// news: only the period and the order can be changed
			_, err := host.DB.Exec(ctx, `UPDATE plugin_carousel_slides SET starts_at=$3, ends_at=$4 WHERE id=$1 AND carousel_id=$2`, id, carousel, s.StartsAt, s.EndsAt)
			return h.NoContent{}, err
		}
		if err := s.validate(); err != nil {
			return nil, err
		}
		var err error
		if id == 0 {
			err = host.DB.QueryRow(ctx, `INSERT INTO plugin_carousel_slides(carousel_id, kind, title, text, link, image, image_alt, starts_at, ends_at, position, created_by)
				VALUES ($1,'manual',$2,$3,$4,$5,$6,$7,$8,(SELECT COALESCE(max(position)+1, 0) FROM plugin_carousel_slides WHERE carousel_id=$1),$9) RETURNING id`,
				carousel, s.Title, s.Text, s.Link, s.Image, s.ImageAlt, s.StartsAt, s.EndsAt, identity.From(ctx).ID()).Scan(&s.ID)
		} else {
			s.ID = id
			_, err = host.DB.Exec(ctx, `UPDATE plugin_carousel_slides SET title=$3, text=$4, link=$5, image=$6, image_alt=$7, starts_at=$8, ends_at=$9 WHERE id=$1 AND carousel_id=$2`,
				id, carousel, s.Title, s.Text, s.Link, s.Image, s.ImageAlt, s.StartsAt, s.EndsAt)
		}
		if err != nil {
			return nil, err
		}
		_ = audit.Log(ctx, host.DB, "carousel.slide", "carousel", carousel, nil, s)
		return s, nil
	}
	host.Handle("POST", "/carousels/{id}/slides", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return saveSlide(r, id, 0)
	})
	host.Handle("PUT", "/carousels/{id}/slides/{slide}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		sid, err := h.PathID(r, "slide")
		if err != nil {
			return nil, err
		}
		return saveSlide(r, id, sid)
	})
	host.Handle("DELETE", "/carousels/{id}/slides/{slide}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		if err := manage(ctx); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		sid, err := h.PathID(r, "slide")
		if err != nil {
			return nil, err
		}
		if _, err := host.DB.Exec(ctx, `DELETE FROM plugin_carousel_slides WHERE id=$1 AND carousel_id=$2`, sid, id); err != nil {
			return nil, err
		}
		_ = audit.Log(ctx, host.DB, "carousel.slide_delete", "carousel", id, map[string]any{"slide": sid}, nil)
		return h.NoContent{}, nil
	})
	// PUT /carousels/{id}/order {"slides": [3, 1, 2]}
	host.Handle("PUT", "/carousels/{id}/order", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		if err := manage(ctx); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Slides []int64 `json:"slides"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		err = pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
			for i, sid := range in.Slides {
				if _, err := tx.Exec(ctx, `UPDATE plugin_carousel_slides SET position=$3 WHERE id=$1 AND carousel_id=$2`, sid, id, i); err != nil {
					return err
				}
			}
			return nil
		})
		return h.NoContent{}, err
	})
	// POST /carousels/{id}/refresh – fetch the feeds now
	host.Handle("POST", "/carousels/{id}/refresh", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		if err := manage(ctx); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		rows, err := host.DB.Query(ctx, `SELECT `+carCols+` FROM plugin_carousels WHERE id=$1`, id)
		if err != nil {
			return nil, err
		}
		c, err := pgx.CollectExactlyOneRow(rows, scanCarousel)
		if err != nil {
			return nil, apperr.NotFound("carousel")
		}
		if err := p.fetchCarousel(ctx, c); err != nil {
			return nil, apperr.BadRequest("%v", err) // the error is also shown in the feed state
		}
		return h.NoContent{}, nil
	})
	return nil
}

// slides loads the own and news slides of a carousel (current ones only if active).
func (p *Plugin) slides(ctx context.Context, carousel int64, active bool) ([]Slide, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT id, kind, element_id, title, text, link, image, image_alt, starts_at, ends_at, created_at
		FROM plugin_carousel_slides WHERE carousel_id=$1 AND (NOT $2 OR ((starts_at IS NULL OR starts_at <= now()) AND (ends_at IS NULL OR ends_at > now())))
		ORDER BY position, id`, carousel, active)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Slide, error) {
		var s Slide
		var created time.Time
		err := r.Scan(&s.ID, &s.Kind, &s.ElementID, &s.Title, &s.Text, &s.Link, &s.Image, &s.ImageAlt, &s.StartsAt, &s.EndsAt, &created)
		if s.Kind == "news" {
			s.Date = &created
		}
		return s, err
	})
}

// atPosition returns the enabled carousels of a position with their visible slides.
func (p *Plugin) atPosition(ctx context.Context, position, lang string) ([]map[string]any, error) {
	me := identity.From(ctx)
	signedIn := me != nil && me.ID() != 0
	rows, err := p.host.DB.Query(ctx, `SELECT `+carCols+` FROM plugin_carousels WHERE enabled AND position=$1 ORDER BY position_order, id`, position)
	if err != nil {
		return nil, err
	}
	cars, err := pgx.CollectRows(rows, scanCarousel)
	if err != nil {
		return nil, err
	}
	if lang == "" {
		lang = p.host.Config.Get().I18n.DefaultLanguage
	}
	out := []map[string]any{}
	for _, c := range cars {
		if (c.Audience == "guests" && signedIn) || (c.Audience == "users" && !signedIn) {
			continue
		}
		list, err := p.slides(ctx, c.ID, true)
		if err != nil {
			return nil, err
		}
		visible := make([]Slide, 0, len(list))
		for _, s := range list {
			if s.Kind == "news" {
				if s.ElementID == nil {
					continue
				}
				v, err := p.host.Content.GetElement(ctx, *s.ElementID, lang, false)
				if err != nil {
					continue // not visible to the caller (or deleted)
				}
				s.Title = v.Title // in the language of the viewer
				if v.URL != "" {
					s.Link = v.URL
				}
			}
			visible = append(visible, s)
		}
		items, err := p.feedSlides(ctx, c)
		if err != nil {
			return nil, err
		}
		visible = append(visible, items...)
		if len(visible) > c.MaxSlides {
			visible = visible[:c.MaxSlides]
		}
		if len(visible) == 0 {
			continue
		}
		out = append(out, map[string]any{"id": c.ID, "name": c.Name, "autoplay": c.Autoplay, "slides": visible})
	}
	return out, nil
}

func (p *Plugin) feedSlides(ctx context.Context, c Carousel) ([]Slide, error) {
	if len(c.Feeds) == 0 {
		return nil, nil
	}
	rows, err := p.host.DB.Query(ctx, `SELECT feed, title, link, summary, image, published_at FROM (
			SELECT *, row_number() OVER (PARTITION BY feed ORDER BY published_at DESC NULLS LAST, fetched_at DESC) AS n
			FROM plugin_carousel_feed_items WHERE carousel_id=$1 AND feed = ANY($2)) x
		WHERE n <= $3 ORDER BY published_at DESC NULLS LAST`, c.ID, c.Feeds, c.FeedItems)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Slide, error) {
		var s Slide
		var feed string
		err := r.Scan(&feed, &s.Title, &s.Link, &s.Text, &s.Image, &s.Date)
		s.Kind = "feed"
		if u, e := url.Parse(feed); e == nil {
			s.Source = strings.TrimPrefix(u.Host, "www.")
		}
		return s, err
	})
}

// ---------------------------------------------------------------- publish option "as news"

type publishOption struct{ p *Plugin }

func (publishOption) ID() string { return "core.carousel" }

// Apply adds the published page as news to the chosen carousels:
// {"carousels": [1, 2], "until": "2026-12-31"}.
func (o publishOption) Apply(ctx context.Context, svc *content.Service, el *content.ElementView, value json.RawMessage) error {
	if !perm.Has(ctx, "core.carousel.publish") && !perm.Has(ctx, "core.carousel.manage") {
		return apperr.ErrForbidden
	}
	var in struct {
		Carousels []int64 `json:"carousels"`
		Until     string  `json:"until"`
	}
	if err := json.Unmarshal(value, &in); err != nil {
		return err
	}
	if len(in.Carousels) == 0 {
		return nil
	}
	var until *time.Time
	if in.Until != "" {
		t, err := time.Parse("2006-01-02", in.Until)
		if err != nil {
			return apperr.BadRequest("until: YYYY-MM-DD")
		}
		t = t.Add(24 * time.Hour)
		until = &t
	}
	full, err := svc.GetElement(ctx, el.ID, el.Lang, true)
	if err != nil {
		return err
	}
	text, img := summary(full.HTML, 240), firstImage(full.HTML)
	if full.PreviewImage != "" {
		img = full.PreviewImage
		if !strings.Contains(img, "/") {
			img = "/api/v1/files/" + img
		}
	}
	uid := identity.From(ctx).ID()
	for _, cid := range in.Carousels {
		if _, err := o.p.host.DB.Exec(ctx, `INSERT INTO plugin_carousel_slides(carousel_id, kind, element_id, title, text, link, image, image_alt, ends_at, position, created_by)
			SELECT $1, 'news', $2, $3, $4, $5, $6, $3, $7, (SELECT COALESCE(min(position)-1, 0) FROM plugin_carousel_slides WHERE carousel_id=$1), $8
			WHERE EXISTS (SELECT 1 FROM plugin_carousels WHERE id=$1 AND enabled)
			ON CONFLICT (carousel_id, element_id) WHERE kind = 'news' DO UPDATE SET title=EXCLUDED.title, text=EXCLUDED.text, image=EXCLUDED.image,
				ends_at=EXCLUDED.ends_at, created_at=now()`,
			cid, el.ID, full.Title, text, fmt.Sprintf("/e/%d", el.ID), img, until, uid); err != nil {
			return err
		}
		_ = audit.Log(ctx, o.p.host.DB, "carousel.news", "carousel", cid, nil, map[string]any{"element_id": el.ID, "until": in.Until})
	}
	return nil
}

// summary returns the first characters of the text of an HTML fragment.
func summary(src string, max int) string {
	var b strings.Builder
	z := nethtml.NewTokenizer(strings.NewReader(src))
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case nethtml.ErrorToken:
			return cut(b.String(), max)
		case nethtml.StartTagToken:
			if n, _ := z.TagName(); string(n) == "script" || string(n) == "style" || string(n) == "nav" {
				skip++
			}
		case nethtml.EndTagToken:
			if n, _ := z.TagName(); (string(n) == "script" || string(n) == "style" || string(n) == "nav") && skip > 0 {
				skip--
			}
			b.WriteByte(' ')
		case nethtml.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		}
		if b.Len() > max*4 {
			return cut(b.String(), max)
		}
	}
}

func cut(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)[:max]
	if i := strings.LastIndexByte(string(r), ' '); i > max/2 {
		return string(r)[:i] + " …"
	}
	return string(r) + " …"
}

var imgRe = regexp.MustCompile(`(?i)<img\b[^>]*?\ssrc="([^"]+)"`)

func firstImage(src string) string {
	if m := imgRe.FindStringSubmatch(src); m != nil && (strings.HasPrefix(m[1], "/") || strings.HasPrefix(m[1], "https://")) {
		return m[1]
	}
	return ""
}

var errNoFeed = errors.New("not an RSS or Atom feed")
