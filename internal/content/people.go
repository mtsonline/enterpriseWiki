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
