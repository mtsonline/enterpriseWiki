package server

import (
	"context"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// overviewRow is one category in the central permission overview.
type overviewRow struct {
	ID        int64             `json:"id"`
	ParentID  *int64            `json:"parent_id"`
	Depth     int               `json:"depth"`
	Titles    map[string]string `json:"titles"`
	SpaceKey  string            `json:"space_key"` // top level: the category is a space
	Personal  bool              `json:"personal"`  // a personal space
	Rules     []acl.Rule        `json:"rules"`     // rules of the subject attached here
	Effective []string          `json:"effective"` // effective permissions (bits and actions)
	Sources   []acl.Source      `json:"sources"`   // where they come from
}

func (s *Server) routesM15() {
	// declared permissions (for the permission dialogs of categories and roles)
	s.handle("GET /api/v1/permissions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if _, err := requireUser(r); err != nil {
			return nil, err
		}
		presets := map[string]any{}
		for _, name := range []string{perm.Reader, perm.Editor, perm.Manager} {
			bits, acts, _ := content.PresetPerms(name)
			presets[name] = map[string]any{"permissions": bits, "actions": acts}
		}
		groups := map[string]string{"core": "Core"}
		for _, p := range s.app.Kernel.List() {
			groups[p.ID] = p.Name
		}
		return map[string]any{"permissions": perm.All(), "presets": presets, "bits": acl.Names, "groups": groups}, nil
	})
	s.handle("PUT /api/v1/acl/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.RuleInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.UpdateRule(r.Context(), id, in)
	})
	// GET /api/v1/permissions/overview?subject_type=user&subject_id=3 – the
	// categories the caller may manage with the rules and effective
	// permissions of one user, group or role
	s.handle("GET /api/v1/permissions/overview", func(w http.ResponseWriter, r *http.Request) (any, error) {
		me, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		typ := r.URL.Query().Get("subject_type")
		var sid *int64
		if v := r.URL.Query().Get("subject_id"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, apperr.BadRequest("invalid subject_id")
			}
			sid = &n
		}
		return s.permissionOverview(r.Context(), me, typ, sid)
	})
}

func (s *Server) permissionOverview(ctx context.Context, me *identity.Principal, typ string, sid *int64) ([]overviewRow, error) {
	var subject *identity.Principal
	switch typ {
	case "user":
		if sid == nil {
			return nil, apperr.BadRequest("subject_id is required")
		}
		p, err := s.app.Auth.LoadPrincipal(ctx, *sid)
		if err != nil {
			return nil, err
		}
		subject = p
	case "group", "role":
		if sid == nil {
			return nil, apperr.BadRequest("subject_id is required")
		}
		subject = &identity.Principal{UserID: -1} // matches the rules of the group or role only
		if typ == "group" {
			subject.Groups = []int64{*sid}
		} else {
			subject.Roles = []int64{*sid}
		}
	case "guest":
		sid = nil
	case "authenticated":
		sid, subject = nil, &identity.Principal{UserID: -1}
	default:
		return nil, apperr.BadRequest("subject_type must be user, group, role, guest or authenticated")
	}
	rows, err := s.app.DB.Query(ctx, `SELECT id, parent_id, nlevel(path) - 1, titles, COALESCE(space_key, ''), parent_id IS NULL AND space ? 'personal'
		FROM categories WHERE deleted_at IS NULL ORDER BY path`)
	if err != nil {
		return nil, err
	}
	cats, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (overviewRow, error) {
		var o overviewRow
		return o, r.Scan(&o.ID, &o.ParentID, &o.Depth, &o.Titles, &o.SpaceKey, &o.Personal)
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(cats))
	for i, c := range cats {
		ids[i] = c.ID
	}
	mine, err := s.app.ACL.PermsMap(ctx, me, ids)
	if err != nil {
		return nil, err
	}
	rules, err := s.app.ACL.RulesOf(ctx, typ, sid)
	if err != nil {
		return nil, err
	}
	byCat := map[int64][]acl.Rule{}
	for _, r := range rules {
		byCat[r.CategoryID] = append(byCat[r.CategoryID], r)
	}
	var subjectPerms map[int64]acl.Perm
	if subject != nil {
		if subjectPerms, err = s.app.ACL.PermsMap(ctx, subject, ids); err != nil {
			return nil, err
		}
	}
	out := []overviewRow{}
	for _, c := range cats {
		if mine[c.ID]&acl.Manage == 0 {
			continue // only categories the caller manages
		}
		c.Rules = byCat[c.ID]
		if c.Rules == nil {
			c.Rules = []acl.Rule{}
		}
		c.Effective = []string{}
		c.Sources = []acl.Source{}
		if subject != nil {
			c.Effective = subjectPerms[c.ID].Strings()
			acts, _ := s.app.ACL.Actions(ctx, subject, c.ID)
			c.Effective = append(c.Effective, acts...)
			c.Sources, _ = s.app.ACL.Explain(ctx, subject, c.ID)
		}
		out = append(out, c)
	}
	return out, nil
}
