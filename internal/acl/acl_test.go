package acl

import (
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
