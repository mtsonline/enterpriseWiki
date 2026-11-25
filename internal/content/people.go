package content

import (
	"context"
	"slices"

	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// PeopleNamesMax is the most people PeopleNames looks up at once.
const PeopleNamesMax = 1000

// PeopleNames returns the names of those of the users ids that the reader
// may see, by the rules of the people blocks: guests see nobody; signed-in
// people see themselves, everybody with core.users.list_all, and the
// members of their own groups with core.users.list_own. Disabled users and
// users that do not exist are left out (the same for both, so nothing is
// told about them). One query for all ids.
func (s *Service) PeopleNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	p := identity.From(ctx)
	if p == nil || p.ID() == 0 || len(ids) == 0 {
		return out, nil
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > PeopleNamesMax {
		ids = ids[:PeopleNamesMax]
	}
	all, own := perm.HasP(p, "core.users.list_all"), perm.HasP(p, "core.users.list_own")
	rows, err := s.pool.Query(ctx, `SELECT u.id, COALESCE(NULLIF(u.display_name, ''), u.username) FROM users u
		WHERE u.id = ANY($1) AND NOT u.disabled AND (u.id = $2 OR $3
		  OR ($4 AND EXISTS (SELECT 1 FROM user_groups a JOIN user_groups b ON a.group_id = b.group_id WHERE a.user_id = u.id AND b.user_id = $2)))`,
		ids, p.ID(), all, own)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// PersonRef is how a person is named for the reader next to content:
// Name is the label (see PeopleLabels); ID is set only if the reader may
// open the person (person page, menu on the name: core.people.view and the
// rules for finding people) – guests never get it.
type PersonRef struct {
	Name string
	ID   *int64
}

// PeopleRefs returns the label and, if the reader may open the person, the
// id of each of the users ids (see PersonRef). One query for all ids.
func (s *Service) PeopleRefs(ctx context.Context, ids []int64) (map[int64]PersonRef, error) {
	out := map[int64]PersonRef{}
	if len(ids) == 0 {
		return out, nil
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > PeopleNamesMax {
		ids = ids[:PeopleNamesMax]
	}
	p := identity.From(ctx)
	var me int64
	var all, own, manage, view bool
	if p != nil && p.ID() > 0 {
		me = p.ID()
		all, own, manage = perm.HasP(p, "core.users.list_all"), perm.HasP(p, "core.users.list_own"), perm.HasP(p, "core.users.manage")
		view = perm.HasP(p, "core.people.view") && (all || own)
	} else if p.IsCLI() {
		manage = true // the CLI without --as sees the handles (no ids: no menu there)
	}
	// rule: the reader may find the person (the same as /users/lookup and
	// auth.Service.Viewable)
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.display_name, u.username || '@' || u.provider,
		  $2 > 0 AND NOT u.disabled AND (u.id = $2 OR $3
		    OR ($4 AND EXISTS (SELECT 1 FROM user_groups a JOIN user_groups b ON a.group_id = b.group_id WHERE a.user_id = u.id AND b.user_id = $2)))
		FROM users u WHERE u.id = ANY($1)`, ids, me, all, own)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, handle string
		var rule bool
		if err := rows.Scan(&id, &name, &handle, &rule); err != nil {
			return nil, err
		}
		r := PersonRef{Name: PersonLabel(name, handle, rule || manage)}
		if rule && view {
			v := id
			r.ID = &v
		}
		out[id] = r
	}
	return out, rows.Err()
}

// PeopleLabels returns how the users ids are named for the reader of ctx
// next to content (author, last change, history, comments, cases): the
// display name, followed by the handle (user@provider) only for people the
// reader may look up – by the rules of PeopleNames, and every account for
// who manages users. A handle can be an e-mail address (with OIDC often the
// UPN), so guests and readers who may not find the person get the display
// name only ("" if the person has none). Users that do not exist are left
// out. One query for all ids.
func (s *Service) PeopleLabels(ctx context.Context, ids []int64) (map[int64]string, error) {
	refs, err := s.PeopleRefs(ctx, ids)
	out := make(map[int64]string, len(refs))
	for id, r := range refs {
		out[id] = r.Name
	}
	return out, err
}

// PersonLabel names a person: the display name, with the handle only if
// showHandle (see PeopleLabels).
func PersonLabel(name, handle string, showHandle bool) string {
	switch {
	case !showHandle:
		return name
	case name == "":
		return handle
	}
	return name + " (" + handle + ")"
}
