// Package watch lets users follow categories and elements and sends them
// e-mails about changes. Changes are recorded in watch_events right after
// they happen, coalesced per user and object and sent after a quiet period
// (default 2 minutes) as one e-mail per user through the mail queue.
package watch

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/i18n"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/mail"
)

// Delay is the quiet period before changes are sent (several saves in a
// row become one e-mail).
var Delay = 2 * time.Minute

// Service manages watches.
type Service struct {
	pool    *pgxpool.Pool
	cfg     *config.Store
	acl     *acl.Service
	auth    *auth.Service
	content *content.Service
	mail    *mail.Service
	i18n    *i18n.Service
	log     *slog.Logger
}

// New creates the service and subscribes to content changes.
func New(pool *pgxpool.Pool, cfg *config.Store, a *acl.Service, au *auth.Service, c *content.Service, m *mail.Service, i *i18n.Service, log *slog.Logger) *Service {
	s := &Service{pool: pool, cfg: cfg, acl: a, auth: au, content: c, mail: m, i18n: i, log: log}
	c.OnChange(s.onChange)
	return s
}

// Watch is a watch of the current user.
type Watch struct {
	ID         int64             `json:"id"`
	TargetType string            `json:"target_type"`
	TargetID   int64             `json:"target_id"`
	Mode       string            `json:"mode"` // link | content
	Recursive  bool              `json:"recursive"`
	CreatedAt  time.Time         `json:"created_at"`
	Titles     map[string]string `json:"titles"`
	Frequency  string            `json:"frequency"` // instant | daily | weekly
}

// Input creates or changes a watch.
type Input struct {
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
	Mode       string `json:"mode"`
	Recursive  *bool  `json:"recursive"`
	Frequency  string `json:"frequency"` // instant (default) | daily | weekly
}

func me(ctx context.Context) (int64, error) {
	p := identity.From(ctx)
	if p == nil || p.ID() == 0 {
		return 0, apperr.ErrUnauthorized
	}
	return p.ID(), nil
}

// category returns the category of the target (checks View).
func (s *Service) category(ctx context.Context, typ string, id int64) (int64, error) {
	switch typ {
	case "element":
		return s.content.ElementCategory(ctx, id, acl.View)
	case "category":
		if err := s.content.Require(ctx, id, acl.View); err != nil {
			return 0, err
		}
		return id, nil
	}
	return 0, apperr.BadRequest("target_type must be category or element")
}

// List returns the watches of the current user.
func (s *Service) List(ctx context.Context) ([]Watch, error) {
	uid, err := me(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT w.id, w.target_type, w.target_id, w.mode, w.recursive, w.created_at,
		COALESCE(CASE w.target_type WHEN 'category' THEN (SELECT titles FROM categories WHERE id=w.target_id AND deleted_at IS NULL)
			ELSE (SELECT titles FROM elements WHERE id=w.target_id AND deleted_at IS NULL) END, '{}'), w.frequency
		FROM watches w WHERE w.user_id=$1 ORDER BY w.created_at DESC`, uid)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Watch])
}

// Get returns the watch of the current user for a target (nil if none).
func (s *Service) Get(ctx context.Context, typ string, id int64) (*Watch, error) {
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range list {
		if w.TargetType == typ && w.TargetID == id {
			return &w, nil
		}
	}
	return nil, nil
}

// Set creates or changes a watch of the current user.
func (s *Service) Set(ctx context.Context, in Input) (*Watch, error) {
	uid, err := me(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.category(ctx, in.TargetType, in.TargetID); err != nil {
		return nil, err
	}
	if in.Mode != "content" {
		in.Mode = "link"
	}
	rec := in.Recursive == nil || *in.Recursive
	switch in.Frequency {
	case "":
		in.Frequency = "instant"
	case "instant", "daily", "weekly":
	default:
		return nil, apperr.BadRequest("frequency must be instant, daily or weekly")
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO watches(user_id, target_type, target_id, mode, recursive, frequency) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (user_id, target_type, target_id) DO UPDATE SET mode=EXCLUDED.mode, recursive=EXCLUDED.recursive, frequency=EXCLUDED.frequency`,
		uid, in.TargetType, in.TargetID, in.Mode, rec, in.Frequency); err != nil {
		return nil, err
	}
	return s.Get(ctx, in.TargetType, in.TargetID)
}

// Delete removes a watch of the current user.
func (s *Service) Delete(ctx context.Context, id int64) error {
	uid, err := me(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM watches WHERE id=$1 AND user_id=$2`, id, uid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("watch")
	}
	return nil
}

// onChange records a change for all watchers (except the person who made it).
func (s *Service) onChange(ctx context.Context, event, etype string, id int64) {
	if !s.mail.Enabled() || content.IsSilent(ctx) {
		return
	}
	ctx = context.WithoutCancel(ctx)
	actor := identity.From(ctx).ID()
	var err error
	switch etype {
	case "element":
		_, err = s.pool.Exec(ctx, `INSERT INTO watch_events(user_id, entity_type, entity_id, event, actor_id, mode, freq)
			SELECT w.user_id, 'element', $1::bigint, $2::text, $3::bigint, max(w.mode), `+freqSQL+` FROM watch_effective w
			WHERE w.user_id <> COALESCE($3::bigint, 0) AND ((w.target_type='element' AND w.target_id=$1) OR (w.target_type='category' AND (
				w.target_id = (SELECT category_id FROM elements WHERE id=$1) OR (w.recursive AND w.target_id IN (
					SELECT a.id FROM categories a, categories c WHERE c.id=(SELECT category_id FROM elements WHERE id=$1) AND a.path @> c.path)))))
			GROUP BY w.user_id
			ON CONFLICT (user_id, entity_type, entity_id) DO UPDATE SET event = CASE WHEN watch_events.event='create' AND EXCLUDED.event='update' THEN 'create' ELSE EXCLUDED.event END,
				actor_id=EXCLUDED.actor_id, last_at=now(), mode = GREATEST(watch_events.mode, EXCLUDED.mode), freq = LEAST(watch_events.freq, EXCLUDED.freq)`, id, event, nullID(actor))
	case "category":
		// a new, changed or deleted category is reported to watchers of the category itself and of its parents
		_, err = s.pool.Exec(ctx, `INSERT INTO watch_events(user_id, entity_type, entity_id, event, actor_id, mode, freq)
			SELECT w.user_id, 'category', $1::bigint, $2::text, $3::bigint, 'link', `+freqSQL+` FROM watch_effective w, categories c
			WHERE c.id=$1 AND w.user_id <> COALESCE($3::bigint, 0) AND w.target_type='category' AND (w.target_id=$1
				OR (w.target_id = c.parent_id) OR (w.recursive AND w.target_id IN (SELECT a.id FROM categories a WHERE a.path @> c.path)))
			GROUP BY w.user_id
			ON CONFLICT (user_id, entity_type, entity_id) DO UPDATE SET event=EXCLUDED.event, actor_id=EXCLUDED.actor_id, last_at=now(), freq = LEAST(watch_events.freq, EXCLUDED.freq)`, id, event, nullID(actor))
	}
	if err != nil {
		s.log.Error("recording a change for watchers failed", "type", etype, "id", id, "err", err)
	}
}

// freqSQL is the most urgent frequency of the matching watches (0 instant, 1 daily, 2 weekly).
const freqSQL = `min(CASE w.frequency WHEN 'daily' THEN 1 WHEN 'weekly' THEN 2 ELSE 0 END)::smallint`

// DigestBoundaries returns the last daily and weekly digest times at or before now.
func DigestBoundaries(m config.Mail, now time.Time) (daily, weekly time.Time) {
	loc := m.DigestLocation()
	n := now.In(loc)
	daily = time.Date(n.Year(), n.Month(), n.Day(), m.DigestHour, 0, 0, 0, loc)
	if daily.After(n) {
		daily = daily.AddDate(0, 0, -1)
	}
	weekly = daily
	for int(weekly.Weekday()) != m.DigestWeekday {
		weekly = weekly.AddDate(0, 0, -1)
	}
	return daily, weekly
}

func nullID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

type pending struct {
	userID     int64
	entityType string
	entityID   int64
	event      string
	actorID    *int64
	mode       string
	at         time.Time
	freq       int16
}

// item is one change in a notification.
type item struct {
	title, url, event, actor, html, text string
}

// Send delivers due changes as one e-mail per user (called by the worker).
func (s *Service) Send(ctx context.Context) (int, error) {
	sent := 0
	for round := 0; round < 20; round++ {
		n, done, err := s.sendBatch(ctx)
		sent += n
		if err != nil || done {
			return sent, err
		}
	}
	return sent, nil
}

func (s *Service) sendBatch(ctx context.Context) (int, bool, error) {
	sent := 0
	done := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// due: instant changes after a quiet period of the user, digests
		// with changes recorded before the last digest time
		daily, weekly := DigestBoundaries(s.cfg.Get().Mail, time.Now())
		rows, err := tx.Query(ctx, `SELECT user_id, entity_type, entity_id, event, actor_id, mode, last_at, freq FROM watch_events
			WHERE user_id IN (SELECT user_id FROM watch_events GROUP BY user_id
				HAVING max(last_at) FILTER (WHERE freq = 0) < now() - make_interval(secs => $1)
					OR min(first_at) FILTER (WHERE freq = 1) < $2 OR min(first_at) FILTER (WHERE freq = 2) < $3 LIMIT 50)
			AND ((freq = 0 AND last_at < now() - make_interval(secs => $1)) OR (freq = 1 AND first_at < $2) OR (freq = 2 AND first_at < $3))
			ORDER BY user_id, last_at FOR UPDATE SKIP LOCKED`, Delay.Seconds(), daily, weekly)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pending, error) {
			var p pending
			return p, r.Scan(&p.userID, &p.entityType, &p.entityID, &p.event, &p.actorID, &p.mode, &p.at, &p.freq)
		})
		if err != nil {
			return err
		}
		if len(list) == 0 {
			done = true
			return nil
		}
		byUser := map[int64][]pending{}
		var users []int64
		for _, p := range list {
			if _, ok := byUser[p.userID]; !ok {
				users = append(users, p.userID)
			}
			byUser[p.userID] = append(byUser[p.userID], p)
		}
		for _, uid := range users {
			if err := s.notify(ctx, tx, uid, byUser[uid]); err != nil {
				return err
			}
			for _, p := range byUser[uid] {
				if _, err := tx.Exec(ctx, `DELETE FROM watch_events WHERE user_id=$1 AND entity_type=$2 AND entity_id=$3 AND last_at <= $4`,
					uid, p.entityType, p.entityID, p.at); err != nil {
					return err
				}
			}
			sent++
		}
		return nil
	})
	return sent, done, err
}

// notify builds and queues the e-mail for one user.
func (s *Service) notify(ctx context.Context, tx pgx.Tx, uid int64, changes []pending) error {
	us, err := s.mail.Users(ctx, tx, []int64{uid})
	if err != nil || len(us) == 0 || !us[0].Wants("watches") {
		return err
	}
	u := us[0]
	p, err := s.auth.LoadPrincipalQ(ctx, tx, uid) // inside the transaction (S-01)
	if err != nil {
		return nil // user disabled or deleted meanwhile
	}
	uctx := identity.With(ctx, p)
	cfg := s.cfg.Get()
	base := strings.TrimSuffix(cfg.Server.BaseURL, "/")
	tr := func(key string, params map[string]string) string { return s.i18n.T(ctx, u.Lang, key, params) }
	var items []item
	for _, c := range changes {
		it, ok := s.describe(uctx, tx, c, u.Lang, base, tr)
		if ok {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return nil
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].title < items[j].title })
	subject := tr("watch.mail_subject_one", map[string]string{"title": items[0].title, "event": items[0].event})
	if len(items) > 1 {
		subject = tr("watch.mail_subject_many", map[string]string{"count": fmt.Sprint(len(items)), "site": cfg.UI.SiteName})
	}
	var digest int16
	for _, c := range changes {
		if c.freq > digest {
			digest = c.freq
		}
	}
	switch digest {
	case 1:
		subject = tr("watch.mail_subject_daily", map[string]string{"count": fmt.Sprint(len(items)), "site": cfg.UI.SiteName})
	case 2:
		subject = tr("watch.mail_subject_weekly", map[string]string{"count": fmt.Sprint(len(items)), "site": cfg.UI.SiteName})
	}
	var text, htm strings.Builder
	text.WriteString(tr("watch.mail_intro", map[string]string{"name": u.Name}) + "\n\n")
	htm.WriteString(`<!DOCTYPE html><html><body style="font-family:Arial,Helvetica,sans-serif;font-size:15px;line-height:1.5;color:#172b4d">`)
	htm.WriteString("<p>" + html.EscapeString(tr("watch.mail_intro", map[string]string{"name": u.Name})) + "</p>")
	for _, it := range items {
		fmt.Fprintf(&text, "- %s: %s (%s)\n  %s\n", it.title, it.event, it.actor, it.url)
		fmt.Fprintf(&htm, `<h2 style="font-size:17px;margin:24px 0 4px"><a href="%s">%s</a></h2><p style="color:#5e6c84;margin:0 0 8px">%s – %s</p>`,
			html.EscapeString(it.url), html.EscapeString(it.title), html.EscapeString(it.event), html.EscapeString(it.actor))
		if it.html != "" {
			htm.WriteString(`<div style="border-left:3px solid #dfe1e6;padding-left:12px">` + it.html + `</div>`)
			text.WriteString("\n" + it.text + "\n\n")
		}
	}
	footer := tr("watch.mail_footer", map[string]string{"url": base + "/profile"})
	text.WriteString("\n" + footer + "\n")
	htm.WriteString(`<p style="color:#5e6c84;font-size:13px;margin-top:32px">` + html.EscapeString(footer) + `</p></body></html>`)
	_, err = s.mail.Enqueue(ctx, tx, mail.Message{To: []string{u.Email}, Subject: subject, Text: text.String(), HTML: htm.String(),
		Source: "watch", Ref: fmt.Sprintf("user:%d", uid), Owner: -1})
	return err
}

// describe turns a change into a notification item if the user may see it.
func (s *Service) describe(ctx context.Context, tx pgx.Tx, c pending, lang, base string, tr func(string, map[string]string) string) (item, bool) {
	it := item{event: tr("watch.event."+c.event, nil)}
	if c.actorID != nil {
		var name string
		_ = tx.QueryRow(ctx, `SELECT CASE WHEN display_name='' THEN username ELSE display_name END FROM users WHERE id=$1`, *c.actorID).Scan(&name)
		it.actor = name
	}
	if it.actor == "" {
		it.actor = tr("watch.someone", nil)
	}
	pick := func(m map[string]string) string {
		if v := m[lang]; v != "" {
			return v
		}
		for _, v := range m {
			return v
		}
		return ""
	}
	if c.entityType == "category" {
		var titles map[string]string
		if err := tx.QueryRow(ctx, `SELECT titles FROM categories WHERE id=$1`, c.entityID).Scan(&titles); err != nil {
			return it, false
		}
		if !s.acl.Can(ctx, identity.From(ctx), c.entityID, acl.View) {
			return it, false
		}
		it.title, it.url = pick(titles), fmt.Sprintf("%s/c/%d", base, c.entityID)
		return it, true
	}
	var titles map[string]string
	var cat int64
	var deleted *time.Time
	if err := tx.QueryRow(ctx, `SELECT titles, category_id, deleted_at FROM elements WHERE id=$1`, c.entityID).Scan(&titles, &cat, &deleted); err != nil {
		return it, false
	}
	if !s.acl.Can(ctx, identity.From(ctx), cat, acl.View) {
		return it, false
	}
	it.title, it.url = pick(titles), fmt.Sprintf("%s/e/%d", base, c.entityID)
	if deleted != nil {
		it.event, it.url = tr("watch.event.delete", nil), fmt.Sprintf("%s/c/%d", base, cat)
		return it, true
	}
	// confidential content does not leave the wiki by e-mail: title and link only
	if c.mode == "content" && s.acl.Classification(ctx, cat) < acl.Confidential {
		view, err := s.content.GetElement(ctx, c.entityID, lang, false)
		if err == nil {
			it.title = view.Title
			it.html = absolutize(view.HTML, base)
			it.text = plain(view.HTML)
		} else if !errors.Is(err, context.Canceled) {
			s.log.Warn("watch: content not available", "element", c.entityID, "err", err)
		}
	}
	return it, true
}

var (
	reBlock = regexp.MustCompile(`(?i)</?(p|div|h[1-6]|li|tr|br|table|ul|ol|pre|blockquote)[^>]*>`)
	reTag   = regexp.MustCompile(`<[^>]*>`)
	reSpace = regexp.MustCompile(`[ \t]+`)
	reLines = regexp.MustCompile(`\n\s*\n+`)
)

// plain converts rendered HTML into readable text for the text part.
func plain(h string) string {
	h = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`).ReplaceAllString(h, "")
	h = reBlock.ReplaceAllString(h, "\n")
	h = html.UnescapeString(reTag.ReplaceAllString(h, ""))
	h = reSpace.ReplaceAllString(h, " ")
	h = reLines.ReplaceAllString(h, "\n\n")
	if len(h) > 20000 {
		h = h[:20000] + " …"
	}
	return strings.TrimSpace(h)
}

// absolutize makes links and images of rendered content work in e-mails.
func absolutize(h, base string) string {
	r := strings.NewReplacer(`href="/`, `href="`+base+`/`, `src="/`, `src="`+base+`/`)
	return r.Replace(h)
}
