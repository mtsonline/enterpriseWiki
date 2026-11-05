// Package core_favorites lets signed-in users mark pages and categories as
// favorites. The list only shows what the user may (still) see.
package core_favorites

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
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

// Max is the maximum number of favorites per user.
const Max = 200

// Plugin is the favorites plugin.
type Plugin struct{ host *sdk.Host }

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Favorite is one entry of the list.
type Favorite struct {
	Type   string            `json:"type"` // element | category
	ID     int64             `json:"id"`
	Titles map[string]string `json:"titles"`
	URL    string            `json:"url"`
}

type item struct {
	Type string `json:"type"`
	ID   int64  `json:"id"`
}

func me(ctx context.Context) (int64, error) {
	p := identity.From(ctx)
	if p == nil || p.ID() == 0 {
		return 0, apperr.ErrUnauthorized
	}
	if !perm.Has(ctx, "core.favorites.use") {
		return 0, apperr.ErrForbidden
	}
	return p.ID(), nil
}

// visible checks that the caller may view the target.
func (p *Plugin) visible(ctx context.Context, typ string, id int64) error {
	switch typ {
	case "element":
		_, err := p.host.Content.ElementCategory(ctx, id, acl.View)
		return err
	case "category":
		return p.host.Content.Require(ctx, id, acl.View)
	}
	return apperr.BadRequest("type must be element or category")
}

// List is the answer of all routes: the favorites and where the list is shown.
type List struct {
	Items []Favorite `json:"items"`
	// Position of the template where the list is shown (setting "position", default sidebar; "" = only in the profile)
	Position string `json:"position"`
}

func (p *Plugin) list(ctx context.Context, uid int64) (*List, error) {
	items, err := p.items(ctx, uid)
	if err != nil {
		return nil, err
	}
	pos, ok := p.host.Settings()["position"].(string)
	if !ok {
		pos = "sidebar"
	}
	if pos == "none" {
		pos = ""
	}
	return &List{Items: items, Position: pos}, nil
}

// items returns the visible favorites of the caller in their order.
func (p *Plugin) items(ctx context.Context, uid int64) ([]Favorite, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT f.target_type, f.target_id,
			COALESCE(CASE f.target_type WHEN 'element' THEN (SELECT e.titles FROM elements e WHERE e.id=f.target_id AND e.deleted_at IS NULL)
				ELSE (SELECT c.titles FROM categories c WHERE c.id=f.target_id AND c.deleted_at IS NULL) END, 'null'::jsonb)
		FROM plugin_favorites f WHERE f.user_id=$1 ORDER BY f.position, f.created_at`, uid)
	if err != nil {
		return nil, err
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Favorite, error) {
		var f Favorite
		return f, r.Scan(&f.Type, &f.ID, &f.Titles)
	})
	if err != nil {
		return nil, err
	}
	out := make([]Favorite, 0, len(all))
	for _, f := range all {
		if f.Titles == nil || p.visible(ctx, f.Type, f.ID) != nil {
			continue // deleted or no longer visible (kept, it may come back)
		}
		f.URL = p.host.Content.Path(ctx, f.Type, f.ID)
		if f.URL == "" {
			f.URL = fmt.Sprintf("/%s/%d", f.Type[:1], f.ID)
		}
		out = append(out, f)
	}
	return out, nil
}

// Setup implements sdk.SetupPlugin.
func (p *Plugin) Setup(host *sdk.Host) error {
	p.host = host
	// GET / – my favorites
	host.Handle("GET", "/", func(w http.ResponseWriter, r *http.Request) (any, error) {
		uid, err := me(r.Context())
		if err != nil {
			return nil, err
		}
		return p.list(r.Context(), uid)
	})
	// POST / {"type": "element", "id": 12} – add (at the end)
	host.Handle("POST", "/", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		uid, err := me(ctx)
		if err != nil {
			return nil, err
		}
		var in item
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := p.visible(ctx, in.Type, in.ID); err != nil {
			return nil, err
		}
		var n int
		if err := host.DB.QueryRow(ctx, `SELECT count(*) FROM plugin_favorites WHERE user_id=$1`, uid).Scan(&n); err != nil {
			return nil, err
		}
		if n >= Max {
			return nil, apperr.BadRequest("at most %d favorites", Max)
		}
		if _, err := host.DB.Exec(ctx, `INSERT INTO plugin_favorites(user_id, target_type, target_id, position)
			VALUES ($1,$2,$3,(SELECT COALESCE(max(position)+1, 0) FROM plugin_favorites WHERE user_id=$1)) ON CONFLICT DO NOTHING`, uid, in.Type, in.ID); err != nil {
			return nil, err
		}
		return p.list(ctx, uid)
	})
	// DELETE /{type}/{id} – remove
	host.Handle("DELETE", "/{type}/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		uid, err := me(ctx)
		if err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if _, err := host.DB.Exec(ctx, `DELETE FROM plugin_favorites WHERE user_id=$1 AND target_type=$2 AND target_id=$3`, uid, r.PathValue("type"), id); err != nil {
			return nil, err
		}
		return p.list(ctx, uid)
	})
	// PUT /order {"items": [{"type": "element", "id": 12}, …]} – new order
	host.Handle("PUT", "/order", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		uid, err := me(ctx)
		if err != nil {
			return nil, err
		}
		var in struct {
			Items []item `json:"items"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		err = pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
			for i, it := range in.Items {
				if _, err := tx.Exec(ctx, `UPDATE plugin_favorites SET position=$4 WHERE user_id=$1 AND target_type=$2 AND target_id=$3`, uid, it.Type, it.ID, i); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return p.list(ctx, uid)
	})
	return nil
}
