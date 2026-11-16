package acl

import (
	"context"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/identity"
)

func id(v int64) *int64 { return &v }

// Tree: 1 -> 2 -> 3, and 4 (separate root).
func snap() *snapshot {
	return &snapshot{
		ancestors: map[int64][]int64{1: {1}, 2: {1, 2}, 3: {1, 2, 3}, 4: {4}},
		deleted:   map[int64]bool{},
		rules: map[int64][]Rule{
			1: {{CategoryID: 1, SubjectType: "guest", Permissions: View, Inherit: false},
				{CategoryID: 1, SubjectType: "group", SubjectID: id(10), Permissions: View | Edit, Inherit: true}},
			2: {{CategoryID: 2, SubjectType: "user", SubjectID: id(5), Permissions: Manage, Inherit: false}},
			4: {{CategoryID: 4, SubjectType: "role", SubjectID: id(7), Permissions: All, Inherit: true}},
		},
	}
}

func TestPerms(t *testing.T) {
	s := &Service{}
	sn := snap()
	member := &identity.Principal{UserID: 5, Groups: []int64{10}}
	cases := []struct {
		name string
		p    *identity.Principal
		cat  int64
		want Perm
	}{
		{"guest sees first level", nil, 1, View},
		{"guest rule does not inherit", nil, 2, None},
		{"group rule inherits", member, 3, View | Edit},
		{"user rule on own category", member, 2, View | Edit | Manage},
		{"non-inheriting user rule", member, 3, View | Edit},
		{"role on other root", &identity.Principal{UserID: 9, Roles: []int64{7}}, 4, All},
		{"no rule, no access", &identity.Principal{UserID: 9}, 3, None},
		{"admin", &identity.Principal{UserID: 1, IsAdmin: true}, 3, All},
		{"unknown category", member, 99, None},
		{"api key mask", &identity.Principal{UserID: 5, Groups: []int64{10}, Via: "apikey", KeyPerms: int(View)}, 3, View},
		{"api key subtree", &identity.Principal{UserID: 5, Groups: []int64{10}, Via: "apikey", KeyPerms: int(All), KeyCategory: 2}, 1, None},
	}
	for _, c := range cases {
		if got := s.perms(sn, c.p, c.cat); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got.Strings(), c.want.Strings())
		}
	}
	sn.deleted[2] = true
	if got := s.perms(sn, member, 3); got != None {
		t.Errorf("trashed subtree still accessible: %v", got.Strings())
	}
}

func TestParsePerms(t *testing.T) {
	if p, err := ParsePerms("view,edit"); err != nil || p != View|Edit {
		t.Fatal(p, err)
	}
	if p, _ := ParsePerms("all"); p != All {
		t.Fatal(p)
	}
	if _, err := ParsePerms("fly"); err == nil {
		t.Fatal("unknown permission accepted")
	}
}

// TestRebuildDuringChange: a rebuild that read the database before a
// change (and its invalidation) is not kept. Before 16.11.2026 it was
// stored, so a new category stayed invisible to its creator (404) and a
// revoked permission could stay in force until the next change.
func TestRebuildDuringChange(t *testing.T) {
	stale, fresh := snap(), snap()
	fresh.ancestors[5] = []int64{5} // the category created meanwhile
	s := &Service{}
	calls := 0
	s.builder = func(context.Context) (*snapshot, error) {
		calls++
		if calls == 1 {
			s.Invalidate() // the change commits while the first rebuild reads
			return stale, nil
		}
		return fresh, nil
	}
	got, err := s.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != fresh || s.snap.Load() != fresh {
		t.Fatalf("the rebuild from before the change was kept (calls %d)", calls)
	}
	// later loads use the stored snapshot
	if got, _ := s.load(context.Background()); got != fresh || calls != 2 {
		t.Fatalf("calls %d", calls)
	}
}

// Viewers and Tokens decide in the search index what perms decides in the
// wiki: both must agree for every person and category.
func TestViewersMatchPerms(t *testing.T) {
	s := &Service{}
	sn := snap()
	sn.ancestors[5] = []int64{4, 5}
	sn.ancestors[6] = []int64{6}
	sn.class = map[int64]int{6: Internal}
	sn.rules[6] = []Rule{{CategoryID: 6, SubjectType: "guest", Permissions: View, Inherit: true}}
	sn.rules[5] = []Rule{{CategoryID: 5, SubjectType: "authenticated", Permissions: Edit, Inherit: true}} // no view bit
	people := []*identity.Principal{nil, {UserID: 5, Groups: []int64{10}}, {UserID: 9, Roles: []int64{7}}, {UserID: 9},
		{UserID: 3, Groups: []int64{11}, Roles: []int64{8}}}
	check := func() {
		t.Helper()
		for _, p := range people {
			tokens := map[string]bool{}
			for _, x := range Tokens(p) {
				tokens[x] = true
			}
			for cat := range sn.ancestors {
				match := false
				for _, v := range sn.viewers(cat) {
					match = match || tokens[v]
				}
				if want := s.perms(sn, p, cat)&View != 0; match != want {
					t.Errorf("person %+v, category %d: index says %v, wiki says %v (viewers %v)", p, cat, match, want, sn.viewers(cat))
				}
			}
		}
	}
	check()
	sn.deleted[2] = true
	check()
	if Tokens(&identity.Principal{UserID: 1, IsAdmin: true}) != nil {
		t.Error("administrators need no restriction")
	}
	if Tokens(&identity.Principal{UserID: 1, IsAdmin: true, Via: "apikey"}) == nil {
		t.Error("API keys of administrators are narrowed")
	}
}

func TestVisible(t *testing.T) {
	s := &Service{}
	sn := snap()
	s.snap.Store(sn)
	ids, all, err := s.Visible(context.Background(), &identity.Principal{UserID: 9, Roles: []int64{7}})
	// the guest rule on 1 applies to everybody
	if err != nil || all || len(ids) != 2 || ids[0] != 1 || ids[1] != 4 {
		t.Fatalf("visible: %v %v %v", ids, all, err)
	}
	if ids, all, _ = s.Visible(context.Background(), nil); all || len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("guest: %v %v", ids, all)
	}
	if _, all, _ = s.Visible(context.Background(), &identity.Principal{UserID: 1, IsAdmin: true}); !all {
		t.Fatal("admin restricted")
	}
}
