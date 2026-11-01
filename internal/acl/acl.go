// Package acl evaluates category permissions.
//
// Rules are attached to a category (by its stable id) and grant a permission
// bitmask to a subject: a user, a group, a role, all guests or all
// authenticated users. A rule with inherit=true also applies to every
// subcategory. Effective permissions are the union of all matching rules on
// the category and its ancestors. Admin roles bypass all checks. Without a
// matching rule nothing is allowed (deny by default).
//
// All rules and the category ancestry are held in an immutable in-memory
// snapshot that is rebuilt after changes (and invalidated on every node in
// cluster mode), so a permission check needs no database round trip.
package acl

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/events"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Perm is a permission bitmask.
type Perm int

// Permission bits.
const (
	View   Perm = 1 << iota // read the category and its elements
	Edit                    // edit elements and category texts
	Create                  // create elements and subcategories
	Delete                  // delete / move away elements and subcategories
	Manage                  // manage permissions and category settings
	None   Perm = 0
	All         = View | Edit | Create | Delete | Manage
)

// Names maps permission bits to names (used by API and UI).
var Names = map[Perm]string{View: "view", Edit: "edit", Create: "create", Delete: "delete", Manage: "manage"}

// Strings returns the names of the set bits.
func (p Perm) Strings() []string {
	out := []string{}
	for _, b := range []Perm{View, Edit, Create, Delete, Manage} {
		if p&b != 0 {
			out = append(out, Names[b])
		}
	}
	return out
}

// Rule is one ACL rule.
type Rule struct {
	ID          int64  `json:"id"`
	CategoryID  int64  `json:"category_id"`
	SubjectType string `json:"subject_type"`
	SubjectID   *int64 `json:"subject_id"`
	Permissions Perm   `json:"permissions"`
	Inherit     bool   `json:"inherit"`
}

type snapshot struct {
	ancestors map[int64][]int64 // category id -> path ids (root..self)
	deleted   map[int64]bool
	rules     map[int64][]Rule
}

// Service evaluates permissions.
type Service struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	snap atomic.Pointer[snapshot]
	mu   sync.Mutex // serializes rebuilds
}

// New creates the ACL service.
func New(pool *pgxpool.Pool, bus *events.Bus, log *slog.Logger) *Service {
	s := &Service{pool: pool, log: log}
	inv := func(string) { s.snap.Store(nil) }
	bus.Subscribe("acl", inv)
	bus.Subscribe("*", inv)
	return s
}

// Invalidate drops the snapshot (local node only; publish "acl" for cluster).
func (s *Service) Invalidate() { s.snap.Store(nil) }

func (s *Service) load(ctx context.Context) (*snapshot, error) {
	if sn := s.snap.Load(); sn != nil {
		return sn, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sn := s.snap.Load(); sn != nil {
		return sn, nil
	}
	sn := &snapshot{ancestors: map[int64][]int64{}, deleted: map[int64]bool{}, rules: map[int64][]Rule{}}
	rows, err := s.pool.Query(ctx, `SELECT id, path::text, deleted_at IS NOT NULL FROM categories`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var path string
		var del bool
		if err := rows.Scan(&id, &path, &del); err != nil {
			rows.Close()
			return nil, err
		}
		parts := strings.Split(path, ".")
		anc := make([]int64, 0, len(parts))
		for _, p := range parts {
			v, _ := strconv.ParseInt(p, 10, 64)
			anc = append(anc, v)
		}
		sn.ancestors[id] = anc
		if del {
			sn.deleted[id] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT id, category_id, subject_type, subject_id, permissions, inherit FROM acl_rules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.CategoryID, &r.SubjectType, &r.SubjectID, &r.Permissions, &r.Inherit); err != nil {
			return nil, err
		}
		sn.rules[r.CategoryID] = append(sn.rules[r.CategoryID], r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.snap.Store(sn)
	return sn, nil
}

func contains(xs []int64, v int64) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func matches(r Rule, p *identity.Principal) bool {
	switch r.SubjectType {
	case "guest":
		return true // guest rules apply to everybody (logged in users see public content too)
	case "authenticated":
		return p != nil && p.UserID != 0
	case "user":
		return p != nil && r.SubjectID != nil && *r.SubjectID == p.UserID
	case "group":
		return p != nil && r.SubjectID != nil && contains(p.Groups, *r.SubjectID)
	case "role":
		return p != nil && r.SubjectID != nil && contains(p.Roles, *r.SubjectID)
	}
	return false
}

// Perms returns the effective permissions of p on category id.
// Deleted (trashed) categories grant nothing except to admins.
func (s *Service) Perms(ctx context.Context, p *identity.Principal, categoryID int64) (Perm, error) {
	sn, err := s.load(ctx)
	if err != nil {
		return None, err
	}
	return s.perms(sn, p, categoryID), nil
}

func (s *Service) perms(sn *snapshot, p *identity.Principal, categoryID int64) Perm {
	anc, ok := sn.ancestors[categoryID]
	if !ok {
		return None
	}
	var out Perm
	if p != nil && p.IsAdmin {
		// Administrators may access all content. For API keys the key's
		// permission mask below still applies; administrative endpoints
		// additionally require allow_admin (identity.Principal.Admin).
		out = All
	} else {
		for i, cid := range anc {
			self := i == len(anc)-1
			for _, r := range sn.rules[cid] {
				if (self || r.Inherit) && matches(r, p) {
					out |= r.Permissions
				}
			}
		}
		for _, cid := range anc {
			if sn.deleted[cid] {
				return None
			}
		}
	}
	// API keys narrow permissions.
	if p != nil && p.Via == "apikey" {
		out &= Perm(p.KeyPerms)
		if p.KeyCategory != 0 && !contains(anc, p.KeyCategory) {
			return None
		}
	}
	return out
}

// Can reports whether p has all bits of want on category id.
func (s *Service) Can(ctx context.Context, p *identity.Principal, categoryID int64, want Perm) bool {
	got, err := s.Perms(ctx, p, categoryID)
	if err != nil {
		s.log.Error("acl check failed", "err", err)
		return false
	}
	return got&want == want
}

// Filter returns the subset of ids on which p has want.
func (s *Service) Filter(ctx context.Context, p *identity.Principal, ids []int64, want Perm) ([]int64, error) {
	sn, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if s.perms(sn, p, id)&want == want {
			out = append(out, id)
		}
	}
	return out, nil
}

// PermsMap returns permissions for many categories with a single snapshot.
func (s *Service) PermsMap(ctx context.Context, p *identity.Principal, ids []int64) (map[int64]Perm, error) {
	sn, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]Perm, len(ids))
	for _, id := range ids {
		out[id] = s.perms(sn, p, id)
	}
	return out, nil
}

// Rules returns the rules attached directly to categoryID and the rules
// inherited from its ancestors.
func (s *Service) Rules(ctx context.Context, categoryID int64) (own []Rule, inherited []Rule, err error) {
	sn, err := s.load(ctx)
	if err != nil {
		return nil, nil, err
	}
	anc, ok := sn.ancestors[categoryID]
	if !ok {
		return nil, nil, fmt.Errorf("category %d not found", categoryID)
	}
	own = append([]Rule{}, sn.rules[categoryID]...)
	inherited = []Rule{}
	for _, cid := range anc[:len(anc)-1] {
		for _, r := range sn.rules[cid] {
			if r.Inherit {
				inherited = append(inherited, r)
			}
		}
	}
	return own, inherited, nil
}

// ParsePerms parses names ("view,edit") or a number into a bitmask.
func ParsePerms(s string) (Perm, error) {
	if n, err := strconv.Atoi(s); err == nil {
		if n < 1 || Perm(n) > All {
			return 0, fmt.Errorf("permissions out of range")
		}
		return Perm(n), nil
	}
	var p Perm
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		found := false
		for b, n := range Names {
			if n == part {
				p |= b
				found = true
			}
		}
		if part == "all" {
			p, found = All, true
		}
		if !found {
			return 0, fmt.Errorf("unknown permission %q", part)
		}
	}
	return p, nil
}
