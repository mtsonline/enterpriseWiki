package core_template

import (
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// TestRightsMay: the rights of a template only narrow down; without
// core.template.design they give nothing (also when the route would not
// check it), administrators always may, managers may also change it.
func TestRightsMay(t *testing.T) {
	_ = perm.Register(ID, perm.Def{Key: permDesign, Scope: perm.Global, Label: "Design templates"}) // as the kernel does from plugin.json
	id := func(n int64) *int64 { return &n }
	designer := func(uid int64, groups ...int64) *identity.Principal {
		return &identity.Principal{UserID: uid, Groups: groups, Perms: []string{permDesign}}
	}
	dana, erik, team := designer(1), designer(2), designer(3, 50)
	olaf := &identity.Principal{UserID: 4} // named, but no designer
	admin := &identity.Principal{UserID: 5, IsAdmin: true}
	open := Rights{}
	limited := Rights{EditSubjects: []acl.Subject{{Type: "user", ID: id(2)}, {Type: "user", ID: id(4)}, {Type: "group", ID: id(50)}}, ManageSubjects: []acl.Subject{{Type: "user", ID: id(1)}}}
	onlyManagers := Rights{ManageSubjects: []acl.Subject{{Type: "user", ID: id(1)}}}
	for _, c := range []struct {
		name string
		r    Rights
		p    *identity.Principal
		want May
	}{
		{"open, a designer", open, erik, May{true, true}},
		{"open, no designer", open, olaf, May{}},
		{"open, guest", open, nil, May{}},
		{"limited, the manager", limited, dana, May{true, true}},
		{"limited, may change", limited, erik, May{true, false}},
		{"limited, by the group", limited, team, May{true, false}},
		{"limited, named but no designer", limited, olaf, May{}},
		{"limited, administrator", limited, admin, May{true, true}},
		{"only managers named, the manager", onlyManagers, dana, May{true, true}},
		{"only managers named, another designer", onlyManagers, erik, May{}},
	} {
		if got := c.r.may(c.p); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	// subjects: no guests, no duplicates
	if _, err := checkSubjects([]acl.Subject{{Type: "guest"}}); err == nil {
		t.Error("guest accepted")
	}
	if got, err := checkSubjects([]acl.Subject{{Type: "user", ID: id(1)}, {Type: "user", ID: id(1)}, {Type: "authenticated"}, {Type: "authenticated"}}); err != nil || len(got) != 2 {
		t.Errorf("duplicates: %v %v", got, err)
	}
}
