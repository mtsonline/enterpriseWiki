package content

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Rights of one element (see acl.Service.ElementPerms): rules in addition
// to those of its category, "own rights only" and an own classification.
// Who may give rights on the element (acl.Grant: managers of the category
// by a rule, administrators, or a rule on the element) changes them; nobody
// gives more than they have.

// ElementACL is the rights configuration of an element.
type ElementACL struct {
	ElementID  int64 `json:"element_id"`
	CategoryID int64 `json:"category_id"`
	// OwnRightsOnly: the category's rules do not apply (administrators and
	// managers of the category by a rule keep access).
	OwnRightsOnly bool `json:"own_rights_only"`
	// Classification set on the element; Effective is the strictest of the
	// element and its categories.
	Classification          int                `json:"classification"`
	EffectiveClassification int                `json:"effective_classification"`
	Hidden                  bool               `json:"hidden"`
	Rules                   []acl.ElementRule  `json:"rules"`
	Users                   map[int64]RuleUser `json:"users"`
	// Mine are the caller's permissions on the element.
	Mine []string `json:"mine"`
}

// ElementRuleInput adds or changes a rule of an element.
type ElementRuleInput struct {
	SubjectType string   `json:"subject_type"`
	SubjectID   *int64   `json:"subject_id"`
	Permissions acl.Perm `json:"permissions"`
	// Actions are named category permissions of plugins (comments …).
	Actions []string `json:"actions"`
	// Preset (reader, editor) sets permissions and actions.
	Preset string `json:"preset"`
}

// ElementRightsInput changes "own rights only" and the classification.
type ElementRightsInput struct {
	OwnRightsOnly  *bool `json:"own_rights_only"`
	Classification *int  `json:"classification"`
}

// requireGrant loads element id and checks that the caller may give rights
// on it.
func (s *Service) requireGrant(ctx context.Context, id int64) (*Element, error) {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		if identity.From(ctx) == nil {
			return nil, apperr.ErrUnauthorized
		}
		return nil, err
	}
	if err := s.requireElement(ctx, e, acl.Grant); err != nil {
		if errors.Is(err, apperr.ErrForbidden) {
			return nil, apperr.Forbidden("changing the rights of this page needs the permission to give rights on it")
		}
		return nil, err
	}
	return e, nil
}

// GetElementACL returns the rights of an element (needs the permission to
// give rights on it).
func (s *Service) GetElementACL(ctx context.Context, id int64) (*ElementACL, error) {
	e, err := s.requireGrant(ctx, id)
	if err != nil {
		return nil, err
	}
	rules, own, class, err := s.acl.ElementRules(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &ElementACL{ElementID: id, CategoryID: e.CategoryID, OwnRightsOnly: own, Classification: class, Hidden: e.Hidden, Rules: rules,
		EffectiveClassification: s.acl.ElementClassification(ctx, id, e.CategoryID), Mine: s.elementPerms(ctx, e).Strings()}
	var ids []int64
	for _, r := range rules {
		if r.SubjectType == "user" && r.SubjectID != nil {
			ids = append(ids, *r.SubjectID)
		}
	}
	if out.Users, err = s.ruleUsers(ctx, ids); err != nil {
		return nil, err
	}
	return out, nil
}

// ruleUsers names the people of rules.
func (s *Service) ruleUsers(ctx context.Context, ids []int64) (map[int64]RuleUser, error) {
	users := map[int64]RuleUser{}
	if len(ids) == 0 {
		return users, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id, username || '@' || provider, display_name FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid int64
		var u RuleUser
		if err := rows.Scan(&uid, &u.Handle, &u.DisplayName); err != nil {
			return nil, err
		}
		users[uid] = u
	}
	return users, rows.Err()
}

// checkElementGrant validates a rule the caller wants to give on element e:
// only element bits, only what the caller has (administrators: anything),
// guests only view.
func (s *Service) checkElementGrant(ctx context.Context, e *Element, in *ElementRuleInput) error {
	if in.Preset != "" {
		switch in.Preset {
		case perm.Reader:
			in.Permissions, in.Actions = acl.View, perm.Preset(perm.Reader)
		case perm.Editor:
			in.Permissions, in.Actions = acl.View|acl.Edit|acl.Delete, perm.Preset(perm.Editor)
		default:
			return apperr.BadRequest("preset must be reader or editor")
		}
	}
	if in.Actions == nil {
		in.Actions = []string{}
	}
	if in.Permissions <= 0 || in.Permissions&^acl.ElementBits != 0 {
		return apperr.BadRequest("permissions of a page: view (1), edit (2), delete (8), grant (32)")
	}
	if in.Permissions&acl.View == 0 {
		return apperr.BadRequest("a rule of a page always includes view")
	}
	if err := perm.Validate(perm.Category, in.Actions); err != nil {
		return err
	}
	switch in.SubjectType {
	case "guest", "authenticated":
		in.SubjectID = nil
	case "user", "group", "role":
		if in.SubjectID == nil || *in.SubjectID <= 0 {
			return apperr.BadRequest("subject_id is required")
		}
		table := map[string]string{"user": "users", "group": "groups", "role": "roles"}[in.SubjectType]
		var ok bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=$1)`, *in.SubjectID).Scan(&ok); err != nil || !ok {
			return apperr.BadRequest("%s %d does not exist", in.SubjectType, *in.SubjectID)
		}
	default:
		return apperr.BadRequest("subject_type must be user, group, role, guest or authenticated")
	}
	if in.SubjectType == "guest" && (in.Permissions != acl.View || len(in.Actions) > 0) {
		return apperr.BadRequest("guests can only be granted view permission")
	}
	p := identity.From(ctx)
	if p.Admin() {
		return nil
	}
	mine := s.elementPerms(ctx, e)
	if in.Permissions&^mine != 0 {
		return apperr.BadRequest("you cannot grant permissions you do not have")
	}
	acts, err := s.acl.ElementActions(ctx, p, e.ID, e.CategoryID)
	if err != nil {
		return err
	}
	if in.Preset != "" {
		// a template gives the permissions of plugins in it that the
		// caller has (a new plugin permission does not block templates)
		in.Actions = slices.DeleteFunc(in.Actions, func(a string) bool { return !slices.Contains(acts, a) })
	}
	for _, a := range in.Actions {
		if !slices.Contains(acts, a) {
			return apperr.BadRequest("you cannot grant the permission %s, which you do not have", a)
		}
	}
	return nil
}

// keepsGrant reports whether the caller still may give rights on e with the
// rules rules: administrators and managers of the category by a rule always,
// others only by a rule of the element with grant (the category gives grant
// to nobody else).
func (s *Service) keepsGrant(ctx context.Context, e *Element, rules []acl.ElementRule) bool {
	p := identity.From(ctx)
	cat, _ := s.acl.Perms(ctx, p, e.CategoryID)
	if p.Admin() || (cat&acl.Manage != 0 && s.acl.ManagesByRule(ctx, p, e.CategoryID)) {
		return true
	}
	for _, r := range rules {
		if r.Permissions&acl.Grant != 0 && acl.MatchesAny([]acl.Subject{{Type: r.SubjectType, ID: r.SubjectID}}, p) {
			return true
		}
	}
	return false
}

// AddElementRule gives permissions on an element.
func (s *Service) AddElementRule(ctx context.Context, id int64, in ElementRuleInput) (*acl.ElementRule, error) {
	e, err := s.requireGrant(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.checkElementGrant(ctx, e, &in); err != nil {
		return nil, err
	}
	r := &acl.ElementRule{ElementID: id, SubjectType: in.SubjectType, SubjectID: in.SubjectID, Permissions: in.Permissions, Actions: in.Actions}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockElementRules(ctx, tx, id); err != nil {
			return err
		}
		var n int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM element_rules WHERE element_id=$1`, id).Scan(&n); err != nil {
			return err
		}
		if max := limits.Value(s.cfg.Get(), limits.ElementRules); n >= max {
			return apperr.BadRequest("at most %d rules per page (expert setting %s)", max, limits.ElementRules)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO element_rules(element_id, subject_type, subject_id, permissions, actions, created_by)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, id, r.SubjectType, r.SubjectID, r.Permissions, r.Actions, nullID(ctx)).Scan(&r.ID); err != nil {
			return err
		}
		if err := queueSearch(ctx, tx, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "element.acl.add", "element", id, nil, r)
	})
	if err != nil {
		return nil, err
	}
	s.aclChanged(ctx)
	return r, nil
}

// elementRule loads a rule and checks the permission to give rights on its
// element.
func (s *Service) elementRule(ctx context.Context, ruleID int64) (*acl.ElementRule, *Element, error) {
	var r acl.ElementRule
	err := s.pool.QueryRow(ctx, `SELECT id, element_id, subject_type, subject_id, permissions, actions FROM element_rules WHERE id=$1`, ruleID).
		Scan(&r.ID, &r.ElementID, &r.SubjectType, &r.SubjectID, &r.Permissions, &r.Actions)
	if err != nil {
		if identity.From(ctx) == nil {
			return nil, nil, apperr.ErrUnauthorized
		}
		return nil, nil, apperr.NotFound("rule")
	}
	e, err := s.requireGrant(ctx, r.ElementID)
	if err != nil {
		if isNotFound(err) {
			return nil, nil, apperr.NotFound("rule")
		}
		return nil, nil, err
	}
	return &r, e, nil
}

// otherRules returns the rules of e without rule id.
func (s *Service) otherRules(ctx context.Context, e *Element, id int64) ([]acl.ElementRule, error) {
	rules, _, _, err := s.acl.ElementRules(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(rules, func(r acl.ElementRule) bool { return r.ID == id }), nil
}

// UpdateElementRule changes the permissions of a rule of an element.
func (s *Service) UpdateElementRule(ctx context.Context, ruleID int64, in ElementRuleInput) (*acl.ElementRule, error) {
	r, e, err := s.elementRule(ctx, ruleID)
	if err != nil {
		return nil, err
	}
	in.SubjectType, in.SubjectID = r.SubjectType, r.SubjectID // the subject stays
	if err := s.checkElementGrant(ctx, e, &in); err != nil {
		return nil, err
	}
	rest, err := s.otherRules(ctx, e, r.ID)
	if err != nil {
		return nil, err
	}
	before := *r
	r.Permissions, r.Actions = in.Permissions, in.Actions
	if !s.keepsGrant(ctx, e, append(rest, *r)) {
		return nil, apperr.BadRequest("you would lose the permission to give rights on this page")
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockElementRules(ctx, tx, e.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE element_rules SET permissions=$2, actions=$3 WHERE id=$1`, r.ID, r.Permissions, r.Actions); err != nil {
			return err
		}
		if err := queueSearch(ctx, tx, e.ID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "element.acl.update", "element", e.ID, before, r)
	})
	if err != nil {
		return nil, err
	}
	s.aclChanged(ctx)
	return r, nil
}

// DeleteElementRule removes a rule of an element.
func (s *Service) DeleteElementRule(ctx context.Context, ruleID int64) error {
	r, e, err := s.elementRule(ctx, ruleID)
	if err != nil {
		return err
	}
	rest, err := s.otherRules(ctx, e, r.ID)
	if err != nil {
		return err
	}
	if !s.keepsGrant(ctx, e, rest) {
		return apperr.BadRequest("you would lose the permission to give rights on this page")
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockElementRules(ctx, tx, e.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM element_rules WHERE id=$1`, r.ID); err != nil {
			return err
		}
		if err := queueSearch(ctx, tx, e.ID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "element.acl.delete", "element", e.ID, r, nil)
	})
	if err != nil {
		return err
	}
	s.aclChanged(ctx)
	return nil
}

// SetElementRights changes "own rights only" and the classification of an
// element (needs the permission to give rights on it).
func (s *Service) SetElementRights(ctx context.Context, id int64, in ElementRightsInput) (*ElementACL, error) {
	e, err := s.requireGrant(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Classification != nil && (*in.Classification < acl.Public || *in.Classification > acl.StrictConfidential) {
		return nil, apperr.BadRequest("classification: 0–3")
	}
	if in.OwnRightsOnly != nil && *in.OwnRightsOnly {
		rules, _, _, err := s.acl.ElementRules(ctx, id)
		if err != nil {
			return nil, err
		}
		if !s.keepsGrant(ctx, e, rules) {
			return nil, apperr.BadRequest("you would lose the permission to give rights on this page: give yourself a rule with it first")
		}
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockElementRules(ctx, tx, id); err != nil {
			return err
		}
		before := map[string]any{"own_rights_only": e.OwnRightsOnly, "classification": e.Classification}
		after := map[string]any{"own_rights_only": e.OwnRightsOnly, "classification": e.Classification}
		if in.OwnRightsOnly != nil {
			if _, err := tx.Exec(ctx, `UPDATE elements SET own_rights_only=$2 WHERE id=$1`, id, *in.OwnRightsOnly); err != nil {
				return err
			}
			after["own_rights_only"] = *in.OwnRightsOnly
		}
		if in.Classification != nil {
			if _, err := tx.Exec(ctx, `UPDATE elements SET classification=$2 WHERE id=$1`, id, *in.Classification); err != nil {
				return err
			}
			after["classification"] = *in.Classification
		}
		if err := queueSearch(ctx, tx, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "element.rights", "element", id, before, after)
	})
	if err != nil {
		return nil, err
	}
	s.aclChanged(ctx)
	return s.GetElementACL(ctx, id)
}

// lockElementRules serializes changes of the rights of one element.
func (s *Service) lockElementRules(ctx context.Context, tx pgx.Tx, id int64) error {
	// acl: internal – row lock after requireGrant
	_, err := tx.Exec(ctx, `SELECT 1 FROM elements WHERE id=$1 FOR UPDATE`, id)
	return err
}

// queueSearch lets external search indexes take the element again (its
// viewers changed).
func queueSearch(ctx context.Context, tx pgx.Tx, id int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO search_changes(element_id) VALUES ($1)`, id)
	return err
}
