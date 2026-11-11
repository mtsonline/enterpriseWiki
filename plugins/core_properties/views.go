package core_properties

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

// Saved views of a property database: personal ones and shared ones
// (saved by people who may edit the database for all its readers).

// ViewState is what a view restores.
type ViewState struct {
	Search  string    `json:"search"`
	View    string    `json:"view"`
	Group   string    `json:"group"`
	Filters []Filter  `json:"filters"`
	Sort    []SortKey `json:"sort"`
}

// SavedView is a saved view of a database.
type SavedView struct {
	ID     int64     `json:"id"`
	Name   string    `json:"name"`
	Shared bool      `json:"shared"`
	Mine   bool      `json:"mine"`
	State  ViewState `json:"state"`
}

const maxViewsPerDatabase = 100

func viewRoutes(host *sdk.Host) {
	// database element: readers may see its views
	dbCategory := func(ctx context.Context, id int64, want acl.Perm) (int64, error) {
		var typ string
		var cat int64
		err := host.DB.QueryRow(ctx, `SELECT type, category_id FROM elements WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&typ, &cat)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && typ != "content.propdb") {
			return 0, apperr.NotFound("database")
		}
		if err != nil {
			return 0, err
		}
		if !host.ACL.Can(ctx, identity.From(ctx), cat, acl.View) {
			return 0, apperr.NotFound("database")
		}
		if want != acl.View && !host.ACL.Can(ctx, identity.From(ctx), cat, want) {
			return 0, apperr.ErrForbidden
		}
		return cat, nil
	}
	// GET /databases/{id}/views – own and shared views
	host.Handle("GET", "/databases/{id}/views", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		if _, err := dbCategory(ctx, id, acl.View); err != nil {
			return nil, err
		}
		uid := identity.From(ctx).ID()
		rows, err := host.DB.Query(ctx, `SELECT id, name, owner_id IS NULL, COALESCE(owner_id = $2, false), state FROM plugin_prop_views
			WHERE element_id=$1 AND (owner_id IS NULL OR owner_id = $2) ORDER BY owner_id IS NOT NULL, name, id`, id, uid)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SavedView, error) {
			var v SavedView
			var raw []byte
			if err := r.Scan(&v.ID, &v.Name, &v.Shared, &v.Mine, &raw); err != nil {
				return v, err
			}
			_ = json.Unmarshal(raw, &v.State)
			return v, nil
		})
	})
	// POST /databases/{id}/views {"name": "…", "shared": false, "state": {...}}
	host.Handle("POST", "/databases/{id}/views", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		me := identity.From(ctx)
		if me == nil || me.ID() <= 0 {
			return nil, apperr.ErrUnauthorized
		}
		var in struct {
			Name   string    `json:"name"`
			Shared bool      `json:"shared"`
			State  ViewState `json:"state"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 {
			return nil, apperr.BadRequest("a name of 1–100 characters is required")
		}
		if utf8.RuneCountInString(in.State.Search) > 200 || len(in.State.Filters) > 10 || len(in.State.Sort) > 5 || len(in.State.Group) > 64 || len(in.State.View) > 20 {
			return nil, apperr.BadRequest("view too large")
		}
		want := acl.View
		if in.Shared {
			want = acl.Edit
		}
		if _, err := dbCategory(ctx, id, want); err != nil {
			return nil, err
		}
		var n int
		if err := host.DB.QueryRow(ctx, `SELECT count(*) FROM plugin_prop_views WHERE element_id=$1`, id).Scan(&n); err != nil {
			return nil, err
		}
		if n >= maxViewsPerDatabase {
			return nil, apperr.BadRequest("at most %d views per database", maxViewsPerDatabase)
		}
		var owner *int64
		if !in.Shared {
			uid := me.ID()
			owner = &uid
		}
		state, _ := json.Marshal(in.State)
		v := SavedView{Name: in.Name, Shared: in.Shared, Mine: !in.Shared, State: in.State}
		if err := host.DB.QueryRow(ctx, `INSERT INTO plugin_prop_views(element_id, owner_id, name, state, created_by) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			id, owner, in.Name, state, me.ID()).Scan(&v.ID); err != nil {
			return nil, err
		}
		return h.Created{V: v}, nil
	})
	// DELETE /views/{id} – own views; shared ones with the permission to edit the database
	host.Handle("DELETE", "/views/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		var el int64
		var owner *int64
		if err := host.DB.QueryRow(ctx, `SELECT element_id, owner_id FROM plugin_prop_views WHERE id=$1`, id).Scan(&el, &owner); err != nil {
			return nil, apperr.NotFound("view")
		}
		if owner != nil && *owner != identity.From(ctx).ID() {
			return nil, apperr.NotFound("view")
		}
		want := acl.View
		if owner == nil {
			want = acl.Edit
		}
		if _, err := dbCategory(ctx, el, want); err != nil {
			return nil, err
		}
		_, err = host.DB.Exec(ctx, `DELETE FROM plugin_prop_views WHERE id=$1`, id)
		return h.NoContent{}, err
	})
}
