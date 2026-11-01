// Package core_menu provides menus. A menu is placed at a position offered
// by the template (header, sidebar, footer, ...) and contains nested items
// that point to a URL or to a category. Category items are only shown to
// users who may view that category.
package core_menu

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// Plugin is the menu plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { sub, _ := fs.Sub(migrations, "migrations"); return sub }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { sub, _ := fs.Sub(locales, "locales"); return sub }

// Item is a menu entry.
type Item struct {
	Label      map[string]string `json:"label"`
	URL        string            `json:"url,omitempty"`
	CategoryID int64             `json:"category_id,omitempty"`
	Children   []Item            `json:"children,omitempty"`
}

// Menu is a menu.
type Menu struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Location  string    `json:"location"`
	Position  int       `json:"position"`
	Items     []Item    `json:"items"`
	UpdatedAt time.Time `json:"updated_at"`
}

var locRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,40}$`)

func validate(items []Item, depth int) error {
	if depth > 4 {
		return apperr.BadRequest("menus can be nested at most 4 levels")
	}
	for i := range items {
		it := &items[i]
		if len(it.Label) == 0 {
			return apperr.BadRequest("every menu item needs a label")
		}
		if it.URL != "" {
			u, err := url.Parse(it.URL)
			if err != nil || !(u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto" || (u.Scheme == "" && strings.HasPrefix(it.URL, "/"))) {
				return apperr.BadRequest("invalid menu URL %q (allowed: http, https, mailto or /path)", it.URL)
			}
		}
		if err := validate(it.Children, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func filter(ctx context.Context, a *acl.Service, items []Item) []Item {
	out := []Item{}
	p := identity.From(ctx)
	for _, it := range items {
		if it.CategoryID != 0 && !a.Can(ctx, p, it.CategoryID, acl.View) {
			continue
		}
		it.Children = filter(ctx, a, it.Children)
		out = append(out, it)
	}
	return out
}

func list(ctx context.Context, hs *sdk.Host, location string) ([]Menu, error) {
	rows, err := hs.DB.Query(ctx, `SELECT id, name, location, position, items, updated_at FROM plugin_menus
		WHERE $1 = '' OR location = $1 ORDER BY location, position, id`, location)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Menu, error) {
		var m Menu
		return m, r.Scan(&m.ID, &m.Name, &m.Location, &m.Position, &m.Items, &m.UpdatedAt)
	})
}

// Setup registers the API.
func (p *Plugin) Setup(hs *sdk.Host) error {
	// Public: menus of a location, filtered by the caller's permissions.
	hs.Handle("GET", "/menus", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ms, err := list(r.Context(), hs, r.URL.Query().Get("location"))
		if err != nil {
			return nil, err
		}
		edit := r.URL.Query().Get("all") == "1"
		if edit {
			if err := sdk.RequireAdmin(r); err != nil {
				return nil, err
			}
			return ms, nil
		}
		for i := range ms {
			ms[i].Items = filter(r.Context(), hs.ACL, ms[i].Items)
		}
		return ms, nil
	})
	save := func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := sdk.RequireAdmin(r); err != nil {
			return nil, err
		}
		var in Menu
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || !locRe.MatchString(in.Location) {
			return nil, apperr.BadRequest("name and a valid location (e.g. header, sidebar, footer) are required")
		}
		if in.Items == nil {
			in.Items = []Item{}
		}
		if err := validate(in.Items, 1); err != nil {
			return nil, err
		}
		items, _ := json.Marshal(in.Items)
		ctx := r.Context()
		if id := r.PathValue("id"); id != "" {
			mid, err := h.PathID(r, "id")
			if err != nil {
				return nil, err
			}
			tag, err := hs.DB.Exec(ctx, `UPDATE plugin_menus SET name=$2, location=$3, position=$4, items=$5, updated_at=now() WHERE id=$1`,
				mid, in.Name, in.Location, in.Position, items)
			if err != nil {
				return nil, err
			}
			if tag.RowsAffected() == 0 {
				return nil, apperr.NotFound("menu")
			}
			in.ID = mid
		} else if err := hs.DB.QueryRow(ctx, `INSERT INTO plugin_menus(name, location, position, items) VALUES ($1,$2,$3,$4) RETURNING id`,
			in.Name, in.Location, in.Position, items).Scan(&in.ID); err != nil {
			return nil, err
		}
		_ = audit.Log(ctx, hs.DB, "menu.save", "menu", in.ID, nil, in)
		return in, nil
	}
	hs.Handle("POST", "/menus", save)
	hs.Handle("PUT", "/menus/{id}", save)
	hs.Handle("DELETE", "/menus/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := sdk.RequireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		tag, err := hs.DB.Exec(r.Context(), `DELETE FROM plugin_menus WHERE id=$1`, id)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, apperr.NotFound("menu")
		}
		_ = audit.Log(r.Context(), hs.DB, "menu.delete", "menu", id, nil, nil)
		return h.NoContent{}, nil
	})
	return nil
}
