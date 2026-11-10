package server

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/i18n"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/media"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
	"github.com/mtsonline/enterprisewiki/internal/version"
)

func (s *Server) handle(pattern string, fn h.Handler) {
	h.RecordRoute(pattern)
	s.app.Mux.HandleFunc(pattern, h.JSON(fn))
}

// adminPerms maps administrative routes to the global permission they need
// (longest prefix wins). Routes without an entry are for administrators only.
var adminPerms = []struct{ prefix, perm string }{
	{"/api/v1/admin/users", "core.users.manage"},
	{"/api/v1/admin/groups", "core.groups.manage"},
	{"/api/v1/admin/roles", "core.roles.manage"},
	{"/api/v1/admin/permissions", "core.roles.manage"},
	{"/api/v1/admin/settings", "core.settings.manage"},
	{"/api/v1/admin/languages", "core.i18n.manage"},
	{"/api/v1/admin/retention", "core.settings.manage"},
	{"/api/v1/admin/audit/settings", "core.audit.manage"},
	{"/api/v1/admin/audit/forward", "core.audit.manage"},
	{"/api/v1/admin/audit", "core.audit.view"},
	{"/api/v1/admin/plugins", "core.plugins.manage"},
	{"/api/v1/admin/restart", "core.plugins.manage"},
	{"/api/v1/admin/updates", "core.plugins.manage"},
	{"/api/v1/admin/jobs", "core.plugins.manage"},
	{"/api/v1/admin/mail", "core.mail.manage"},
	{"/api/v1/admin/webhooks", "core.webhooks.manage"},
	{"/api/v1/admin/watch-assignments", "core.watches.assign"},
	{"/api/v1/admin/license", "core.license.manage"},
	{"/api/v1/admin/cache", "core.system.manage"},
	{"/api/v1/admin/database", "core.system.manage"},
	// /api/v1/admin/i18n: checked per language by the i18n service (core.i18n.manage or core.i18n.translate.<lang>)
	{"/api/v1/prerender", "core.settings.manage"},
}

// routePerm returns the permission of an administrative route pattern.
func routePerm(pattern string) string {
	_, path, _ := strings.Cut(pattern, " ")
	best := ""
	key := ""
	for _, e := range adminPerms {
		if strings.HasPrefix(path, e.prefix) && len(e.prefix) > len(best) {
			best, key = e.prefix, e.perm
		}
	}
	return key
}

// requireAdmin checks the permission of the requested administrative route
// (see adminPerms); administrators may use all of them.
func requireAdmin(r *http.Request) error {
	p := identity.From(r.Context())
	if p == nil {
		return apperr.ErrUnauthorized
	}
	if key := routePerm(r.Pattern); key != "" {
		return perm.Require(r.Context(), key)
	}
	if !p.Admin() {
		return apperr.ErrForbidden
	}
	return nil
}

func requireUser(r *http.Request) (*identity.Principal, error) {
	p := identity.From(r.Context())
	if p == nil {
		return nil, apperr.ErrUnauthorized
	}
	return p, nil
}

func (s *Server) routes() {
	s.routesM2()
	s.routesMedia()
	s.routesOffice()
	s.routesDemo()
	s.routesM3()
	s.routesM4()
	s.routesM6()
	s.routesM7()
	s.routesM8()
	s.routesSites()
	s.routesM9()
	s.routesM15()
	s.routesAudit()
	s.routesDrafts()
	s.routesNotifications()
	s.routesWatchAssignments()
	// ---------------------------------------------------------------- system
	s.app.Mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { h.Write(w, 200, map[string]string{"status": "ok"}) })
	// on-demand TLS of the bundled reverse proxy (Caddy "ask"): 200 only for
	// the wiki's own host names, so no certificates are requested for others
	s.app.Mux.HandleFunc("GET /tls-allowed", func(w http.ResponseWriter, r *http.Request) {
		ok, err := s.app.Instance.AllowedHost(r.Context(), r.URL.Query().Get("domain"))
		switch {
		case err != nil:
			http.Error(w, "error", http.StatusInternalServerError)
		case ok:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unknown host", http.StatusNotFound)
		}
	})
	s.handle("GET /readyz", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := s.app.DB.Ping(r.Context()); err != nil {
			return nil, &apperr.Error{Status: 503, Code: "db_unavailable", Msg: "database unavailable"}
		}
		return map[string]string{"status": "ready"}, nil
	})
	s.handle("GET /api/v1/site", s.site)

	// ---------------------------------------------------------------- auth
	s.handle("GET /api/v1/auth/providers", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Auth.ProviderInfos(), nil
	})
	s.handle("POST /api/v1/auth/login", s.login)
	s.handle("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if p := identity.From(r.Context()); p != nil {
			_ = audit.Log(r.Context(), s.app.DB, "auth.logout", "user", p.ID(), nil, nil)
		}
		if c, err := r.Cookie(sessionCookie); err == nil {
			_ = s.app.Auth.Logout(r.Context(), c.Value)
		}
		if a, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && !strings.HasPrefix(a, "ewk_") {
			_ = s.app.Auth.Logout(r.Context(), a)
		}
		s.clearCookie(w)
		return h.NoContent{}, nil
	})
	s.handle("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p := identity.From(r.Context())
		if p == nil {
			return map[string]any{"user": nil}, nil
		}
		return map[string]any{"user": p, "handle": p.Handle(), "manages_categories": s.app.ACL.Any(r.Context(), p, acl.Manage)}, nil
	})
	s.handle("POST /api/v1/auth/ssh/challenge", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Username string `json:"username"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		c, err := s.app.Auth.SSHChallenge(r.Context(), in.Username)
		if err != nil {
			return nil, err
		}
		return map[string]string{"challenge": c, "namespace": auth.SSHNamespace,
			"hint": "printf %s CHALLENGE | ssh-keygen -Y sign -f ~/.ssh/id_ed25519 -n " + auth.SSHNamespace}, nil
	})
	s.handle("POST /api/v1/auth/ssh/verify", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Challenge string `json:"challenge"`
			Signature string `json:"signature"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		res, err := s.app.Auth.SSHLogin(r.Context(), in.Challenge, in.Signature, s.clientIP(r), r.UserAgent())
		if err != nil {
			return nil, err
		}
		return map[string]any{"token": res.Token, "expires_at": res.ExpiresAt, "user": res.Principal}, nil
	})

	// ---------------------------------------------------------------- me
	s.handle("PUT /api/v1/me", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			DisplayName *string `json:"display_name"`
			Email       *string `json:"email"`
			Language    *string `json:"language"`
			// Prefs changes personal settings ("" removes one).
			Prefs map[string]string `json:"prefs"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := s.app.Auth.UpdateProfile(r.Context(), in.DisplayName, in.Email, in.Language); err != nil {
			return nil, err
		}
		if in.Prefs != nil {
			if err := s.app.Auth.SetPrefs(r.Context(), in.Prefs); err != nil {
				return nil, err
			}
		}
		return h.NoContent{}, nil
	})
	s.handle("GET /api/v1/me/prefs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p := identity.From(r.Context())
		if p == nil || p.UserID == 0 {
			return nil, apperr.ErrUnauthorized
		}
		cur := p.Prefs
		if cur == nil {
			cur = map[string]string{}
		}
		return map[string]any{"prefs": cur, "options": auth.PrefOptions()}, nil
	})
	s.handle("PUT /api/v1/me/password", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Current string `json:"current"`
			New     string `json:"new"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.ChangeOwnPassword(r.Context(), in.Current, in.New)
	})
	s.handle("GET /api/v1/me/api-keys", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Auth.ListAPIKeys(r.Context()) })
	s.handle("POST /api/v1/me/api-keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in auth.NewAPIKey
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		k, err := s.app.Auth.CreateAPIKey(r.Context(), in)
		return h.Created{V: k}, err
	})
	s.handle("DELETE /api/v1/me/api-keys/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.RevokeAPIKey(r.Context(), id)
	})
	s.handle("GET /api/v1/me/ssh-keys", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Auth.ListSSHKeys(r.Context()) })
	s.handle("POST /api/v1/me/ssh-keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Name      string `json:"name"`
			PublicKey string `json:"public_key"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		k, err := s.app.Auth.AddSSHKey(r.Context(), in.Name, in.PublicKey)
		return h.Created{V: k}, err
	})
	s.handle("DELETE /api/v1/me/ssh-keys/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.DeleteSSHKey(r.Context(), id)
	})

	// ---------------------------------------------------------------- users, groups, roles
	s.handle("GET /api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Auth.ListUsers(r.Context(), r.URL.Query().Get("q"), h.QueryInt(r, "limit", 100), h.QueryInt(r, "offset", 0))
	})
	s.handle("POST /api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in auth.NewUser
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		u, err := s.app.Auth.CreateLocalUser(r.Context(), in)
		return h.Created{V: u}, err
	})
	s.handle("GET /api/v1/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Auth.GetUser(r.Context(), id)
	})
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
	s.handle("GET /api/v1/groups", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Auth.ListGroups(r.Context()) })
	s.handle("GET /api/v1/roles", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Auth.ListRoles(r.Context()) })
	s.handle("GET /api/v1/users/lookup", func(w http.ResponseWriter, r *http.Request) (any, error) {
		// Minimal user search for permission dialogs (any user with manage rights somewhere).
		if _, err := requireUser(r); err != nil {
			return nil, err
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if len(q) < 2 {
			return []any{}, nil
		}
		rows, err := s.app.DB.Query(r.Context(), `SELECT id, username || '@' || provider, display_name FROM users
			WHERE NOT disabled AND (username ILIKE $1 || '%' OR display_name ILIKE '%' || $1 || '%') ORDER BY username LIMIT 20`, q)
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
		return map[string]int64{"id": nid}, err
	}
	s.handle("POST /api/v1/admin/groups", groupSave)
	s.handle("PUT /api/v1/admin/groups/{id}", groupSave)
	s.handle("DELETE /api/v1/admin/groups/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.DeleteGroup(r.Context(), id)
	})
	s.handle("GET /api/v1/admin/groups/{id}/members", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Auth.GroupMembers(r.Context(), id)
	})
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
		return map[string]int64{"id": nid}, err
	}
	// global permissions: the declared permissions and the defaults of every signed-in user
	s.handle("GET /api/v1/admin/permissions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		defaults, err := s.app.Auth.DefaultPermissions(r.Context())
		return map[string]any{"permissions": perm.All(), "authenticated": defaults}, err
	})
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
	s.handle("POST /api/v1/admin/roles", roleSave)
	s.handle("PUT /api/v1/admin/roles/{id}", roleSave)
	s.handle("DELETE /api/v1/admin/roles/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.DeleteRole(r.Context(), id)
	})

	// ---------------------------------------------------------------- categories
	s.handle("GET /api/v1/categories", func(w http.ResponseWriter, r *http.Request) (any, error) {
		parent, _ := strconv.ParseInt(r.URL.Query().Get("parent"), 10, 64)
		return s.app.Content.ChildrenOpt(r.Context(), parent, r.URL.Query().Get("hidden") == "1")
	})
	s.handle("GET /api/v1/categories/tree", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Tree(r.Context(), h.QueryInt(r, "limit", 5000))
	})
	s.handle("POST /api/v1/categories", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in content.CategoryInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		c, err := s.app.Content.CreateCategory(r.Context(), in)
		return h.Created{V: c}, err
	})
	s.handle("GET /api/v1/categories/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		c, err := s.app.Content.GetCategory(r.Context(), id)
		if err != nil {
			return nil, err
		}
		bc, err := s.app.Content.Breadcrumbs(r.Context(), c)
		if err != nil {
			return nil, err
		}
		els := []content.Element{}
		if slices.Contains(c.Permissions, "view") { // category administrators: structure only
			if els, err = s.app.Content.ListElements(r.Context(), id); err != nil {
				return nil, err
			}
		}
		return map[string]any{"category": c, "breadcrumbs": bc, "elements": els}, nil
	})
	s.handle("PUT /api/v1/categories/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.CategoryInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.UpdateCategory(r.Context(), id, in)
	})
	// PUT {"title": "…", "description": "…"} translates the category into lang; {"confirm": true} marks an outdated translation as current
	s.handle("PUT /api/v1/categories/{id}/translations/{lang}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.CatTranslationInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.TranslateCategory(r.Context(), id, r.PathValue("lang"), in)
	})
	s.handle("DELETE /api/v1/categories/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteCategory(r.Context(), id)
	})
	s.handle("POST /api/v1/categories/{id}/move", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			ParentID int64 `json:"parent_id"`
			Position *int  `json:"position"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.MoveCategory(r.Context(), id, in.ParentID, in.Position)
	})
	s.handle("POST /api/v1/categories/{id}/duplicate", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			ParentID int64 `json:"parent_id"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		c, err := s.app.Content.DuplicateCategory(r.Context(), id, in.ParentID)
		return h.Created{V: c}, err
	})
	s.handle("POST /api/v1/categories/{id}/reorder", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Order []int64 `json:"order"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.Reorder(r.Context(), id, in.Order)
	})
	s.handle("GET /api/v1/categories/{id}/acl", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.GetACL(r.Context(), id)
	})
	s.handle("POST /api/v1/categories/{id}/acl", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.RuleInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		rule, err := s.app.Content.AddRule(r.Context(), id, in)
		return h.Created{V: rule}, err
	})
	s.handle("DELETE /api/v1/acl/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteRule(r.Context(), id)
	})
	s.handle("GET /api/v1/categories/{id}/history", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.CategoryHistory(r.Context(), id, h.QueryInt(r, "limit", 200))
	})
	s.handle("POST /api/v1/categories/{id}/rollback", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.RollbackInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.RollbackCategory(r.Context(), id, in)
	})

	// ---------------------------------------------------------------- elements
	s.handle("GET /api/v1/element-types", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Content.Types(), nil })
	s.handle("POST /api/v1/elements", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in content.ElementInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		e, err := s.app.Content.CreateElement(r.Context(), in)
		return h.Created{V: e}, err
	})
	s.handle("POST /api/v1/elements/preview", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Type string          `json:"type"`
			Lang string          `json:"lang"`
			Body json.RawMessage `json:"body"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		html, err := s.app.Content.Preview(r.Context(), in.Type, in.Lang, in.Body)
		return map[string]string{"html": html}, err
	})
	s.handle("GET /api/v1/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		v, err := s.app.Content.GetElement(r.Context(), id, r.URL.Query().Get("lang"), r.URL.Query().Get("edit") == "1")
		if err == nil && s.app.Config.Get().Audit.LogViews {
			_ = audit.Log(r.Context(), s.app.DB, "element.view", "element", id, nil, map[string]string{"lang": v.ShownLang})
		}
		return v, err
	})
	s.handle("PUT /api/v1/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.SaveInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.SaveElement(r.Context(), id, in)
	})
	s.handle("PATCH /api/v1/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.MetaInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.UpdateElementMeta(r.Context(), id, in)
	})
	s.handle("DELETE /api/v1/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteElement(r.Context(), id)
	})
	s.handle("POST /api/v1/elements/{id}/move", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			CategoryID int64 `json:"category_id"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.MoveElement(r.Context(), id, in.CategoryID)
	})
	s.handle("POST /api/v1/elements/{id}/duplicate", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			CategoryID int64 `json:"category_id"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		e, err := s.app.Content.DuplicateElement(r.Context(), id, in.CategoryID)
		return h.Created{V: e}, err
	})
	s.handle("GET /api/v1/elements/{id}/revisions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.Revisions(r.Context(), id, r.URL.Query().Get("lang"))
	})
	s.handle("GET /api/v1/elements/{id}/revisions/{lang}/{rev}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		rev, err := h.PathID(r, "rev")
		if err != nil {
			return nil, err
		}
		return s.app.Content.GetRevision(r.Context(), id, r.PathValue("lang"), int(rev))
	})
	s.handle("POST /api/v1/elements/{id}/restore", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Lang string `json:"lang"`
			Rev  int    `json:"rev"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.RestoreRevision(r.Context(), id, in.Lang, in.Rev)
	})

	// ---------------------------------------------------------------- trash, search, tags
	s.handle("GET /api/v1/trash", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Content.Trash(r.Context()) })
	s.handle("POST /api/v1/trash/{type}/{id}/restore", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.Restore(r.Context(), r.PathValue("type"), id)
	})
	s.handle("DELETE /api/v1/trash/{type}/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.Purge(r.Context(), r.PathValue("type"), id)
	})
	s.handle("GET /api/v1/search", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		var scope []int64
		if c := q.Get("category"); c != "" {
			id, err := strconv.ParseInt(c, 10, 64)
			if err != nil {
				return nil, apperr.BadRequest("invalid category")
			}
			// category=ID searches in the category and its subcategories (recursive=0: only the category)
			if scope, err = s.app.Content.Scope(r.Context(), id, q.Get("recursive") != "0"); err != nil {
				return nil, err
			}
		}
		return s.app.Content.SearchIn(r.Context(), q.Get("q"), h.QueryList(r, "tags"), q.Get("lang"), h.QueryInt(r, "limit", 30), scope)
	})
	s.handle("GET /api/v1/tags", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Tags(r.Context(), r.URL.Query().Get("prefix"), h.QueryInt(r, "limit", 50))
	})

	// ---------------------------------------------------------------- i18n
	s.handle("GET /api/v1/i18n/status", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.I18n.Status(r.Context()) })
	s.handle("GET /api/v1/i18n/{lang}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		lang := r.PathValue("lang")
		if !config.ValidLanguage(lang) {
			return nil, apperr.BadRequest("invalid language")
		}
		return s.app.I18n.Messages(r.Context(), lang)
	})
	s.handle("GET /api/v1/admin/i18n/{lang}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.I18n.Strings(r.Context(), r.PathValue("lang"))
	})
	// PUT {"key": "text", "plural.key_few": "…"}: translations; "" removes a custom text
	s.handle("PUT /api/v1/admin/i18n/{lang}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in map[string]string
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		lang := r.PathValue("lang")
		res, err := s.app.I18n.SetStrings(r.Context(), lang, in)
		if err == nil && res.Written+res.Removed > 0 {
			_ = audit.Log(r.Context(), s.app.DB, "i18n.translate", "language", 0, nil, map[string]any{"lang": lang, "keys": slices.Sorted(maps.Keys(in)), "written": res.Written, "removed": res.Removed})
		}
		return res, err
	})
	// GET …/export?format=json|xliff|po&source=core&filter=all|missing|custom
	h.RecordRoute("GET /api/v1/admin/i18n/{lang}/export")
	s.app.Mux.HandleFunc("GET /api/v1/admin/i18n/{lang}/export", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		data, ctype, name, err := s.app.I18n.Export(r.Context(), r.PathValue("lang"), i18n.ExportOptions{Format: q.Get("format"), Source: q.Get("source"), Filter: q.Get("filter")})
		if err != nil {
			h.WriteError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": safeFileName(name)}))
		_, _ = w.Write(data) // #nosec G705 -- download (attachment, nosniff), not rendered
	})
	// POST …/import?format=json|xliff|po (body: the file; format detected when omitted)
	s.handle("POST /api/v1/admin/i18n/{lang}/import", func(w http.ResponseWriter, r *http.Request) (any, error) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 20<<20))
		if err != nil {
			return nil, apperr.BadRequest("file too large (max. 20 MB)")
		}
		lang := r.PathValue("lang")
		res, err := s.app.I18n.Import(r.Context(), lang, r.URL.Query().Get("format"), data)
		if err == nil {
			_ = audit.Log(r.Context(), s.app.DB, "i18n.import", "language", 0, nil, map[string]any{"lang": lang, "written": res.Written, "unchanged": res.Unchanged, "unknown": len(res.Unknown)})
		}
		return res, err
	})
	s.handle("POST /api/v1/admin/languages", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		var in struct {
			Code string `json:"code"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if !config.ValidLanguage(in.Code) {
			return nil, apperr.BadRequest("invalid language code (use e.g. de, fr, pt-BR)")
		}
		err := s.app.Config.Update(func(c *config.Config) error {
			if slices.Contains(c.I18n.Languages, in.Code) {
				return apperr.Conflict("language already exists")
			}
			c.I18n.Languages = append(c.I18n.Languages, in.Code)
			return nil
		})
		s.auditSettings(r, "settings.language_add", in.Code)
		return h.NoContent{}, err
	})
	s.handle("DELETE /api/v1/admin/languages/{code}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		code := r.PathValue("code")
		err := s.app.Config.Update(func(c *config.Config) error {
			if code == c.I18n.DefaultLanguage {
				return apperr.BadRequest("the default language cannot be removed")
			}
			if !slices.Contains(c.I18n.Languages, code) {
				return apperr.NotFound("language")
			}
			c.I18n.Languages = slices.DeleteFunc(c.I18n.Languages, func(x string) bool { return x == code })
			return nil
		})
		s.auditSettings(r, "settings.language_remove", code)
		return h.NoContent{}, err
	})

	// ---------------------------------------------------------------- settings, audit, maintenance
	s.handle("GET /api/v1/admin/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		c := s.app.Config.Raw()
		maskConfig(c)
		return map[string]any{"config": c, "path": s.app.Config.Path()}, nil
	})
	s.handle("PUT /api/v1/admin/settings", s.updateSettings)
	s.handle("GET /api/v1/admin/audit", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		entries, _, err := audit.Find(r.Context(), s.app.DB, auditSearch(r))
		return entries, err
	})
	s.handle("POST /api/v1/admin/retention/run", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		res, err := s.app.Content.RunRetention(r.Context())
		if err != nil {
			return nil, err
		}
		files, err := s.app.Files.GC(r.Context())
		return map[string]any{"revisions": res.Revisions, "trash": res.Trash, "audit": res.Audit, "files": files}, err
	})

	// ---------------------------------------------------------------- plugins
	s.handle("GET /api/v1/admin/plugins", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return map[string]any{"installed": s.app.Kernel.List(), "catalog": s.app.Config.Get().Plugins.Catalog,
			"pending": s.plugins.Pending(), "build": s.plugins.BuildStatus(), "can_build": s.plugins.CanBuild(),
			"build_unavailable": s.plugins.BuildUnavailable(), "mode": s.plugins.Mode(), "plugins_dir": s.plugins.PluginsDir(),
			"restart_pending": s.plugins.RestartPending(), "auto_restart_on_drop": s.app.Config.Get().Plugins.AutoRestartOnDrop,
			"cluster": s.app.Config.Get().Mode == "cluster", "removable": s.removablePlugins()}, nil
	})
	s.handle("PUT /api/v1/admin/plugins/options", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // installed plugins and the process are shared by all sites
			return nil, err
		}
		var in struct {
			AutoRestartOnDrop bool `json:"auto_restart_on_drop"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := s.app.Config.Base().Update(func(c *config.Config) error { c.Plugins.AutoRestartOnDrop = in.AutoRestartOnDrop; return nil }); err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.options", fmt.Sprintf("auto_restart_on_drop=%v", in.AutoRestartOnDrop))
		return in, nil
	})
	s.handle("POST /api/v1/admin/plugins/{id}/enable", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		err := s.app.Kernel.SetEnabled(r.Context(), r.PathValue("id"), true)
		s.auditSettings(r, "plugin.enable", r.PathValue("id"))
		return h.NoContent{}, err
	})
	s.handle("POST /api/v1/admin/plugins/{id}/disable", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		err := s.app.Kernel.SetEnabled(r.Context(), r.PathValue("id"), false)
		s.auditSettings(r, "plugin.disable", r.PathValue("id"))
		return h.NoContent{}, err
	})
	s.handle("POST /api/v1/admin/plugins/install", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // installed plugins and the process are shared by all sites
			return nil, err
		}
		var in struct {
			Source string `json:"source"` // git URL, github.com/owner/repo, catalog id or local directory
			Ref    string `json:"ref"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		res, err := s.plugins.Install(r.Context(), in.Source, in.Ref)
		s.auditSettings(r, "plugin.install", in.Source)
		if err != nil {
			return nil, err
		}
		return s.buildAfterChange(r.Context(), res, "install"), nil
	})
	s.handle("DELETE /api/v1/admin/plugins/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // installed plugins and the process are shared by all sites
			return nil, err
		}
		if err := s.plugins.Remove(r.Context(), r.PathValue("id")); err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.remove", r.PathValue("id"))
		return s.buildAfterChange(r.Context(), nil, "remove"), nil
	})
	s.handle("POST /api/v1/admin/plugins/build", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // installed plugins and the process are shared by all sites
			return nil, err
		}
		return s.plugins.StartBuild(r.Context(), "manual")
	})
	s.handle("POST /api/v1/admin/restart", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // installed plugins and the process are shared by all sites
			return nil, err
		}
		if s.app.Config.Get().Mode != "cluster" {
			if err := s.plugins.ScheduleRestart(); err != nil {
				return nil, err
			}
		} else {
			s.plugins.RestartAll(r.Context()) // one node after the other
		}
		s.auditSettings(r, "system.restart", "")
		return map[string]string{"status": "restarting"}, nil
	})
}

// buildAfterChange starts the build after an installation or removal; the
// server restarts automatically when it succeeded.
func (s *Server) buildAfterChange(ctx context.Context, res *pluginmgr.InstallResult, trigger string) map[string]any {
	out := map[string]any{"building": false}
	if res != nil {
		out["id"], out["name"], out["version"], out["dir"], out["upgrade"] = res.ID, res.Name, res.Version, res.Dir, res.Upgrade
	}
	if !s.plugins.CanBuild() {
		out["rebuild_required"] = true
		return out
	}
	if _, err := s.plugins.StartBuild(ctx, trigger); err != nil {
		out["rebuild_required"], out["error"] = true, err.Error()
		return out
	}
	out["building"] = true
	return out
}

func (s *Server) auditSettings(r *http.Request, action, detail string) {
	_ = audit.Log(r.Context(), s.app.DB, action, "settings", 0, nil, map[string]string{"detail": detail})
}

func asAppErr(err error, target **apperr.Error) bool { return errors.As(err, target) }

func maskURL(u string) string {
	if config.HasEnvRef(u) {
		return u
	}
	pu, err := url.Parse(u)
	if err != nil || pu.User == nil {
		return u
	}
	if _, has := pu.User.Password(); has {
		pu.User = url.UserPassword(pu.User.Username(), "xxxxx")
	}
	return pu.String()
}

// site returns public information needed by the frontend before login.
func (s *Server) site(w http.ResponseWriter, r *http.Request) (any, error) {
	c := s.app.Config.Get()
	var frontend []string
	for _, p := range s.app.Kernel.List() {
		if p.Active && p.Frontend {
			frontend = append(frontend, p.ID)
		}
	}
	home := c.UI.HomeCategory
	return map[string]any{
		"name": c.UI.SiteName, "template": c.UI.Template, "home_category": home, "home_element": c.UI.HomeElement, "editor": cmp.Or(c.UI.Editor, "visual"),
		"languages": c.I18n.Languages, "default_language": c.I18n.DefaultLanguage,
		"plugins": frontend, "element_types": s.app.Content.Types(), "auth_providers": s.app.Auth.ProviderInfos(),
		"version": version.Version, "permissions": acl.Names,
		// site: the site of this host; instance_only: settings only
		// instance administrators change (in config.yaml of the instance)
		"site": s.app.Site, "instance_only": config.InstanceOnly, "sites_accounts": s.app.Config.Get().Sites.Accounts,
		"separate_accounts": s.app.SeparateAccounts,
	}, nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
		// MFA "email" asks for a code by e-mail instead of the authenticator app
		MFA string `json:"mfa"`
	}
	if err := h.Decode(r, &in); err != nil {
		return nil, err
	}
	res, err := s.app.Auth.LoginMFA(r.Context(), in.Username, in.Password, in.TOTP, in.MFA, s.clientIP(r), r.UserAgent())
	if err != nil {
		s.auditLogin(r, nil, "password", in.Username, err)
		return nil, err
	}
	s.auditLogin(r, res.Principal, "password", in.Username, nil)
	s.setCookie(w, res.Token, res.ExpiresAt)
	return map[string]any{"user": res.Principal, "expires_at": res.ExpiresAt}, nil
}

// SettingsUpdate is the subset of the configuration editable in the UI.
// Server, database and mode settings require a restart and are changed in
// config.yaml or with "ewiki config set".
type SettingsUpdate struct {
	UI             *config.UI                `json:"ui"`
	I18n           *config.I18n              `json:"i18n"`
	Auth           *config.Auth              `json:"auth"`
	Retention      *config.Retention         `json:"retention"`
	Media          *config.Media             `json:"media"`
	Macros         *config.MacroSecurity     `json:"macros"`
	Mail           *config.Mail              `json:"mail"`
	Catalog        []config.CatalogEntry     `json:"catalog"`
	PluginSettings map[string]map[string]any `json:"plugin_settings"`
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := requireAdmin(r); err != nil {
		return nil, err
	}
	var in SettingsUpdate
	if err := h.Decode(r, &in); err != nil {
		return nil, err
	}
	before := s.app.Config.Raw()
	err := s.app.Config.Update(func(c *config.Config) error {
		if in.UI != nil {
			c.UI = *in.UI
		}
		if in.I18n != nil {
			c.I18n = *in.I18n
		}
		if in.Auth != nil {
			for _, d := range []string{in.Auth.SessionTTL, in.Auth.APIKeyMaxTTL, in.Auth.SSHTokenTTL, in.Auth.LockoutDuration} {
				if _, err := time.ParseDuration(d); err != nil {
					return apperr.BadRequest("invalid duration %q (use e.g. 24h, 30m)", d)
				}
			}
			c.Auth = *in.Auth
		}
		if in.Retention != nil {
			c.Retention = *in.Retention
		}
		if in.Media != nil {
			if v := in.Media.Viewer; v != "" && v != media.Internal && s.app.Media.Opener(v) == nil {
				return apperr.BadRequest("unknown or inactive program %q", v)
			}
			c.Media = *in.Media
		}
		if in.Macros != nil && !reflect.DeepEqual(*in.Macros, c.Plugins.Macros) {
			return apperr.BadRequest("macro security settings can only be changed in config.yaml")
		}
		if in.Catalog != nil {
			c.Plugins.Catalog = in.Catalog
		}
		if in.Mail != nil {
			if in.Mail.SMTP.Password == secretMask {
				in.Mail.SMTP.Password = c.Mail.SMTP.Password
			}
			if in.Mail.Bounce.Password == secretMask {
				in.Mail.Bounce.Password = c.Mail.Bounce.Password
			}
			c.Mail = *in.Mail
		}
		for id, m := range in.PluginSettings {
			if c.Settings == nil {
				c.Settings = map[string]map[string]any{}
			}
			// keep stored secrets the UI only saw masked
			for k, v := range m {
				if v == secretMask {
					m[k] = c.Settings[id][k]
				}
			}
			c.Settings[id] = m
		}
		return nil
	})
	if err != nil {
		var ae *apperr.Error
		if !asAppErr(err, &ae) {
			return nil, apperr.BadRequest("%s", err.Error())
		}
		return nil, err
	}
	maskConfig(before)
	c := s.app.Config.Raw()
	maskConfig(c)
	_ = audit.Log(r.Context(), s.app.DB, "settings.update", "settings", 0, before, c)
	return map[string]any{"config": c, "path": s.app.Config.Path()}, nil
}

// removablePlugins lists plugins whose source can be removed (Docker images:
// only the additional ones in the data directory).
func (s *Server) removablePlugins() []string {
	out := []string{}
	disk, dirs, err := s.plugins.OnDisk()
	if err != nil {
		return out
	}
	for id := range dirs {
		if mf := disk[id]; mf != nil && !mf.Required {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// auditLogin records a sign-in or a failed attempt (audit.log_logins).
func (s *Server) auditLogin(r *http.Request, p *identity.Principal, method, username string, err error) {
	ctx := audit.WithRequest(r.Context(), s.clientIP(r), r.UserAgent())
	if err != nil {
		reason := "error"
		var ae *apperr.Error
		if errors.As(err, &ae) {
			reason = ae.Code
		}
		_ = audit.Log(ctx, s.app.DB, "auth.login_failed", "user", 0, nil, map[string]string{"username": username, "method": method, "reason": reason})
		return
	}
	_ = audit.Log(identity.With(ctx, p), s.app.DB, "auth.login", "user", p.ID(), nil, map[string]string{"method": method})
}

// safeFileName keeps letters, digits, dot, dash and underscore of a
// download name (no quotes, separators or control characters).
func safeFileName(name string) string {
	b := []rune{}
	for _, r := range name {
		if r < 128 && (r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			b = append(b, r)
		}
	}
	if len(b) == 0 || len(b) > 120 {
		return "download"
	}
	return string(b)
}
