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
// Name is the display name only – never the handle (user@provider), which
// can be an e-mail address (with OIDC often the UPN); the handle appears
// only on the person page and in the administration of users. ID is set
// only if the reader may open the person (person page, menu on the name:
// core.people.view and the rules for finding people) – guests never get
// it.
type PersonRef struct {
	Name string
	ID   *int64
}

// PeopleRefs returns the display name and, if the reader may open the
// person, the id of each of the users ids (see PersonRef). One query for
// all ids.
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
	var all, own, view bool
	if p != nil && p.ID() > 0 {
		me = p.ID()
		all, own = perm.HasP(p, "core.users.list_all"), perm.HasP(p, "core.users.list_own")
		view = perm.HasP(p, "core.people.view") && (all || own)
	}
	// open: the reader may find the person (the same as /users/lookup and
	// auth.Service.Viewable) and has core.people.view
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.display_name,
		  $5 AND $2 > 0 AND NOT u.disabled AND (u.id = $2 OR $3
		    OR ($4 AND EXISTS (SELECT 1 FROM user_groups a JOIN user_groups b ON a.group_id = b.group_id WHERE a.user_id = u.id AND b.user_id = $2)))
		FROM users u WHERE u.id = ANY($1)`, ids, me, all, own, view)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		var open bool
		if err := rows.Scan(&id, &name, &open); err != nil {
			return nil, err
		}
		r := PersonRef{Name: name}
		if open {
			v := id
			r.ID = &v
		}
		out[id] = r
	}
	return out, rows.Err()
}

// PeopleLabels returns the display names of the users ids ("" if a person
// has none; never the handle, see PersonRef). Users that do not exist are
// left out. One query for all ids.
func (s *Service) PeopleLabels(ctx context.Context, ids []int64) (map[int64]string, error) {
	refs, err := s.PeopleRefs(ctx, ids)
	out := make(map[int64]string, len(refs))
	for id, r := range refs {
		out[id] = r.Name
	}
	return out, err
}
