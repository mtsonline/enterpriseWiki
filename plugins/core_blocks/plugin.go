// Package core_blocks provides content blocks at template positions: text in
// Markdown or HTML (sanitized like pages, macros allowed) per language, shown
// to everyone, only to guests or only to signed-in users.
package core_blocks

import (
	"context"
	"embed"
	"errors"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
	"github.com/mtsonline/enterprisewiki/plugins/core_html"

	_ "embed"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// Plugin is the blocks plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Block is a stored block.
type Block struct {
	ID       int64                     `json:"id"`
	Name     string                    `json:"name"`
	Location string                    `json:"location"`
	Position int                       `json:"position"`
	Audience string                    `json:"audience"`
	Enabled  bool                      `json:"enabled"`
	Titles   map[string]string         `json:"titles"`
	Bodies   map[string]core_html.Body `json:"bodies"`
	Updated  time.Time                 `json:"updated_at"`
}

// Rendered is a block as shown to a visitor.
type Rendered struct {
	ID       int64  `json:"id"`
	Location string `json:"location"`
	Position int    `json:"position"`
	Title    string `json:"title"`
	Lang     string `json:"lang"`
	HTML     string `json:"html"`
}

var locRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,40}$`)

func (b *Block) validate(langs []string) error {
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" || utf8.RuneCountInString(b.Name) > 200 {
		return apperr.BadRequest("a name (max. 200 characters) is required")
	}
	if !locRe.MatchString(b.Location) {
		return apperr.BadRequest("invalid position %q", b.Location)
	}
	if b.Audience == "" {
		b.Audience = "all"
	}
	if b.Audience != "all" && b.Audience != "users" && b.Audience != "guests" {
		return apperr.BadRequest("audience must be all, users or guests")
	}
	ok := map[string]bool{}
	for _, l := range langs {
		ok[l] = true
	}
	titles := map[string]string{}
	for l, v := range b.Titles {
		if v = strings.TrimSpace(v); v != "" && ok[l] {
			if utf8.RuneCountInString(v) > 200 {
				return apperr.BadRequest("titles have at most 200 characters")
			}
			titles[l] = v
		}
	}
	bodies := map[string]core_html.Body{}
	for l, v := range b.Bodies {
		if !ok[l] || strings.TrimSpace(v.Source) == "" {
			continue
		}
		if v.Format != "html" {
			v.Format = "markdown"
		}
		if len(v.Source) > 200000 {
			return apperr.BadRequest("a block may have at most 200 KB per language")
		}
		bodies[l] = v
	}
	if len(bodies) == 0 {
		return apperr.BadRequest("enter the content of the block in at least one language")
	}
	b.Titles, b.Bodies = titles, bodies
	return nil
}

func list(ctx context.Context, q *sdk.Host) ([]Block, error) {
	rows, err := q.DB.Query(ctx, `SELECT id, name, location, position, audience, enabled, titles, bodies, updated_at FROM plugin_blocks ORDER BY location, position, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Block])
}

// Setup registers the API.
func (p *Plugin) Setup(host *sdk.Host) error {
	// GET /blocks?lang=de lists the blocks for the caller, rendered
	// (?all=1: all blocks with their sources, administrators only).
	host.Handle("GET", "/blocks", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		bs, err := list(ctx, host)
		if err != nil {
			return nil, err
		}
		if r.URL.Query().Get("all") == "1" {
			if err := perm.Require(r.Context(), "core.blocks.edit"); err != nil {
				return nil, err
			}
			return bs, nil
		}
		me := identity.From(ctx)
		lang := r.URL.Query().Get("lang")
		def := host.Config.Get().I18n.DefaultLanguage
		out := []Rendered{}
		for _, b := range bs {
			if !b.Enabled || (b.Audience == "users" && me == nil) || (b.Audience == "guests" && me != nil) {
				continue
			}
			shown := lang
			body, ok := b.Bodies[lang]
			if !ok {
				shown = def
				if body, ok = b.Bodies[def]; !ok {
					for l, v := range b.Bodies {
						shown, body = l, v
						break
					}
				}
			}
			title := b.Titles[shown]
			if t, ok := b.Titles[lang]; ok {
				title = t
			}
			html := core_html.ToHTML(&body)
			html = host.Content.RenderMacros(ctx, content.RenderContext{Lang: shown, Principal: me, Service: host.Content}, html)
			out = append(out, Rendered{ID: b.ID, Location: b.Location, Position: b.Position, Title: title, Lang: shown, HTML: html})
		}
		return out, nil
	})
	save := func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := perm.Require(r.Context(), "core.blocks.edit"); err != nil {
			return nil, err
		}
		var in Block
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := in.validate(host.Config.Get().I18n.Languages); err != nil {
			return nil, err
		}
		ctx := r.Context()
		if id := r.PathValue("id"); id != "" {
			bid, err := h.PathID(r, "id")
			if err != nil {
				return nil, err
			}
			tag, err := host.DB.Exec(ctx, `UPDATE plugin_blocks SET name=$2, location=$3, position=$4, audience=$5, enabled=$6, titles=$7, bodies=$8, updated_at=now() WHERE id=$1`,
				bid, in.Name, in.Location, in.Position, in.Audience, in.Enabled, in.Titles, in.Bodies)
			if err != nil {
				return nil, err
			}
			if tag.RowsAffected() == 0 {
				return nil, apperr.NotFound("block")
			}
			in.ID = bid
			_ = audit.Log(ctx, host.DB, "block.update", "block", bid, nil, in)
			return in, nil
		}
		if err := host.DB.QueryRow(ctx, `INSERT INTO plugin_blocks(name, location, position, audience, enabled, titles, bodies) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			in.Name, in.Location, in.Position, in.Audience, in.Enabled, in.Titles, in.Bodies).Scan(&in.ID); err != nil {
			return nil, err
		}
		_ = audit.Log(ctx, host.DB, "block.create", "block", in.ID, nil, in)
		return h.Created{V: in}, nil
	}
	host.Handle("POST", "/blocks", save)
	host.Handle("PUT", "/blocks/{id}", save)
	host.Handle("DELETE", "/blocks/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := perm.Require(r.Context(), "core.blocks.edit"); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var b Block
		err = host.DB.QueryRow(r.Context(), `DELETE FROM plugin_blocks WHERE id=$1 RETURNING name, location, titles, bodies`, id).Scan(&b.Name, &b.Location, &b.Titles, &b.Bodies)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.NotFound("block")
		}
		if err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), host.DB, "block.delete", "block", id, b, nil)
		return h.NoContent{}, nil
	})
	return nil
}
