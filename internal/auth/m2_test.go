package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTOTPRFC6238(t *testing.T) {
	// RFC 6238 test secret "12345678901234567890"
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for ts, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924"} {
		got, err := TOTPCode(secret, ts/30)
		if err != nil || got != want {
			t.Errorf("t=%d: %s, want %s", ts, got, want)
		}
	}
	now := time.Unix(1234567890, 0)
	if _, ok := VerifyTOTP(secret, "005924", now, 0); !ok {
		t.Fatal("valid code rejected")
	}
	if _, ok := VerifyTOTP(secret, "005924", now, now.Unix()/30); ok {
		t.Fatal("replay accepted")
	}
	if _, ok := VerifyTOTP(secret, "123456", now, 0); ok {
		t.Fatal("wrong code accepted")
	}
}

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
