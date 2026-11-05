// Package core_mentions notifies people mentioned with @name (user name, or
// name@provider for external accounts) in pages and comments. Each person
// is notified once per page or comment, and only if they may see it.
package core_mentions

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/notify"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

//go:embed migrations/*.sql
var migrations embed.FS

// Plugin is the mentions plugin.
type Plugin struct{ host *sdk.Host }

// instances holds the plugin instance of each site (for Notify).
var instances sdk.PerSite[*Plugin]

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Setup implements sdk.SetupPlugin.
func (p *Plugin) Setup(host *sdk.Host) error {
	instances.Set(host.Site, p)
	p.host = host
	notify.RegisterKind("mention", "core.mentions.kind")
	host.Content.OnChange(func(ctx context.Context, event, etype string, id int64) {
		if etype != "element" || (event != "create" && event != "update") || !host.Active() {
			return
		}
		p.element(context.WithoutCancel(ctx), id)
	})
	return nil
}

var mentionRe = regexp.MustCompile(`(?:^|[\s(\[{>"'„“,;:])@([\p{L}\p{N}][\p{L}\p{N}._-]{0,63}(?:@[\p{L}\p{N}._-]{1,64})?)`)

// Handles returns the mentioned handles of a text (without duplicates).
func Handles(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mentionRe.FindAllStringSubmatch(text, 50) {
		h := strings.TrimRight(strings.ToLower(m[1]), ".-_")
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// element notifies the people mentioned in the latest revisions of an element.
func (p *Plugin) element(ctx context.Context, id int64) {
	var typ string
	var cat int64
	var titles map[string]string
	if err := p.host.DB.QueryRow(ctx, `SELECT type, category_id, titles FROM elements WHERE id=$1`, id).Scan(&typ, &cat, &titles); err != nil {
		return
	}
	t := p.host.Content.Type(typ)
	if t == nil {
		return
	}
	rows, err := p.host.DB.Query(ctx, `SELECT DISTINCT ON (lang) body FROM element_revisions WHERE element_id=$1 ORDER BY lang, rev DESC`, id)
	if err != nil {
		return
	}
	var text strings.Builder
	for rows.Next() {
		var body []byte
		if rows.Scan(&body) == nil {
			text.WriteString(t.Text(body))
			text.WriteByte('\n')
		}
	}
	rows.Close()
	title := titles[p.host.Config.Get().I18n.DefaultLanguage]
	for _, v := range titles {
		if title == "" {
			title = v
		}
	}
	p.notify(ctx, "element", id, cat, title, fmt.Sprintf("/e/%d", id), text.String())
}

// Notify notifies the people mentioned in text (e.g. a comment) once per
// entity; it is used by other plugins and does nothing while this plugin is
// disabled.
func Notify(ctx context.Context, entityType string, entityID, categoryID int64, title, link, text string) {
	instance := instances.Get(ctx)
	if instance == nil || instance.host == nil || !instance.host.Active() {
		return
	}
	instance.notify(ctx, entityType, entityID, categoryID, title, link, text)
}

func (p *Plugin) notify(ctx context.Context, entityType string, entityID, categoryID int64, title, link, text string) {
	handles := Handles(text)
	me := identity.From(ctx)
	if len(handles) == 0 || me == nil || !perm.HasP(me, "core.mentions.use") {
		return
	}
	var ids []int64
	for _, hdl := range handles {
		name, provider, ok := strings.Cut(hdl, "@")
		var id int64
		var err error
		if ok {
			err = p.host.DB.QueryRow(ctx, `SELECT id FROM users WHERE lower(username)=$1 AND lower(provider)=$2 AND NOT disabled`, name, provider).Scan(&id)
		} else {
			err = p.host.DB.QueryRow(ctx, `SELECT id FROM users WHERE lower(username)=$1 AND NOT disabled ORDER BY provider <> 'local', id LIMIT 1`, name).Scan(&id)
		}
		if err != nil || id == me.ID() || !p.host.UserCan(ctx, id, categoryID, acl.View) {
			continue
		}
		// once per entity and person
		tag, err := p.host.DB.Exec(ctx, `INSERT INTO plugin_mentions(entity_type, entity_id, user_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, entityType, entityID, id)
		if err != nil || tag.RowsAffected() == 0 {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}
	by := me.DisplayName
	if by == "" {
		by = me.Handle()
	}
	excerpt := excerptAround(text, handles)
	_, err := p.host.Notify.Send(ctx, ids, notify.Message{Kind: "mention", EntityType: entityType, EntityID: entityID, Link: link,
		Text: func(lang string) (string, string) {
			params := map[string]string{"by": by, "title": title, "excerpt": excerpt}
			return p.host.T(ctx, lang, "core.mentions.mail_subject", params), p.host.T(ctx, lang, "core.mentions.mail_body", params)
		}})
	if err != nil {
		p.host.Log.Error("mention notifications", "err", err)
	}
}

// excerptAround returns the text around the first mention (max. 300 characters).
func excerptAround(text string, handles []string) string {
	flat := strings.Join(strings.Fields(text), " ")
	i := strings.Index(strings.ToLower(flat), "@"+handles[0])
	if i < 0 {
		i = 0
	}
	r := []rune(flat)
	start := len([]rune(flat[:i])) - 120
	if start < 0 {
		start = 0
	}
	end := start + 300
	if end > len(r) {
		end = len(r)
	}
	out := string(r[start:end])
	if start > 0 {
		out = "… " + out
	}
	if end < len(r) {
		out += " …"
	}
	return out
}
