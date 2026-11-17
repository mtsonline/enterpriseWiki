package content

import (
	"context"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Instance loads an instance of a plugin (an element of a type with instance
// permissions) that the caller may view.
func (s *Service) Instance(ctx context.Context, elementID int64) (acl.Instance, error) {
	cat, err := s.ElementCategory(ctx, elementID, acl.View)
	if err != nil {
		return acl.Instance{}, err
	}
	var typ string
	if err := s.pool.QueryRow(ctx, `SELECT type FROM elements WHERE id=$1`, elementID).Scan(&typ); err != nil {
		return acl.Instance{}, apperr.NotFound("element")
	}
	if len(perm.InstanceKeys(typ)) == 0 {
		return acl.Instance{}, apperr.BadRequest("elements of type %q have no permissions of their own", typ)
	}
	return acl.Instance{ID: elementID, Type: typ, CategoryID: cat}, nil
}

// InstanceRightsView is what GET /elements/{id}/rights returns.
type InstanceRightsView struct {
	Type string `json:"type"`
	// Available are the permissions of the type (key, label, description).
	Available []perm.Def `json:"available"`
	// Mine are the caller's own permissions on the instance.
	Mine []string `json:"mine"`
	// Rules of the instance, only for those who may manage it.
	Rules []InstanceRuleView `json:"rules,omitempty"`
	// Manage: the caller may change the rules.
	Manage bool `json:"manage"`
}

// InstanceRuleView is a rule with the name of its subject.
type InstanceRuleView struct {
	acl.InstanceRule
	Name string `json:"name"`
}

// manageKey is the permission that manages instances of typ (the one whose
// key ends in ".manage"); empty if the type has none.
func manageKey(typ string) string {
	for _, k := range perm.InstanceKeys(typ) {
		if d, _ := perm.Lookup(k); d.Instance == typ && len(k) > 7 && k[len(k)-7:] == ".manage" {
			return k
		}
	}
	return ""
}

// InstanceRights returns the permissions of an instance for the caller.
func (s *Service) InstanceRights(ctx context.Context, elementID int64) (*InstanceRightsView, error) {
	inst, err := s.Instance(ctx, elementID)
	if err != nil {
		return nil, err
	}
	p := identity.From(ctx)
	mine, err := s.acl.InstanceActions(ctx, p, inst)
	if err != nil {
		return nil, err
	}
	v := &InstanceRightsView{Type: inst.Type, Available: []perm.Def{}, Mine: mine}
	for _, k := range perm.InstanceKeys(inst.Type) {
		d, _ := perm.Lookup(k)
		v.Available = append(v.Available, d)
	}
	mk := manageKey(inst.Type)
	v.Manage = mk != "" && slices.Contains(mine, mk)
	if !v.Manage {
		return v, nil
	}
	rules, err := s.acl.InstanceRules(ctx, elementID)
	if err != nil {
		return nil, err
	}
	v.Rules = []InstanceRuleView{}
	for _, r := range rules {
		v.Rules = append(v.Rules, InstanceRuleView{InstanceRule: r, Name: s.subjectName(ctx, r.SubjectType, r.SubjectID)})
	}
	return v, nil
}

// subjectName is the display name of a user, group or role ("" for
// authenticated).
func (s *Service) subjectName(ctx context.Context, typ string, id *int64) string {
	if id == nil {
		return ""
	}
	var q string
	switch typ {
	case "user":
		q = `SELECT COALESCE(NULLIF(display_name, ''), username) FROM users WHERE id=$1`
	case "group":
		q = `SELECT name FROM groups WHERE id=$1`
	case "role":
		q = `SELECT name FROM roles WHERE id=$1`
	default:
		return ""
	}
	var name string
	_ = s.pool.QueryRow(ctx, q, *id).Scan(&name)
	return name
}

// InstanceRuleInput is one rule to set.
type InstanceRuleInput struct {
	SubjectType string   `json:"subject_type"`
	SubjectID   *int64   `json:"subject_id"`
	Actions     []string `json:"actions"`
}

// MaxInstanceRules limits the rules of one instance.
const MaxInstanceRules = 500

// SetInstanceRights replaces the rules of an instance. It needs the manage
// permission of the instance, and nobody grants a permission they do not
// have themselves (administrators and managers of the category have all).
func (s *Service) SetInstanceRights(ctx context.Context, elementID int64, in []InstanceRuleInput) (*InstanceRightsView, error) {
	inst, err := s.Instance(ctx, elementID)
	if err != nil {
		return nil, err
	}
	p := identity.From(ctx)
	mine, err := s.acl.InstanceActions(ctx, p, inst)
	if err != nil {
		return nil, err
	}
	mk := manageKey(inst.Type)
	if mk == "" || !slices.Contains(mine, mk) {
		return nil, apperr.Forbidden("changing the permissions of this %s needs the permission %q", inst.Type, mk)
	}
	if len(in) > MaxInstanceRules {
		return nil, apperr.BadRequest("at most %d rules per instance", MaxInstanceRules)
	}
	allowed := perm.InstanceKeys(inst.Type)
	seen := map[string]bool{}
	for i, r := range in {
		switch r.SubjectType {
		case "user", "group", "role":
			if r.SubjectID == nil || *r.SubjectID <= 0 {
				return nil, apperr.BadRequest("rule %d: subject_id is required", i+1)
			}
			if err := s.subjectExists(ctx, r.SubjectType, *r.SubjectID); err != nil {
				return nil, err
			}
		case "authenticated":
			if r.SubjectID != nil {
				return nil, apperr.BadRequest("rule %d: authenticated has no subject_id", i+1)
			}
		default:
			return nil, apperr.BadRequest("rule %d: subject_type must be user, group, role or authenticated", i+1)
		}
		if len(r.Actions) == 0 {
			return nil, apperr.BadRequest("rule %d: choose at least one permission", i+1)
		}
		for _, a := range r.Actions {
			if !slices.Contains(allowed, a) {
				return nil, apperr.BadRequest("rule %d: %q is not a permission of %s", i+1, a, inst.Type)
			}
			if !slices.Contains(mine, a) {
				return nil, apperr.Forbidden("you cannot grant %q: you do not have it yourself", a)
			}
		}
		key := r.SubjectType
		if r.SubjectID != nil {
			key += ":" + strconv.FormatInt(*r.SubjectID, 10)
		}
		if seen[key] {
			return nil, apperr.BadRequest("rule %d: one rule per person, group or role", i+1)
		}
		seen[key] = true
		slices.Sort(in[i].Actions)
		in[i].Actions = slices.Compact(in[i].Actions)
	}
	before, err := s.acl.InstanceRules(ctx, elementID)
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// lock the instance: concurrent changes of its rules apply one after another
		if _, err := tx.Exec(ctx, `SELECT 1 FROM elements WHERE id=$1 FOR UPDATE`, elementID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM instance_rules WHERE element_id=$1`, elementID); err != nil {
			return err
		}
		for _, r := range in {
			if _, err := tx.Exec(ctx, `INSERT INTO instance_rules(element_id, subject_type, subject_id, actions, created_by) VALUES ($1,$2,$3,$4,$5)`,
				elementID, r.SubjectType, r.SubjectID, r.Actions, nullID(ctx)); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, "instance.rights", "element", elementID, before, in)
	})
	if err != nil {
		return nil, err
	}
	s.aclChanged(ctx)
	return s.InstanceRights(ctx, elementID)
}

func (s *Service) subjectExists(ctx context.Context, typ string, id int64) error {
	table := map[string]string{"user": "users", "group": "groups", "role": "roles"}[typ]
	var ok bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=$1)`, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return apperr.BadRequest("unknown %s %d", typ, id)
	}
	return nil
}
