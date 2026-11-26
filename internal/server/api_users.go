package server

import (
	"net/http"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// routesUsers registers the routes of administration of users, groups and roles.
func (s *Server) routesUsers() {
	// action: core.users.list
	s.handle("GET /api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Auth.ListUsers(r.Context(), r.URL.Query().Get("q"), h.QueryInt(r, "limit", 100), h.QueryInt(r, "offset", 0))
	})
	// action: core.users.create
	s.handle("POST /api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in auth.NewUser
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		u, err := s.app.Auth.CreateLocalUser(r.Context(), in)
		return h.Created{V: u}, err
	})
	// action: core.users.get
	s.handle("GET /api/v1/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Auth.GetUser(r.Context(), id)
	})
	// action: core.users.update
	s.handle("PUT /api/v1/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in auth.UserUpdate
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Auth.UpdateUser(r.Context(), id, in)
	})
	// action: core.users.delete
	s.handle("DELETE /api/v1/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if p := identity.From(r.Context()); p != nil && p.UserID == id {
			return nil, apperr.BadRequest("you cannot delete your own account")
		}
		return h.NoContent{}, s.app.Auth.DeleteUser(r.Context(), id)
	})
	// action: core.groups.names
	s.handle("GET /api/v1/groups", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Auth.ListGroups(r.Context()) })
	// action: core.roles.names
	s.handle("GET /api/v1/roles", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Auth.ListRoles(r.Context()) })
	// action: core.users.lookup
	s.handle("GET /api/v1/users/lookup", func(w http.ResponseWriter, r *http.Request) (any, error) {
		// Minimal user search for permission, share and mention dialogs:
		// all users (core.users.list_all) or those of the own groups
		// (core.users.list_own).
		me, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		scope := auth.ListScope(me, "users")
		if scope == auth.ListNone {
			return nil, apperr.Forbidden("finding users needs the permission core.users.list_all or core.users.list_own")
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if len(q) < 2 {
			return []any{}, nil
		}
		like := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q)
		rows, err := s.app.DB.Query(r.Context(), `SELECT id, username || '@' || provider, display_name FROM users u
			WHERE NOT disabled AND (username ILIKE $1 || '%' OR username || '@' || provider ILIKE $1 || '%' OR display_name ILIKE '%' || $1 || '%')
			AND ($2 OR u.id = $3 OR EXISTS (SELECT 1 FROM user_groups a JOIN user_groups b ON a.group_id = b.group_id WHERE a.user_id = u.id AND b.user_id = $3))
			ORDER BY username LIMIT 20`, like, scope == auth.ListAll, me.UserID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id int64
			var handle, name string
			if err := rows.Scan(&id, &handle, &name); err != nil {
				return nil, err
			}
			out = append(out, map[string]any{"id": id, "handle": handle, "display_name": name})
		}
		return out, rows.Err()
	})
	groupSave := func(w http.ResponseWriter, r *http.Request) (any, error) {
		var id int64
		if r.PathValue("id") != "" {
			var err error
			if id, err = h.PathID(r, "id"); err != nil {
				return nil, err
			}
		}
		var in struct {
			Name        string  `json:"name"`
			Description string  `json:"description"`
			Roles       []int64 `json:"roles"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		nid, err := s.app.Auth.SaveGroup(r.Context(), id, in.Name, in.Description, in.Roles)
		if r.Method == http.MethodPost {
			return h.Created{V: map[string]int64{"id": nid}}, err
		}
		return map[string]int64{"id": nid}, err
	}
	// action: core.groups.create
	s.handle("POST /api/v1/admin/groups", groupSave)
	// action: core.groups.update
	s.handle("PUT /api/v1/admin/groups/{id}", groupSave)
	// action: core.groups.delete
	s.handle("DELETE /api/v1/admin/groups/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.DeleteGroup(r.Context(), id)
	})
	// action: core.groups.members.list
	s.handle("GET /api/v1/admin/groups/{id}/members", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Auth.GroupMembers(r.Context(), id)
	})
	// PUT/DELETE /api/v1/admin/groups/{id}/members/{user}: one person into or out of a group
	memberSet := func(member bool) h.Handler {
		return func(w http.ResponseWriter, r *http.Request) (any, error) {
			if err := requireAdmin(r); err != nil {
				return nil, err
			}
			gid, err := h.PathID(r, "id")
			if err != nil {
				return nil, err
			}
			uid, err := h.PathID(r, "user")
			if err != nil {
				return nil, err
			}
			_, err = s.app.Auth.SetGroupMember(r.Context(), gid, uid, member)
			return h.NoContent{}, err
		}
	}
	// action: core.groups.members.add
	s.handle("PUT /api/v1/admin/groups/{id}/members/{user}", memberSet(true))
	// action: core.groups.members.remove
	s.handle("DELETE /api/v1/admin/groups/{id}/members/{user}", memberSet(false))
	roleSave := func(w http.ResponseWriter, r *http.Request) (any, error) {
		var id int64
		if r.PathValue("id") != "" {
			var err error
			if id, err = h.PathID(r, "id"); err != nil {
				return nil, err
			}
		}
		var in auth.RoleInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		nid, err := s.app.Auth.SaveRole(r.Context(), id, in)
		if r.Method == http.MethodPost {
			return h.Created{V: map[string]int64{"id": nid}}, err
		}
		return map[string]int64{"id": nid}, err
	}
	// global permissions: the declared permissions and the defaults of every signed-in user
	// action: core.permissions.global
	s.handle("GET /api/v1/admin/permissions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		defaults, err := s.app.Auth.DefaultPermissions(r.Context())
		return map[string]any{"permissions": perm.All(), "authenticated": defaults}, err
	})
	// action: core.permissions.defaults
	s.handle("PUT /api/v1/admin/permissions/authenticated", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		var in struct {
			Permissions []string `json:"permissions"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.SetDefaultPermissions(r.Context(), in.Permissions)
	})
	// action: core.roles.create
	s.handle("POST /api/v1/admin/roles", roleSave)
	// action: core.roles.update
	s.handle("PUT /api/v1/admin/roles/{id}", roleSave)
	// action: core.roles.delete
	s.handle("DELETE /api/v1/admin/roles/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.DeleteRole(r.Context(), id)
	})
}
