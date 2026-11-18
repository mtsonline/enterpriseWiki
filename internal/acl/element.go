package acl

import (
	"cmp"
	"context"
	"slices"
	"strconv"

	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// ElementRule gives permissions on one element (in addition to those of its
// category, or instead of them with "own rights only").
type ElementRule struct {
	ID          int64  `json:"id"`
	ElementID   int64  `json:"element_id"`
	SubjectType string `json:"subject_type"`
	SubjectID   *int64 `json:"subject_id"`
	Permissions Perm   `json:"permissions"`
	// Actions are named category permissions of plugins (comments …) for
	// this element.
	Actions []string `json:"actions"`
}

// ElementPerms returns the effective permissions of p on an element in
// categoryID (the element's category as stored). Every check of an element
// goes through it (or through ElementFilter for lists):
//   - without own settings of the element: the permissions on the category;
//   - element rules add to them;
//   - "own rights only": the category's rules do not apply, except for
//     administrators and managers of the category by a rule;
//   - the strictest classification of the category and the element applies;
//   - archived, trashed categories and API keys narrow as for categories.
func (s *Service) ElementPerms(ctx context.Context, p *identity.Principal, elementID, categoryID int64) (Perm, error) {
	sn, err := s.load(ctx)
	if err != nil {
		return None, err
	}
	return s.elemPerms(sn, p, elementID, categoryID), nil
}

// CanElement reports whether p has all bits of want on an element.
func (s *Service) CanElement(ctx context.Context, p *identity.Principal, elementID, categoryID int64, want Perm) bool {
	got, err := s.ElementPerms(ctx, p, elementID, categoryID)
	if err != nil {
		s.log.Error("acl check failed", "err", err)
		return false
	}
	return got&want == want
}

func (s *Service) elemPerms(sn *snapshot, p *identity.Principal, elementID, categoryID int64) Perm {
	out := s.perms(sn, p, categoryID)
	anc, ok := sn.ancestors[categoryID]
	if !ok {
		return None
	}
	admin := p != nil && p.IsAdmin
	manager := admin || s.manageByRule(sn, p, categoryID)
	ei := sn.elems[elementID]
	if ei != nil && !admin {
		for _, cid := range anc {
			if sn.deleted[cid] {
				return None
			}
		}
		if ei.own && !manager {
			out = None
		}
		for _, r := range ei.rules {
			if matches(Rule{SubjectType: r.SubjectType, SubjectID: r.SubjectID}, p) {
				out |= r.Permissions & ElementBits
			}
		}
	}
	if manager && out&Manage != 0 {
		out |= Grant
	}
	if out == None {
		return None
	}
	if ei != nil {
		level := max(sn.level(anc), ei.class)
		if (p == nil || p.UserID <= 0) && level >= Internal {
			return None
		}
	}
	if sn.isArchived(anc) {
		out &= View | Manage
	}
	if p != nil && p.Via == "apikey" {
		mask := Perm(p.KeyPerms)
		if mask&Manage != 0 {
			mask |= Grant
		}
		out &= mask
		if p.KeyCategory != 0 && !contains(anc, p.KeyCategory) {
			return None
		}
	}
	return out
}

// ElementActions returns the named category permissions of p on an element:
// those of its category (not with "own rights only") and those of the
// element's rules; nothing without View on the element.
func (s *Service) ElementActions(ctx context.Context, p *identity.Principal, elementID, categoryID int64) ([]string, error) {
	sn, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	return s.elemActions(sn, p, elementID, categoryID), nil
}

func (s *Service) elemActions(sn *snapshot, p *identity.Principal, elementID, categoryID int64) []string {
	ei := sn.elems[elementID]
	if ei == nil {
		return s.actions(sn, p, categoryID)
	}
	if s.elemPerms(sn, p, elementID, categoryID)&View == 0 {
		return nil
	}
	anc, ok := sn.ancestors[categoryID]
	if !ok || sn.isArchived(anc) {
		return nil
	}
	if p != nil && p.IsAdmin {
		return s.actions(sn, p, categoryID) // includes the API key checks
	}
	if p != nil && p.Via == "apikey" && (Perm(p.KeyPerms)&Edit == 0 || (p.KeyCategory != 0 && !contains(anc, p.KeyCategory))) {
		return nil
	}
	set := map[string]bool{}
	if !ei.own || s.manageByRule(sn, p, categoryID) {
		for _, a := range s.actions(sn, p, categoryID) {
			set[a] = true
		}
	}
	for _, r := range ei.rules {
		if matches(Rule{SubjectType: r.SubjectType, SubjectID: r.SubjectID}, p) {
			for _, a := range r.Actions {
				set[a] = true
			}
		}
	}
	out := perm.Expand(keysOf(set))
	slices.Sort(out)
	return out
}

// CanElementAction reports whether p has the named category permission
// action on an element.
func (s *Service) CanElementAction(ctx context.Context, p *identity.Principal, elementID, categoryID int64, action string) bool {
	if d, ok := perm.Lookup(action); !ok || d.Scope != perm.Category {
		s.log.Error("permission check for an undeclared category permission – declare it in plugin.json", "permission", action)
		return false
	}
	acts, err := s.ElementActions(ctx, p, elementID, categoryID)
	if err != nil {
		s.log.Error("acl check failed", "err", err)
		return false
	}
	return slices.Contains(acts, action)
}

// ElementClassification returns the classification of an element: the
// strictest of its category (and above) and its own.
func (s *Service) ElementClassification(ctx context.Context, elementID, categoryID int64) int {
	sn, err := s.load(ctx)
	if err != nil {
		return StrictConfidential // fail closed
	}
	l := sn.level(sn.ancestors[categoryID])
	if ei := sn.elems[elementID]; ei != nil && ei.class > l {
		l = ei.class
	}
	return l
}

// ElementFilter restricts a query on elements to those p has a permission
// on. Build the condition with SQL.
type ElementFilter struct {
	All   bool    // no restriction by category (administrators)
	Cats  []int64 // categories with the permission (for elements without own settings)
	Hide  []int64 // elements in these categories without the permission
	Extra []int64 // elements outside these categories with the permission
}

// ElementFilter returns the filter for elements on which p has want.
func (s *Service) ElementFilter(ctx context.Context, p *identity.Principal, want Perm) (ElementFilter, error) {
	sn, err := s.load(ctx)
	if err != nil {
		return ElementFilter{}, err
	}
	f := ElementFilter{Cats: []int64{}, Hide: []int64{}, Extra: []int64{}}
	if p != nil && p.IsAdmin && p.Via != "apikey" {
		f.All = true
		return f, nil
	}
	ok := map[int64]bool{}
	for id := range sn.ancestors {
		if s.perms(sn, p, id)&want == want {
			ok[id] = true
			f.Cats = append(f.Cats, id)
		}
	}
	for id, ei := range sn.elems {
		has := s.elemPerms(sn, p, id, ei.cat)&want == want
		switch {
		case ok[ei.cat] && !has:
			f.Hide = append(f.Hide, id)
		case !ok[ei.cat] && has:
			f.Extra = append(f.Extra, id)
		}
	}
	slices.Sort(f.Cats)
	slices.Sort(f.Hide)
	slices.Sort(f.Extra)
	return f, nil
}

// SQL returns the condition for the element alias (e.g. "e") and appends
// its arguments to args: elements in Cats that are not hidden, or in Extra.
func (f ElementFilter) SQL(alias string, args *[]any) string {
	return f.SQLWith(alias, func(v any) string {
		*args = append(*args, v)
		return "$" + strconv.Itoa(len(*args))
	})
}

// SQLWith is SQL for query builders: arg adds an argument and returns its
// placeholder.
func (f ElementFilter) SQLWith(alias string, arg func(any) string) string {
	if f.All {
		return "TRUE"
	}
	a, b, c := arg(f.Cats), arg(f.Hide), arg(f.Extra)
	return "((" + alias + ".category_id = ANY(" + a + "::bigint[]) AND NOT (" + alias + ".id = ANY(" + b + "::bigint[]))) OR " + alias + ".id = ANY(" + c + "::bigint[]))"
}

// Allows reports whether the filter lets an element through (for results
// that were not filtered in the query).
func (f ElementFilter) Allows(elementID, categoryID int64) bool {
	if f.All {
		return true
	}
	if _, found := slices.BinarySearch(f.Extra, elementID); found {
		return true
	}
	if _, found := slices.BinarySearch(f.Cats, categoryID); !found {
		return false
	}
	_, hidden := slices.BinarySearch(f.Hide, elementID)
	return !hidden
}

// Restricted reports whether an element has own settings (rules, "own
// rights only" or an own classification); its moves must invalidate the
// snapshot.
func (s *Service) Restricted(ctx context.Context, elementID int64) bool {
	sn, err := s.load(ctx)
	if err != nil {
		return true
	}
	return sn.elems[elementID] != nil
}

// ElementRules returns the rules of an element and whether only they apply.
func (s *Service) ElementRules(ctx context.Context, elementID int64) (rules []ElementRule, own bool, class int, err error) {
	sn, err := s.load(ctx)
	if err != nil {
		return nil, false, 0, err
	}
	rules = []ElementRule{}
	if ei := sn.elems[elementID]; ei != nil {
		rules = append(rules, ei.rules...)
		own, class = ei.own, ei.class
	}
	return rules, own, class, nil
}

// ElementViewers is Viewers for elements with own settings: the subjects
// that may view the element (for a search index that stores viewers).
func (s *Service) ElementViewers(ctx context.Context, elementID, categoryID int64) ([]string, error) {
	sn, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	return sn.elemViewers(elementID, categoryID), nil
}

func (sn *snapshot) elemViewers(elementID, categoryID int64) []string {
	ei := sn.elems[elementID]
	if ei == nil {
		return sn.viewers(categoryID)
	}
	anc, ok := sn.ancestors[categoryID]
	if !ok {
		return []string{}
	}
	for _, cid := range anc {
		if sn.deleted[cid] {
			return []string{}
		}
	}
	internal := max(sn.level(anc), ei.class) >= Internal
	seen := map[string]bool{}
	out := []string{}
	add := func(t string) {
		if t == "guest" && internal {
			t = "authenticated"
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	if ei.own {
		// managers of the category by a rule keep access
		for i, cid := range anc {
			self := i == len(anc)-1
			for _, r := range sn.rules[cid] {
				if (self || r.Inherit) && r.Permissions&Manage != 0 && r.Permissions&View != 0 {
					add(subjectToken(r.SubjectType, r.SubjectID))
				}
			}
		}
	} else {
		for _, t := range sn.viewers(categoryID) {
			add(t)
		}
	}
	for _, r := range ei.rules {
		if r.Permissions&View != 0 {
			add(subjectToken(r.SubjectType, r.SubjectID))
		}
	}
	slices.Sort(out)
	return out
}

func subjectToken(t string, id *int64) string {
	if id != nil {
		return t + ":" + strconv.FormatInt(*id, 10)
	}
	return t
}

// SubjectElementRules are the rules of one subject on one element.
type SubjectElementRules struct {
	ElementID     int64         `json:"id"`
	CategoryID    int64         `json:"category_id"`
	OwnRightsOnly bool          `json:"own_rights_only"`
	Rules         []ElementRule `json:"rules"`
}

// ElementRulesOf returns the element rules of a subject (for the permission
// overview), per element.
func (s *Service) ElementRulesOf(ctx context.Context, subjectType string, subjectID *int64) ([]SubjectElementRules, error) {
	sn, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := []SubjectElementRules{}
	for id, ei := range sn.elems {
		var mine []ElementRule
		for _, r := range ei.rules {
			if r.SubjectType == subjectType && (subjectID == nil || (r.SubjectID != nil && *r.SubjectID == *subjectID)) {
				mine = append(mine, r)
			}
		}
		if len(mine) > 0 {
			out = append(out, SubjectElementRules{ElementID: id, CategoryID: ei.cat, OwnRightsOnly: ei.own, Rules: mine})
		}
	}
	slices.SortFunc(out, func(a, b SubjectElementRules) int { return cmp.Compare(a.ElementID, b.ElementID) })
	return out, nil
}
