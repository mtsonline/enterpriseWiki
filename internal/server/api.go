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
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/i18n"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"github.com/mtsonline/enterprisewiki/internal/media"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
	"github.com/mtsonline/enterprisewiki/internal/readability"
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
	{"/api/v1/admin/auth/login", "core.settings.manage"},
	{"/api/v1/admin/limits", "core.settings.manage"},
	{"/api/v1/admin/data", "core.settings.manage"},
	{"/api/v1/admin/consent", "core.settings.manage"},
	{"/api/v1/admin/consents", "core.audit.view"},
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
	s.routesLogin()
	s.routesMedia()
	s.routesOffice()
	s.routesDemo()
	s.routesCollab()
	s.routesIdentity()
	s.routesAlias()
	s.routesSignatures()
	s.routesSessions()
	s.routesProviderAdmin()
	s.routesSCIM()
	s.routesMFA()
	s.routesPasswordless()
	s.routesScimAdmin()
	s.routesM3()
	s.routesM4()
	s.routesM6()
	s.routesM7()
	s.routesM8()
	s.routesSites()
	s.routesM9()
	s.routesM15()
	s.routesAccess()
	s.routesAudit()
	s.routesLimits()
	s.routesKeyLimits()
	s.routesSecrets()
	s.routesDrafts()
	s.routesInstances()
	s.routesElementRights()
	s.routesMaintenance()
	s.routesConsent()
	s.routesScheme()
	s.routesData()
	s.routesPlacements()
	s.routesNotifications()
	s.routesWatchAssignments()
	// ---------------------------------------------------------------- system
	// action: core.health
	h.RecordRoute("GET /healthz")
	s.app.Mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { h.Write(w, 200, map[string]string{"status": "ok"}) })
	// on-demand TLS of the bundled reverse proxy (Caddy "ask"): 200 only for
	// the wiki's own host names, so no certificates are requested for others
	// action: core.tls_allowed
	h.RecordRoute("GET /tls-allowed")
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
	// action: core.ready
	s.handle("GET /readyz", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := s.app.DB.Ping(r.Context()); err != nil {
			return nil, &apperr.Error{Status: 503, Code: "db_unavailable", Msg: "database unavailable"}
		}
		return map[string]string{"status": "ready"}, nil
	})
	// action: core.site
	s.handle("GET /api/v1/site", s.site)

	// ---------------------------------------------------------------- auth
	// action: core.auth.providers
	s.handle("GET /api/v1/auth/providers", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Auth.ProviderInfos(), nil
	})
	// action: core.auth.login
	s.handle("POST /api/v1/auth/login", s.login)
	// action: core.auth.logout
	s.handle("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if p := identity.From(r.Context()); p != nil {
			_ = audit.Log(r.Context(), s.app.DB, "auth.logout", "user", p.ID(), nil, nil)
		}
		var redirect string
		if c, err := r.Cookie(sessionCookie); err == nil {
			redirect, _ = s.app.Auth.Logout(r.Context(), c.Value)
		}
		if a, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && !strings.HasPrefix(a, "ewk_") {
			_, _ = s.app.Auth.Logout(r.Context(), a)
		}
		s.clearCookie(w)
		if redirect != "" {
			// single logout: the browser continues at the identity provider
			return map[string]string{"redirect": redirect}, nil
		}
		return h.NoContent{}, nil
	})
	// action: core.auth.me
	s.handle("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p := identity.From(r.Context())
		if p == nil {
			return map[string]any{"user": nil}, nil
		}
		return map[string]any{"user": p, "handle": p.Handle(), "manages_categories": s.app.ACL.Any(r.Context(), p, acl.Manage)}, nil
	})
	// action: core.auth.ssh.challenge
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
	// action: core.auth.ssh.verify
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
	// action: core.me.update
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
	// action: core.me.prefs
	s.handle("GET /api/v1/me/prefs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p := identity.From(r.Context())
		if p == nil || p.UserID == 0 {
			return nil, apperr.ErrUnauthorized
		}
		cur := p.Prefs
		if cur == nil {
			cur = map[string]string{}
		}
		// the hint on long sentences: what the site offers and whether it
		// is on for this person
		mode := cmp.Or(s.app.Config.Get().UI.Readability.Mode, readability.ModeOff)
		return map[string]any{"prefs": cur, "options": auth.PrefOptions(),
			"readability": map[string]any{"site": mode, "active": readability.Active(mode, cur["readability"])}}, nil
	})
	// action: core.me.password
	s.handle("PUT /api/v1/me/password", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Current string `json:"current"`
			New     string `json:"new"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.ChangeOwnPassword(r.Context(), in.Current, in.New, s.sessionToken(r))
	})
	// action: core.me.api_keys.list
	s.handle("GET /api/v1/me/api-keys", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Auth.ListAPIKeys(r.Context()) })
	// action: core.me.api_keys.create
	s.handle("POST /api/v1/me/api-keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in auth.NewAPIKey
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		k, err := s.app.Auth.CreateAPIKey(r.Context(), in)
		return h.Created{V: k}, err
	})
	// action: core.me.api_keys.delete
	s.handle("DELETE /api/v1/me/api-keys/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.RevokeAPIKey(r.Context(), id)
	})
	// action: core.me.ssh_keys.list
	s.handle("GET /api/v1/me/ssh-keys", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Auth.ListSSHKeys(r.Context()) })
	// action: core.me.ssh_keys.add
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
	// action: core.me.ssh_keys.delete
	s.handle("DELETE /api/v1/me/ssh-keys/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.DeleteSSHKey(r.Context(), id)
	})

	// ---------------------------------------------------------------- users, groups, roles
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

	// ---------------------------------------------------------------- categories
	// action: core.categories.list
	s.handle("GET /api/v1/categories", func(w http.ResponseWriter, r *http.Request) (any, error) {
		parent, _ := strconv.ParseInt(r.URL.Query().Get("parent"), 10, 64)
		return s.app.Content.ChildrenOpt(r.Context(), parent, r.URL.Query().Get("hidden") == "1")
	})
	// action: core.categories.tree
	s.handle("GET /api/v1/categories/tree", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Tree(r.Context(), h.QueryInt(r, "limit", 5000))
	})
	// action: core.categories.create
	s.handle("POST /api/v1/categories", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in content.CategoryInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		c, err := s.app.Content.CreateCategory(r.Context(), in)
		return h.Created{V: c}, err
	})
	// action: core.categories.get
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
		space, err := s.app.Content.SpaceOf(r.Context(), id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"category": c, "breadcrumbs": bc, "elements": els, "space": space}, nil
	})
	// GET /api/v1/categories/{id}/overview?lang=&page=&view= → one page of
	// the overview of the category with its display (only visible pages)
	// action: core.categories.overview
	s.handle("GET /api/v1/categories/{id}/overview", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		q := r.URL.Query()
		lang := q.Get("lang")
		if lang == "" {
			lang = s.app.Config.Get().I18n.DefaultLanguage
		}
		return s.app.Content.CategoryOverview(r.Context(), id, clip(lang, 20), h.QueryInt(r, "page", 1), clip(q.Get("view"), 20))
	})
	// spaces: the top-level categories (directory, address /s/KEY, settings)
	// action: core.spaces.list
	s.handle("GET /api/v1/spaces", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Spaces(r.Context())
	})
	// action: core.spaces.create
	s.handle("POST /api/v1/spaces", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in content.NewSpaceInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		sp, err := s.app.Content.CreateSpaceFrom(r.Context(), in)
		if err != nil {
			return nil, err
		}
		return h.Created{V: sp}, nil
	})
	// page views (counted per element and day only) and the evaluation of a category
	// action: core.elements.view
	s.handle("POST /api/v1/elements/{id}/view", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.CountView(r.Context(), id)
	})
	// action: core.categories.insights
	s.handle("GET /api/v1/categories/{id}/insights", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.Insights(r.Context(), id, h.QueryInt(r, "days", 30), r.URL.Query().Get("lang"))
	})
	// action: core.spaces.personal
	s.handle("POST /api/v1/spaces/personal", func(w http.ResponseWriter, r *http.Request) (any, error) {
		sp, created, err := s.app.Content.CreatePersonalSpace(r.Context())
		if created {
			return h.Created{V: sp}, err
		}
		return sp, err
	})
	// action: core.spaces.get
	s.handle("GET /api/v1/spaces/{key}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.SpaceByKey(r.Context(), r.PathValue("key"))
	})
	// action: core.categories.archive
	s.handle("POST /api/v1/categories/{id}/archive", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Archived bool `json:"archived"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.SetArchived(r.Context(), id, in.Archived)
	})
	// export a category tree as JSON Lines; import one below ?parent= (0 = new space)
	// action: core.categories.export
	h.RecordRoute("GET /api/v1/categories/{id}/export")
	s.app.Mux.HandleFunc("GET /api/v1/categories/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		id, err := h.PathID(r, "id")
		if err != nil {
			h.WriteError(w, r, err)
			return
		}
		ew := &lazyWriter{w: w, name: fmt.Sprintf("space-%d-%s.jsonl", id, time.Now().Format("20060102"))}
		if err := s.app.Content.ExportTree(r.Context(), id, ew); err != nil && !ew.started {
			h.WriteError(w, r, err)
		}
	})
	// action: core.categories.import
	s.handle("POST /api/v1/categories/import", func(w http.ResponseWriter, r *http.Request) (any, error) {
		parent, _ := strconv.ParseInt(r.URL.Query().Get("parent"), 10, 64)
		res, err := s.app.Content.ImportTree(r.Context(), parent, r.Body)
		if err != nil {
			return nil, err
		}
		return h.Created{V: res}, nil
	})
	// action: core.categories.space
	s.handle("PUT /api/v1/categories/{id}/space", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var raw json.RawMessage
		if err := h.Decode(r, &raw); err != nil {
			return nil, err
		}
		var in content.SpaceInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, apperr.BadRequest("invalid JSON: %v", err)
		}
		// which settings were sent (the others stay)
		var keys map[string]json.RawMessage
		_ = json.Unmarshal(raw, &keys)
		in.Present = map[string]bool{}
		for k := range keys {
			in.Present[k] = true
		}
		return s.app.Content.SetSpace(r.Context(), id, in)
	})
	// action: core.categories.settings.get
	s.handle("GET /api/v1/categories/{id}/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.GetCategorySettings(r.Context(), id)
	})
	// action: core.categories.settings.set
	s.handle("PUT /api/v1/categories/{id}/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.Space
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.SetCategorySettings(r.Context(), id, in)
	})
	// action: core.categories.update
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
	// action: core.categories.translate
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
	// action: core.categories.delete
	s.handle("DELETE /api/v1/categories/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteCategory(r.Context(), id)
	})
	// action: core.categories.move
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
	// action: core.categories.duplicate
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
	// action: core.categories.reorder
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
	// action: core.categories.acl.list
	s.handle("GET /api/v1/categories/{id}/acl", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.GetACL(r.Context(), id)
	})
	// action: core.categories.acl.add
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
	// action: core.categories.acl.remove
	s.handle("DELETE /api/v1/acl/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteRule(r.Context(), id)
	})
	// action: core.categories.history
	s.handle("GET /api/v1/categories/{id}/history", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.CategoryHistory(r.Context(), id, h.QueryInt(r, "limit", 200))
	})
	// action: core.categories.rollback
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
	// action: core.elements.types
	s.handle("GET /api/v1/element-types", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Content.Types(), nil })
	// action: core.elements.create
	s.handle("POST /api/v1/elements", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in content.ElementInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		e, err := s.app.Content.CreateElement(r.Context(), in)
		return h.Created{V: e}, err
	})
	// action: core.elements.preview
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
	// action: core.elements.get
	s.handle("GET /api/v1/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		if idx, page, ok := macroPageParam(r.URL.Query().Get("mp")); ok {
			ctx = content.WithMacroPage(ctx, id, idx, page) // a page of a list (link ?mp=INDEX-PAGE)
		}
		v, err := s.app.Content.GetElement(ctx, id, r.URL.Query().Get("lang"), r.URL.Query().Get("edit") == "1")
		if err == nil && s.app.Config.Get().Audit.LogViews {
			_ = audit.Log(r.Context(), s.app.DB, "element.view", "element", id, nil, map[string]string{"lang": v.ShownLang})
		}
		return v, err
	})
	// GET /api/v1/elements/batch?ids=1,2,3&lang= → the pages (as GET
	// /elements/{id}) the caller may view, in the order asked for; others
	// are left out without saying why (featured pages of a category page).
	// At most core.categories.featured ids.
	// action: core.elements.batch
	s.handle("GET /api/v1/elements/batch", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		// enough for the featured pages and for the whole pages of an overview
		max := max(limits.Value(s.app.Config.Get(), limits.CategoryFeatured), limits.Value(s.app.Config.Get(), limits.CategoryFull))
		ids := []int64{}
		for _, part := range strings.Split(q.Get("ids"), ",") {
			if part = strings.TrimSpace(part); part == "" {
				continue
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || id <= 0 {
				return nil, apperr.BadRequest("ids: element ids separated by commas")
			}
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		if int64(len(ids)) > max {
			return nil, apperr.BadRequest("at most %d ids (expert settings %s and %s)", max, limits.CategoryFeatured, limits.CategoryFull)
		}
		ctx := r.Context()
		items := []*content.ElementView{}
		// bounded by the limit above; each page is rendered with its own checks
		for _, id := range ids {
			v, err := s.app.Content.GetElement(ctx, id, q.Get("lang"), false)
			if err != nil {
				if code, _, _ := apperr.Status(err); code >= 500 {
					return nil, err
				}
				continue // not visible or gone: left out
			}
			if s.app.Config.Get().Audit.LogViews {
				_ = audit.Log(ctx, s.app.DB, "element.view", "element", id, nil, map[string]string{"lang": v.ShownLang})
			}
			items = append(items, v)
		}
		return map[string]any{"items": items}, nil
	})
	// preview of sources for their editor: count and first titles with the caller's rights
	// action: core.sources.preview
	s.handle("POST /api/v1/sources/preview", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if _, err := requireUser(r); err != nil {
			return nil, err
		}
		var in struct {
			Sources    string `json:"sources"`
			CategoryID int64  `json:"category_id"`
			Lang       string `json:"lang"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		list, err := content.ParseSources(in.Sources)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"total": 0, "items": []map[string]any{}}
		if list = content.WithDefault(list, in.CategoryID); len(list) == 0 {
			return out, nil
		}
		set, err := s.app.Content.ResolveSources(r.Context(), list)
		if err != nil || set.Empty() {
			return out, err
		}
		items, total, err := s.app.Content.ListIn(r.Context(), content.ListOptions{Sources: set, Lang: in.Lang, Order: "updated", Limit: 5, SummaryLen: 1})
		if err != nil {
			return nil, err
		}
		shown := []map[string]any{}
		for _, e := range items {
			shown = append(shown, map[string]any{"id": e.ID, "title": e.Title})
		}
		out["total"], out["items"] = total, shown
		return out, nil
	})
	// one macro of an element again at another page of its list
	// action: core.elements.macro
	s.handle("GET /api/v1/elements/{id}/macro/{index}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		idx, err := h.PathID(r, "index")
		if err != nil || idx > 200 {
			return nil, apperr.BadRequest("index")
		}
		page := h.QueryInt(r, "page", 1)
		if page < 1 || page > 100000 {
			page = 1
		}
		out, err := s.app.Content.RenderMacroPage(r.Context(), id, int(idx), page, r.URL.Query().Get("lang"))
		if err != nil {
			return nil, err
		}
		if out == "" {
			return nil, apperr.NotFound("macro")
		}
		return map[string]string{"html": out}, nil
	})
	// action: core.elements.update
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
	// action: core.elements.patch
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
	// action: core.elements.delete
	s.handle("DELETE /api/v1/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteElement(r.Context(), id)
	})
	// action: core.elements.move
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
	// action: core.elements.duplicate
	s.handle("POST /api/v1/elements/{id}/duplicate", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			CategoryID int64 `json:"category_id"`
			// WithRights copies the element's own rules and "own rights only"
			WithRights bool `json:"with_rights"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		e, err := s.app.Content.DuplicateElement(r.Context(), id, in.CategoryID, in.WithRights)
		return h.Created{V: e}, err
	})
	// action: core.elements.revisions
	s.handle("GET /api/v1/elements/{id}/revisions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.Revisions(r.Context(), id, r.URL.Query().Get("lang"))
	})
	// action: core.elements.revision
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
	// action: core.elements.restore
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
	// action: core.trash.list
	s.handle("GET /api/v1/trash", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Content.Trash(r.Context()) })
	// action: core.trash.restore
	s.handle("POST /api/v1/trash/{type}/{id}/restore", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.Restore(r.Context(), r.PathValue("type"), id)
	})
	// action: core.trash.purge
	s.handle("DELETE /api/v1/trash/{type}/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.Purge(r.Context(), r.PathValue("type"), id)
	})
	// action: core.elements.recent
	s.handle("GET /api/v1/recent", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		limit := 10
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 50 {
				return nil, apperr.BadRequest("limit must be between 1 and 50")
			}
			limit = n
		}
		switch q.Get("by") {
		case "":
			var cat int64
			if v := q.Get("category"); v != "" {
				var err error
				if cat, err = strconv.ParseInt(v, 10, 64); err != nil || cat < 0 {
					return nil, apperr.BadRequest("category must be a category id")
				}
			}
			return s.app.Content.Recent(r.Context(), cat, limit)
		case "me":
			return s.app.Content.RecentlyEditedByMe(r.Context(), limit)
		}
		return nil, apperr.BadRequest(`by must be empty or "me"`)
	})
	// action: core.search
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
	// action: core.tags
	s.handle("GET /api/v1/tags", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Tags(r.Context(), r.URL.Query().Get("prefix"), h.QueryInt(r, "limit", 50))
	})

	// ---------------------------------------------------------------- i18n
	// action: core.i18n.status
	s.handle("GET /api/v1/i18n/status", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.I18n.Status(r.Context()) })
	// action: core.i18n.texts
	s.handle("GET /api/v1/i18n/{lang}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		lang := r.PathValue("lang")
		if !config.ValidLanguage(lang) {
			return nil, apperr.BadRequest("invalid language")
		}
		return s.app.I18n.Messages(r.Context(), lang)
	})
	// action: core.i18n.strings
	s.handle("GET /api/v1/admin/i18n/{lang}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.I18n.Strings(r.Context(), r.PathValue("lang"))
	})
	// PUT {"key": "text", "plural.key_few": "…"}: translations; "" removes a custom text
	// action: core.i18n.set
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
	// action: core.i18n.export
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
	// action: core.i18n.import
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
	// action: core.languages.add
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
	// action: core.languages.remove
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
	// action: core.settings.get
	s.handle("GET /api/v1/admin/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		c := s.app.Config.Raw()
		maskConfig(c)
		return map[string]any{"config": c, "path": s.app.Config.Path()}, nil
	})
	// action: core.settings.update
	s.handle("PUT /api/v1/admin/settings", s.updateSettings)
	// action: core.retention.run
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
	// action: core.plugins.list
	s.handle("GET /api/v1/admin/plugins", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return map[string]any{"installed": s.app.Kernel.List(), "origins": s.plugins.Origins(), "catalog": s.app.Config.Get().Plugins.Catalog,
			"pending": s.plugins.Pending(), "build": s.plugins.BuildStatus(), "can_build": s.plugins.CanBuild(),
			"build_unavailable": s.plugins.BuildUnavailable(), "mode": s.plugins.Mode(), "plugins_dir": s.plugins.PluginsDir(),
			"restart_pending": s.plugins.RestartPending(), "auto_restart_on_drop": s.app.Config.Get().Plugins.AutoRestartOnDrop,
			"cluster": s.app.Config.Get().Mode == "cluster", "removable": s.removablePlugins()}, nil
	})
	// action: core.plugins.sources.check (origin level of a source before installing)
	s.handle("POST /api/v1/admin/plugins/origin", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		var in struct {
			Source string `json:"source"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.plugins.CheckSource(in.Source)
	})
	// action: core.plugins.sources.list
	s.handle("GET /api/v1/admin/plugins/sources", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		c := s.app.Config.Get().Plugins
		list := c.TrustedSources
		if list == nil {
			list = []config.TrustedSource{}
		}
		return map[string]any{"sources": list, "allow_external": c.ExternalAllowed()}, nil
	})
	// action: core.plugins.sources.add (critical: needs a recent sign-in)
	s.handle("POST /api/v1/admin/plugins/sources", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		if err := requireRecentSignIn(r); err != nil {
			return nil, err
		}
		var in config.TrustedSource
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		norm, err := pluginmgr.NormalizeTrusted(in.Prefix)
		if err != nil {
			return nil, apperr.BadRequest("%s", err.Error())
		}
		if err := s.app.Config.Base().Update(func(c *config.Config) error {
			return pluginmgr.SetTrustedSources(c, append(slices.Clone(c.Plugins.TrustedSources), config.TrustedSource{Prefix: norm, Note: in.Note}))
		}); err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.sources.add", norm)
		return h.Created{V: config.TrustedSource{Prefix: norm, Note: strings.TrimSpace(in.Note)}}, nil
	})
	// action: core.plugins.sources.remove
	s.handle("DELETE /api/v1/admin/plugins/sources", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		prefix := r.URL.Query().Get("prefix")
		found := false
		if err := s.app.Config.Base().Update(func(c *config.Config) error {
			c.Plugins.TrustedSources = slices.DeleteFunc(slices.Clone(c.Plugins.TrustedSources), func(t config.TrustedSource) bool {
				if t.Prefix == prefix {
					found = true
				}
				return t.Prefix == prefix
			})
			return nil
		}); err != nil {
			return nil, err
		}
		if !found {
			return nil, apperr.NotFound("trusted source")
		}
		s.auditSettings(r, "plugin.sources.remove", prefix)
		return h.NoContent{}, nil
	})
	// Access data of plugin sources (private repositories). The token is
	// written once and never shown again (neither API nor audit log).
	// action: core.plugins.credentials.list
	s.handle("GET /api/v1/admin/plugins/credentials", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		out := []pluginmgr.CredentialView{}
		for _, c := range s.app.Config.Base().Raw().Plugins.Credentials {
			out = append(out, pluginmgr.ViewCredential(c))
		}
		return map[string]any{"credentials": out}, nil
	})
	// action: core.plugins.credentials.set (critical: needs a recent sign-in)
	s.handle("PUT /api/v1/admin/plugins/credentials", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		if err := requireRecentSignIn(r); err != nil {
			return nil, err
		}
		var in struct {
			Prefix   string `json:"prefix"`
			Username string `json:"username"`
			Token    string `json:"token"`
			Note     string `json:"note"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		var saved config.SourceCredential
		if err := s.app.Config.Base().Update(func(c *config.Config) error {
			var err error
			saved, err = pluginmgr.SetCredential(c, config.SourceCredential{Prefix: in.Prefix, Username: in.Username, Token: in.Token, Note: in.Note})
			return err
		}); err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.credentials.set", saved.Prefix)
		return pluginmgr.ViewCredential(saved), nil
	})
	// action: core.plugins.credentials.remove
	s.handle("DELETE /api/v1/admin/plugins/credentials", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		prefix := r.URL.Query().Get("prefix")
		found := false
		if err := s.app.Config.Base().Update(func(c *config.Config) error {
			c.Plugins.Credentials = slices.DeleteFunc(slices.Clone(c.Plugins.Credentials), func(x config.SourceCredential) bool {
				if x.Prefix == prefix {
					found = true
				}
				return x.Prefix == prefix
			})
			return nil
		}); err != nil {
			return nil, err
		}
		if !found {
			return nil, apperr.NotFound("access data")
		}
		s.auditSettings(r, "plugin.credentials.remove", prefix)
		return h.NoContent{}, nil
	})
	// action: core.plugins.sources.external (allow or forbid external core
	// plugins; allowing needs a recent sign-in)
	s.handle("PUT /api/v1/admin/plugins/sources/external", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		var in struct {
			Allow bool `json:"allow"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if in.Allow {
			if err := requireRecentSignIn(r); err != nil {
				return nil, err
			}
		}
		if err := s.app.Config.Base().Update(func(c *config.Config) error { c.Plugins.AllowExternal = &in.Allow; return nil }); err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.sources.external", fmt.Sprintf("allow=%v", in.Allow))
		return in, nil
	})
	// action: core.plugins.options
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
	// action: core.plugins.enable
	s.handle("POST /api/v1/admin/plugins/{id}/enable", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		err := s.app.Kernel.SetEnabled(r.Context(), r.PathValue("id"), true)
		s.auditSettings(r, "plugin.enable", r.PathValue("id"))
		return h.NoContent{}, err
	})
	// action: core.plugins.disable
	s.handle("POST /api/v1/admin/plugins/{id}/disable", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		err := s.app.Kernel.SetEnabled(r.Context(), r.PathValue("id"), false)
		s.auditSettings(r, "plugin.disable", r.PathValue("id"))
		return h.NoContent{}, err
	})
	// action: core.plugins.install
	s.handle("POST /api/v1/admin/plugins/install", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // installed plugins and the process are shared by all sites
			return nil, err
		}
		var in struct {
			Source string `json:"source"` // git URL, github.com/owner/repo, catalog id or local directory
			Ref    string `json:"ref"`
			// AcceptExternal confirms the warning for a source that is
			// neither official nor trusted (needed for every installation).
			AcceptExternal bool `json:"accept_external"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		res, err := s.plugins.Install(r.Context(), in.Source, in.Ref, pluginmgr.InstallOptions{AcceptExternal: in.AcceptExternal, By: handleOf(r)})
		if err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.install", fmt.Sprintf("%s level=%s source=%s commit=%s accepted=%v",
			res.ID, res.Origin.Level, res.Origin.Source, res.Origin.Commit, res.Origin.Accepted))
		return s.buildAfterChange(r.Context(), res, "install"), nil
	})
	// action: core.plugins.remove
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
	// action: core.plugins.build
	s.handle("POST /api/v1/admin/plugins/build", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // installed plugins and the process are shared by all sites
			return nil, err
		}
		st, err := s.plugins.StartBuild(r.Context(), "manual")
		return h.Accepted{V: st}, err
	})
	// action: core.plugins.restart
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
		return h.Accepted{V: map[string]string{"status": "restarting"}}, nil
	})
}

// buildAfterChange starts the build after an installation or removal; the
// server restarts automatically when it succeeded.
func (s *Server) buildAfterChange(ctx context.Context, res *pluginmgr.InstallResult, trigger string) map[string]any {
	out := map[string]any{"building": false}
	if res != nil {
		out["id"], out["name"], out["version"], out["dir"], out["upgrade"] = res.ID, res.Name, res.Version, res.Dir, res.Upgrade
		out["origin"] = res.Origin
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
	templates := []map[string]any{}
	for _, p := range s.app.Kernel.List() {
		if p.Active && p.Frontend {
			frontend = append(frontend, p.ID)
		}
		if p.Active && slices.Contains(p.Kinds, coreplugin.KindTemplate) {
			templates = append(templates, map[string]any{"id": p.ID, "name": p.Name, "color_scheme": p.ColorScheme})
		}
	}
	// templates offered by plugins (templates of data, core.template/12)
	for _, t := range s.app.Content.ExtraTemplates(r.Context()) {
		templates = append(templates, map[string]any{"id": t.ID, "name": t.Name, "names": t.Names, "color_scheme": t.ColorScheme})
	}
	slices.SortFunc(templates, func(a, b map[string]any) int { return strings.Compare(a["name"].(string), b["name"].(string)) })
	home := c.UI.HomeCategory
	return map[string]any{
		"name": c.UI.SiteName, "template": c.UI.Template, "templates": templates, "home_category": home, "home_element": c.UI.HomeElement, "editor": cmp.Or(c.UI.Editor, "visual"), "collaboration": c.UI.Collaboration, "personal_placement": c.UI.PersonalPlacement, "custom_css": c.UI.CustomCSS, "editor_tools": siteEditorTools(c.UI.EditorTools), "dashboard_tiles": nonNil(c.UI.DashboardTiles), "dashboard_home": c.UI.DashboardHome, "maintenance": siteMaintenance(c.Maintenance), "consent": siteConsent(c.Consent), "not_found_element": c.UI.NotFoundElement, "forbidden_element": c.UI.ForbiddenElement,
		"wait_notice_s": limits.Value(c, limits.WaitNoticeSeconds),
		"readability":   siteReadability(c),
		"languages":     c.I18n.Languages, "default_language": c.I18n.DefaultLanguage,
		"plugins": frontend, "element_types": s.app.Content.Types(), "auth_providers": s.app.Auth.ProviderInfos(),
		"version": version.Version, "licensed": s.licensed(), "permissions": acl.Names,
		// site: the site of this host; instance_only: settings only
		// instance administrators change (in config.yaml of the instance)
		"site": s.app.Site, "instance_only": config.InstanceOnly, "sites_accounts": s.app.Config.Get().Sites.Accounts,
		"separate_accounts": s.app.SeparateAccounts,
		"login":             s.loginPage(r.Context()),
	}, nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := h.Decode(r, &in); err != nil {
		return nil, err
	}
	res, err := s.app.Auth.Login(r.Context(), in.Username, in.Password, s.clientIP(r), r.UserAgent())
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
	Lists          *config.Lists             `json:"lists"`
	People         *config.People            `json:"people"`
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
			if len(in.UI.DashboardTiles) > 200 {
				return apperr.BadRequest("ui.dashboard_tiles: at most 200 tiles")
			}
			for _, b := range in.UI.DashboardTiles {
				if !content.ValidBlockID(b) {
					return apperr.BadRequest("ui.dashboard_tiles: %q is no block id", b)
				}
			}
			// the template of the site: an active one (an unchanged value may stay)
			if in.UI.Template != "" && in.UI.Template != c.UI.Template && in.UI.Template != "core.basic" && !s.app.Content.TemplateOK(r.Context(), in.UI.Template) {
				return apperr.BadRequest("ui.template: %q is not an active website template", in.UI.Template)
			}
			if len(in.UI.EditorTools) > 0 {
				if err := content.CheckEditorTools(in.UI.EditorTools); err != nil {
					return err
				}
			}
			if err := checkReadability(in.UI.Readability); err != nil {
				return err
			}
			c.UI = *in.UI
		}
		if in.I18n != nil {
			c.I18n = *in.I18n
		}
		if in.Auth != nil {
			if in.Auth.SessionIdle != "" && in.Auth.SessionIdle != "0" {
				if _, err := time.ParseDuration(in.Auth.SessionIdle); err != nil {
					return apperr.BadRequest("invalid duration %q (use e.g. 12h, 30m, or 0)", in.Auth.SessionIdle)
				}
			}
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
		if in.Lists != nil {
			if in.Lists.MaxItems < 0 || in.Lists.MaxItems > 10000 {
				return apperr.BadRequest("lists.max_items: 0 (default 1000) or 50 to 10000")
			}
			c.Lists = *in.Lists
		}
		if in.People != nil {
			if v := in.People.EmailVisibility; v != "" && !slices.Contains(config.EmailLevels, v) {
				return apperr.BadRequest("people.email_visibility must be users, permission or nobody")
			}
			c.People = *in.People
		}
		if in.Macros != nil && !reflect.DeepEqual(*in.Macros, c.Plugins.Macros) {
			return apperr.BadRequest("macro security settings can only be changed in config.yaml")
		}
		if in.Catalog != nil {
			c.Plugins.Catalog = in.Catalog
		}
		if in.Mail != nil {
			// the stored password only for the same server and protection (else enter it again)
			if in.Mail.SMTP.Password == secretMask {
				n, o := in.Mail.SMTP, c.Mail.SMTP
				if n.Host != o.Host || n.Port != o.Port || n.Security != o.Security || (n.SkipVerify && !o.SkipVerify) {
					return errSecretAgain("mail.smtp.password")
				}
				in.Mail.SMTP.Password = c.Mail.SMTP.Password
			}
			if in.Mail.Bounce.Password == secretMask {
				n, o := in.Mail.Bounce, c.Mail.Bounce
				if n.IMAPHost != o.IMAPHost || n.IMAPPort != o.IMAPPort || n.Security != o.Security || (n.SkipVerify && !o.SkipVerify) {
					return errSecretAgain("mail.bounce.password")
				}
				in.Mail.Bounce.Password = c.Mail.Bounce.Password
			}
			c.Mail = *in.Mail
		}
		for id, m := range in.PluginSettings {
			if c.Settings == nil {
				c.Settings = map[string]map[string]any{}
			}
			if err := checkInstances(m); err != nil {
				return err
			}
			// keep stored secrets the UI only saw masked
			if err := keepMasked(m, c.Settings[id], id); err != nil {
				return err
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
		_ = audit.Log(ctx, s.app.DB, "auth.login_failed", "user", 0, nil, map[string]string{"username": clip(username, 255), "method": method, "reason": reason})
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

// lazyWriter sets the download headers on the first write, so that errors
// before any output can still be answered as JSON.
type lazyWriter struct {
	w       http.ResponseWriter
	name    string
	started bool
}

func (l *lazyWriter) Write(p []byte) (int, error) {
	if !l.started {
		l.started = true
		l.w.Header().Set("Content-Type", "application/x-ndjson")
		l.w.Header().Set("Content-Disposition", `attachment; filename="`+l.name+`"`)
	}
	return l.w.Write(p)
}

// macroPageParam reads "INDEX-PAGE" (a page of a macro list).
func macroPageParam(v string) (index, page int, ok bool) {
	a, b, found := strings.Cut(v, "-")
	if !found {
		return 0, 0, false
	}
	i, err1 := strconv.Atoi(a)
	p, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || i < 1 || i > 200 || p < 1 || p > 100000 {
		return 0, 0, false
	}
	return i, p, true
}

// clip shortens a text to at most n bytes (whole runes).
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s[:n])
	return string(r[:len(r)-1]) + "…"
}

// siteReadability is the hint on long sentences and words for the editors:
// the mode of the site, the limits (defaults filled in) and how much of a
// text the browser checks.
func siteReadability(c *config.Config) map[string]any {
	r := c.UI.Readability
	return map[string]any{
		"mode":           cmp.Or(r.Mode, readability.ModeOff),
		"sentence_words": cmp.Or(r.SentenceWords, readability.DefaultSentenceWords),
		"word_letters":   cmp.Or(r.WordLetters, readability.DefaultWordLetters),
		"max_kb":         limits.Value(c, limits.ReadabilityKB),
	}
}

// checkReadability checks the settings of the readability hint (the same
// rules as config.ValidateReadability, with messages in the language of
// the user).
func checkReadability(r config.Readability) error {
	if r.Mode != "" && !slices.Contains(readability.Modes, r.Mode) {
		return apperr.BadRequest("ui.readability.mode must be off, offered or on")
	}
	if n := r.SentenceWords; n != 0 && (n < readability.MinSentenceWords || n > readability.MaxSentenceWords) {
		return apperr.BadRequest("ui.readability.sentence_words must be 0 (default %d) or %d to %d", readability.DefaultSentenceWords, readability.MinSentenceWords, readability.MaxSentenceWords)
	}
	if n := r.WordLetters; n != 0 && (n < readability.MinWordLetters || n > readability.MaxWordLetters) {
		return apperr.BadRequest("ui.readability.word_letters must be 0 (default %d) or %d to %d", readability.DefaultWordLetters, readability.MinWordLetters, readability.MaxWordLetters)
	}
	return nil
}

// siteEditorTools is the site's choice of toolbar buttons (["*"] = all).
func siteEditorTools(list []string) []string {
	if len(list) == 0 {
		return []string{content.AllEditorTools}
	}
	return list
}

// nonNil returns an empty list for nil (JSON [] instead of null).
func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
