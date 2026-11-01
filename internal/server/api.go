package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"slices"
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
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/version"
)

func (s *Server) handle(pattern string, fn h.Handler) {
	h.RecordRoute(pattern)
	s.app.Mux.HandleFunc(pattern, h.JSON(fn))
}

func requireAdmin(r *http.Request) error {
	p := identity.From(r.Context())
	if p == nil {
		return apperr.ErrUnauthorized
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
	s.routesM3()
	s.routesM4()
	// ---------------------------------------------------------------- system
	s.app.Mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { h.Write(w, 200, map[string]string{"status": "ok"}) })
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
		return map[string]any{"user": p, "handle": p.Handle()}, nil
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
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.UpdateProfile(r.Context(), in.DisplayName, in.Email, in.Language)
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
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			IsAdmin     bool   `json:"is_admin"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		nid, err := s.app.Auth.SaveRole(r.Context(), id, in.Name, in.Description, in.IsAdmin)
		return map[string]int64{"id": nid}, err
	}
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
		return s.app.Content.Children(r.Context(), parent)
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
		els, err := s.app.Content.ListElements(r.Context(), id)
		if err != nil {
			return nil, err
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
		return s.app.Content.GetElement(r.Context(), id, r.URL.Query().Get("lang"), r.URL.Query().Get("edit") == "1")
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
		return s.app.Content.Search(r.Context(), q.Get("q"), h.QueryList(r, "tags"), q.Get("lang"), h.QueryInt(r, "limit", 30))
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
	s.handle("PUT /api/v1/admin/i18n/{lang}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in map[string]string
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.I18n.SetStrings(r.Context(), r.PathValue("lang"), in)
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
		id, _ := strconv.ParseInt(r.URL.Query().Get("entity_id"), 10, 64)
		return audit.Query(r.Context(), s.app.DB, r.URL.Query().Get("entity_type"), id, h.QueryInt(r, "limit", 100), h.QueryInt(r, "offset", 0))
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
			"pending": s.plugins.Pending(), "build": s.plugins.BuildStatus(), "can_build": s.plugins.CanBuild()}, nil
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
		if err := requireAdmin(r); err != nil {
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
		return res, err
	})
	s.handle("DELETE /api/v1/admin/plugins/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		err := s.plugins.Remove(r.Context(), r.PathValue("id"))
		s.auditSettings(r, "plugin.remove", r.PathValue("id"))
		return h.NoContent{}, err
	})
	s.handle("POST /api/v1/admin/plugins/build", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.plugins.StartBuild(r.Context())
	})
	s.handle("POST /api/v1/admin/restart", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		if err := s.plugins.ScheduleRestart(); err != nil {
			return nil, err
		}
		s.auditSettings(r, "system.restart", "")
		return map[string]string{"status": "restarting"}, nil
	})
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
		"name": c.UI.SiteName, "template": c.UI.Template, "home_category": home,
		"languages": c.I18n.Languages, "default_language": c.I18n.DefaultLanguage,
		"plugins": frontend, "element_types": s.app.Content.Types(), "auth_providers": s.app.Auth.ProviderInfos(),
		"version": version.Version, "permissions": acl.Names,
	}, nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
	}
	if err := h.Decode(r, &in); err != nil {
		return nil, err
	}
	res, err := s.app.Auth.Login(r.Context(), in.Username, in.Password, in.TOTP, s.clientIP(r), r.UserAgent())
	if err != nil {
		return nil, err
	}
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
