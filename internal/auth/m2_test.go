package auth

import (
	"strings"
	"testing"
)

func TestMapping(t *testing.T) {
	m := MappingFromSettings(map[string]any{
		"group_mapping":        map[string]any{"CN=HR,OU=Groups,DC=x": "Human Resources", "ignored": ""},
		"role_mapping":         map[string]any{"wiki-admins": "Administrators"},
		"default_groups":       []any{"Staff"},
		"keep_unmapped_groups": true,
	})
	id := &ExternalIdentity{Groups: []string{"cn=hr,ou=groups,dc=x", "cn=Wiki-Admins,ou=g,dc=x", "ignored", "cn=Sales,ou=g,dc=x"}}
	m.Apply(id)
	if got := strings.Join(id.Groups, ","); got != "Human Resources,Sales,Staff,Wiki-Admins" {
		t.Fatalf("groups %s", got)
	}
	if strings.Join(id.Roles, ",") != "Administrators" {
		t.Fatalf("roles %v", id.Roles)
	}
	m.KeepUnmappedGroups = false
	id = &ExternalIdentity{Groups: []string{"other"}}
	m.Apply(id)
	if strings.Join(id.Groups, ",") != "Staff" {
		t.Fatalf("unmapped kept: %v", id.Groups)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{"/c/1": "/c/1", "//evil": "/", "https://evil": "/", "/\\evil": "/", "": "/"} {
		if got := SafeNext(in); got != want {
			t.Errorf("%q -> %q", in, got)
		}
	}
}
