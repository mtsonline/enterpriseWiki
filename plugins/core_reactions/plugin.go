// Package core_reactions adds emoji reactions to pages, categories and
// comments. Any emoji may be used; administrators can block single emojis
// (with all skin tones). Reading reactions requires the View permission on
// the target; reacting requires core.reactions.use. The author of the
// target is notified once per person (in the wiki; e-mail is off by
// default and can be switched on in the profile).
package core_reactions

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/notify"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// LimitPerPerson is the limit (manifest) of the different emojis of one
// person on one target.
const LimitPerPerson = "core.reactions.per_person"

// Plugin is the reactions plugin.
type Plugin struct{ host *sdk.Host }

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Reaction is one emoji on a target with the people who chose it.
type Reaction struct {
	Emoji string   `json:"emoji"`
	Count int      `json:"count"`
	Mine  bool     `json:"mine"`
	Users []string `json:"users"` // display names (first 20)
	// UserIDs of all people (only for core.reactions.manage, to remove reactions)
	UserIDs []int64 `json:"user_ids,omitempty"`
}

// Summary are the reactions of one target.
type Summary struct {
	TargetType string     `json:"target_type"`
	TargetID   int64      `json:"target_id"`
	Reactions  []Reaction `json:"reactions"`
	CanReact   bool       `json:"can_react"`
}

// target is a reactable object.
type target struct {
	category int64
	author   *int64
	title    string
	link     string
}

// Setup implements sdk.SetupPlugin.
func (p *Plugin) Setup(host *sdk.Host) error {
	p.host = host
	notify.RegisterKindOff("reaction", "core.reactions.kind")

	// GET /{type}/{id} – the reactions of one target
	host.Handle("GET", "/{type}/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		list, err := p.summaries(r.Context(), r.PathValue("type"), []int64{id})
		if err != nil {
			return nil, err
		}
		return list[0], nil
	})
	// GET /batch?type=comment&ids=1,2,3 – reactions of several targets (invisible ones are left out)
	host.Handle("GET", "/batch", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var ids []int64
		for _, s := range strings.Split(r.URL.Query().Get("ids"), ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && id > 0 {
				ids = append(ids, id)
			}
		}
		if len(ids) > 500 {
			return nil, apperr.BadRequest("at most 500 ids")
		}
		return p.summaries(r.Context(), r.URL.Query().Get("type"), ids)
	})
	// POST /{type}/{id} {"emoji": "👍", "on": true} – add or remove an own reaction
	host.Handle("POST", "/{type}/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Emoji string `json:"emoji"`
			On    *bool  `json:"on"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		on := in.On == nil || *in.On
		return p.react(r.Context(), r.PathValue("type"), id, in.Emoji, on)
	})
	// DELETE /{type}/{id}/users/{uid}?emoji=… – remove reactions of others (manage permission)
	host.Handle("DELETE", "/{type}/{id}/users/{uid}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		if err := perm.Require(ctx, "core.reactions.manage"); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		uid, err := h.PathID(r, "uid")
		if err != nil {
			return nil, err
		}
		typ, emoji := r.PathValue("type"), r.URL.Query().Get("emoji")
		tag, err := host.DB.Exec(ctx, `DELETE FROM plugin_reactions WHERE target_type=$1 AND target_id=$2 AND user_id=$3 AND ($4 = '' OR emoji=$4)`, typ, id, uid, emoji)
		if err != nil {
			return nil, err
		}
		_ = audit.Log(ctx, host.DB, "reaction.remove", typ, id, map[string]any{"user_id": uid, "emoji": emoji}, map[string]any{"removed": tag.RowsAffected()})
		return h.NoContent{}, nil
	})
	// GET/PUT /settings {"blocked": ["🖕"]}
	host.Handle("GET", "/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if identity.From(r.Context()) == nil {
			return nil, apperr.ErrUnauthorized
		}
		return map[string]any{"blocked": p.blocked()}, nil
	})
	host.Handle("PUT", "/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		if err := perm.Require(ctx, "core.reactions.manage"); err != nil {
			return nil, err
		}
		var in struct {
			Blocked []string `json:"blocked"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		clean := []string{}
		for _, e := range in.Blocked {
			e = strings.TrimSpace(e)
			if e == "" {
				continue
			}
			if !ValidEmoji(e) {
				return nil, apperr.BadRequest("%q is not an emoji", e)
			}
			if !slices.Contains(clean, Base(e)) {
				clean = append(clean, Base(e))
			}
		}
		before := p.blocked()
		err := host.Config.Update(func(c *config.Config) error {
			if c.Settings == nil {
				c.Settings = map[string]map[string]any{}
			}
			if c.Settings[host.Manifest.ID] == nil {
				c.Settings[host.Manifest.ID] = map[string]any{}
			}
			list := make([]any, len(clean))
			for i, e := range clean {
				list[i] = e
			}
			c.Settings[host.Manifest.ID]["blocked"] = list
			return nil
		})
		if err != nil {
			return nil, err
		}
		_ = audit.Log(ctx, host.DB, "reaction.blocklist", "settings", 0, map[string]any{"blocked": before}, map[string]any{"blocked": clean})
		return map[string]any{"blocked": clean}, nil
	})
	return nil
}

// blocked returns the blocked emojis (base form, see Base).
func (p *Plugin) blocked() []string {
	out := []string{}
	if l, ok := p.host.Settings()["blocked"].([]any); ok {
		for _, v := range l {
			if s, ok := v.(string); ok && s != "" {
				out = append(out, Base(s))
			}
		}
	}
	return out
}

func (p *Plugin) isBlocked(emoji string) bool {
	return slices.Contains(p.blocked(), Base(emoji))
}

// resolve checks that the caller may see the target and returns its data.
func (p *Plugin) resolve(ctx context.Context, typ string, id int64) (*target, error) {
	lang := p.host.Config.Get().I18n.DefaultLanguage
	switch typ {
	case "element":
		cat, err := p.host.Content.ElementCategory(ctx, id, acl.View)
		if err != nil {
			return nil, err
		}
		t := &target{category: cat, link: fmt.Sprintf("/e/%d", id)}
		_ = p.host.DB.QueryRow(ctx, `SELECT created_by, COALESCE(titles->>$2, (SELECT v FROM jsonb_each_text(titles) AS x(k, v) LIMIT 1), '')
			FROM elements WHERE id=$1`, id, lang).Scan(&t.author, &t.title)
		return t, nil
	case "category":
		if err := p.host.Content.Require(ctx, id, acl.View); err != nil {
			return nil, err
		}
		t := &target{category: id, link: fmt.Sprintf("/c/%d", id)}
		_ = p.host.DB.QueryRow(ctx, `SELECT created_by, COALESCE(titles->>$2, (SELECT v FROM jsonb_each_text(titles) AS x(k, v) LIMIT 1), '')
			FROM categories WHERE id=$1`, id, lang).Scan(&t.author, &t.title)
		return t, nil
	case "comment":
		// comments of the plugin core.comments (missing table: not found)
		var el int64
		var author *int64
		err := p.host.DB.QueryRow(ctx, `SELECT element_id, author_id FROM plugin_comments WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&el, &author)
		if err != nil {
			return nil, apperr.NotFound("comment")
		}
		t, err := p.resolve(ctx, "element", el)
		if err != nil {
			return nil, apperr.NotFound("comment")
		}
		t.author, t.link = author, fmt.Sprintf("/e/%d#comment-%d", el, id)
		return t, nil
	}
	return nil, apperr.BadRequest("type must be element, category or comment")
}

// summaries loads the reactions of targets the caller may see.
func (p *Plugin) summaries(ctx context.Context, typ string, ids []int64) ([]Summary, error) {
	var visible []int64
	for _, id := range ids {
		if _, err := p.resolve(ctx, typ, id); err == nil {
			visible = append(visible, id)
		} else if len(ids) == 1 {
			return nil, err
		}
	}
	me := identity.From(ctx)
	canReact := me != nil && me.ID() != 0 && perm.Has(ctx, "core.reactions.use")
	out := make([]Summary, 0, len(visible))
	if len(visible) == 0 {
		return out, nil
	}
	var uid int64
	if me != nil {
		uid = me.ID()
	}
	manage := perm.Has(ctx, "core.reactions.manage")
	rows, err := p.host.DB.Query(ctx, `SELECT r.target_id, r.emoji, count(*), bool_or(r.user_id = $3),
			(array_agg(COALESCE(NULLIF(u.display_name, ''), u.username) ORDER BY r.created_at))[1:CASE WHEN $4 THEN 100000 ELSE 20 END], array_agg(r.user_id ORDER BY r.created_at)
		FROM plugin_reactions r JOIN users u ON u.id = r.user_id
		WHERE r.target_type=$1 AND r.target_id = ANY($2)
		GROUP BY r.target_id, r.emoji ORDER BY min(r.created_at)`, typ, visible, uid, manage)
	if err != nil {
		return nil, err
	}
	by := map[int64][]Reaction{}
	blocked := p.blocked()
	for rows.Next() {
		var id int64
		var x Reaction
		var ids []int64
		if err := rows.Scan(&id, &x.Emoji, &x.Count, &x.Mine, &x.Users, &ids); err != nil {
			rows.Close()
			return nil, err
		}
		if manage {
			x.UserIDs = ids
		}
		if !slices.Contains(blocked, Base(x.Emoji)) { // blocked later: hidden
			by[id] = append(by[id], x)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range visible {
		list := by[id]
		if list == nil {
			list = []Reaction{}
		}
		out = append(out, Summary{TargetType: typ, TargetID: id, Reactions: list, CanReact: canReact})
	}
	return out, nil
}

// react adds (on) or removes an own reaction and returns the new summary.
func (p *Plugin) react(ctx context.Context, typ string, id int64, emoji string, on bool) (*Summary, error) {
	me := identity.From(ctx)
	if me == nil || me.ID() == 0 {
		return nil, apperr.ErrUnauthorized
	}
	if err := perm.Require(ctx, "core.reactions.use"); err != nil {
		return nil, err
	}
	emoji = strings.TrimSpace(emoji)
	if !ValidEmoji(emoji) {
		return nil, apperr.BadRequest("not an emoji")
	}
	t, err := p.resolve(ctx, typ, id)
	if err != nil {
		return nil, err
	}
	first := false
	if on {
		if p.isBlocked(emoji) {
			return nil, apperr.BadRequest("this emoji is not allowed")
		}
		err = pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM plugin_reactions WHERE target_type=$1 AND target_id=$2 AND user_id=$3`, typ, id, me.ID()).Scan(&n); err != nil {
				return err
			}
			if max := int(p.host.Limit(LimitPerPerson)); n >= max {
				return apperr.BadRequest("at most %d reactions per person", max)
			}
			first = n == 0
			if _, err := tx.Exec(ctx, `INSERT INTO plugin_reactions(target_type, target_id, user_id, emoji) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, typ, id, me.ID(), emoji); err != nil {
				return err
			}
			// the author learns about the first reaction of each person (not
			// about each emoji) - stored with the reaction or not at all
			if !first || t.author == nil || *t.author == me.ID() {
				return nil
			}
			author, err := p.host.Auth.LoadPrincipalQ(ctx, tx, *t.author)
			if err != nil || !p.host.ACL.Can(ctx, author, t.category, acl.View) {
				return nil // gone or without access: nobody to tell
			}
			by := me.DisplayName
			if by == "" {
				by = me.Handle()
			}
			_, err = p.host.Notify.SendQ(ctx, tx, []int64{*t.author}, notify.Message{Kind: "reaction", EntityType: typ, EntityID: id, Link: t.link,
				Text: func(lang string) (string, string) {
					params := map[string]string{"by": by, "emoji": emoji, "title": t.title}
					return p.host.T(ctx, lang, "core.reactions.mail_subject", params), p.host.T(ctx, lang, "core.reactions.mail_body", params)
				}})
			return err
		})
	} else {
		_, err = p.host.DB.Exec(ctx, `DELETE FROM plugin_reactions WHERE target_type=$1 AND target_id=$2 AND user_id=$3 AND emoji=$4`, typ, id, me.ID(), emoji)
	}
	if err != nil {
		return nil, err
	}
	list, err := p.summaries(ctx, typ, []int64{id})
	if err != nil {
		return nil, err
	}
	return &list[0], nil
}

// ValidEmoji reports whether s is one emoji (a single grapheme of emoji
// code points: with skin tone, variation selector, ZWJ sequences, flags,
// keycaps and tag sequences).
func ValidEmoji(s string) bool {
	if s == "" || len(s) > 64 || !utf8.ValidString(s) {
		return false
	}
	pict := 0
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == 0x200D || r == 0xFE0F || r == 0xFE0E || r == 0x20E3:
			// joiner, variation selectors, keycap
		case r >= 0x1F3FB && r <= 0x1F3FF:
			// skin tones
		case r >= 0xE0020 && r <= 0xE007F:
			// tag sequences (subdivision flags)
		case (r >= '0' && r <= '9') || r == '#' || r == '*':
			if i+1 >= len(runes) || (runes[i+1] != 0xFE0F && runes[i+1] != 0x20E3) {
				return false // a plain digit is no emoji
			}
			pict++
		case isPictographic(r):
			pict++
		default:
			return false
		}
	}
	return pict >= 1 && pict <= 10
}

func isPictographic(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF, r >= 0x2600 && r <= 0x27BF, r >= 0x2300 && r <= 0x23FF,
		r >= 0x2B00 && r <= 0x2BFF, r >= 0x2190 && r <= 0x21FF, r >= 0x25A0 && r <= 0x25FF,
		r >= 0x2934 && r <= 0x2935, r >= 0x3030 && r <= 0x303D, r == 0x3297, r == 0x3299,
		r == 0x00A9, r == 0x00AE, r == 0x203C, r == 0x2049, r == 0x2122, r == 0x2139:
		return true
	}
	return false
}

// Base strips skin tones and variation selectors (blocking 👍 blocks 👍🏽 too).
func Base(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == 0xFE0F || r == 0xFE0E || (r >= 0x1F3FB && r <= 0x1F3FF) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
