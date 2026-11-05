package server

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// requireInstance allows instance administrators only (on the site "main"
// also its administrators, who are instance administrators).
func requireInstance(r *http.Request) error {
	p := identity.From(r.Context())
	if p == nil {
		return apperr.ErrUnauthorized
	}
	if !p.Instance() {
		return apperr.Forbidden("only instance administrators can do this")
	}
	return nil
}

// InstanceAdmin is a user with instance administration rights.
type InstanceAdmin struct {
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	// Implicit: administrator of the site main (not removable here).
	Implicit bool `json:"implicit"`
}

func (s *Server) routesSites() {
	in := s.app.Instance
	inst := func(fn func(w http.ResponseWriter, r *http.Request) (any, error)) func(w http.ResponseWriter, r *http.Request) (any, error) {
		return func(w http.ResponseWriter, r *http.Request) (any, error) {
			if err := requireInstance(r); err != nil {
				return nil, err
			}
			if in == nil {
				return nil, apperr.NotFound("instance")
			}
			return fn(w, r)
		}
	}
	s.handle("GET /api/v1/instance/sites", inst(func(w http.ResponseWriter, r *http.Request) (any, error) {
		return in.ListSites(r.Context())
	}))
	s.handle("POST /api/v1/instance/sites", inst(func(w http.ResponseWriter, r *http.Request) (any, error) {
		var body app.SiteInput
		if err := h.Decode(r, &body); err != nil {
			return nil, err
		}
		si, err := in.CreateSite(r.Context(), body)
		if err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), s.app.DB, "site.create", "site", 0, nil, si)
		return h.Created{V: si}, nil
	}))
	s.handle("PATCH /api/v1/instance/sites/{name}", inst(func(w http.ResponseWriter, r *http.Request) (any, error) {
		var body app.SiteInput
		if err := h.Decode(r, &body); err != nil {
			return nil, err
		}
		body.Name = r.PathValue("name")
		before, err := in.GetSite(r.Context(), body.Name)
		if err != nil {
			return nil, err
		}
		si, err := in.UpdateSite(r.Context(), body)
		if err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), s.app.DB, "site.update", "site", 0, before, si)
		return si, nil
	}))
	s.handle("DELETE /api/v1/instance/sites/{name}", inst(func(w http.ResponseWriter, r *http.Request) (any, error) {
		name := r.PathValue("name")
		if name == s.app.Site {
			return nil, apperr.BadRequest("a site cannot delete itself")
		}
		if err := in.DeleteSite(r.Context(), name); err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), s.app.DB, "site.delete", "site", 0, map[string]string{"name": name}, nil)
		return h.NoContent{}, nil
	}))
	s.handle("GET /api/v1/instance/admins", inst(func(w http.ResponseWriter, r *http.Request) (any, error) {
		rows, err := in.Shared.Query(r.Context(), `SELECT u.id, u.username, u.display_name, NOT EXISTS(SELECT 1 FROM instance_admins a WHERE a.user_id=u.id)
			FROM users u WHERE u.id IN (SELECT user_id FROM instance_admins)
			   OR u.id IN (SELECT ur.user_id FROM site_main.user_roles ur JOIN roles ro ON ro.id=ur.role_id WHERE ro.is_admin)
			   OR u.id IN (SELECT ug.user_id FROM user_groups ug JOIN site_main.group_roles gr ON gr.group_id=ug.group_id JOIN roles ro ON ro.id=gr.role_id WHERE ro.is_admin)
			ORDER BY lower(u.display_name), u.username`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (InstanceAdmin, error) {
			var a InstanceAdmin
			err := row.Scan(&a.UserID, &a.Username, &a.DisplayName, &a.Implicit)
			return a, err
		})
	}))
	s.handle("POST /api/v1/instance/admins", inst(func(w http.ResponseWriter, r *http.Request) (any, error) {
		var body struct {
			UserID int64 `json:"user_id"`
		}
		if err := h.Decode(r, &body); err != nil {
			return nil, err
		}
		tag, err := in.Shared.Exec(r.Context(), `INSERT INTO instance_admins(user_id) SELECT id FROM users WHERE id=$1 ON CONFLICT DO NOTHING`, body.UserID)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() > 0 {
			_ = audit.Log(r.Context(), s.app.DB, "instance.admin.add", "user", body.UserID, nil, nil)
			s.app.Bus.Publish(r.Context(), "auth", "")
		}
		return h.NoContent{}, nil
	}))
	s.handle("DELETE /api/v1/instance/admins/{id}", inst(func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if id == identity.From(r.Context()).UserID {
			return nil, apperr.BadRequest("you cannot remove yourself")
		}
		tag, err := in.Shared.Exec(r.Context(), `DELETE FROM instance_admins WHERE user_id=$1`, id)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() > 0 {
			_ = audit.Log(r.Context(), s.app.DB, "instance.admin.remove", "user", id, nil, nil)
			s.app.Bus.Publish(r.Context(), "auth", "")
		}
		return h.NoContent{}, nil
	}))
}
