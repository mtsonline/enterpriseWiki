package server_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

// TestLoadPrincipals: several people with one query, in the order asked,
// each once; people that do not exist or are disabled are left out, as
// when they are loaded one by one.
func TestLoadPrincipals(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var a, b, c struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "pa", "password": "Passw0rd!Passw0rd"}, &a, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "pb", "password": "Passw0rd!Passw0rd"}, &b, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "pc", "password": "Passw0rd!Passw0rd"}, &c, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", c.ID), map[string]any{"disabled": true}, nil, 200)
	ctx := context.Background()
	ps, err := e.app.Auth.LoadPrincipalsQ(ctx, e.app.DB, []int64{b.ID, 999999, c.ID, a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].UserID != b.ID || ps[1].UserID != a.ID || ps[0].Username != "pb" {
		t.Fatalf("loaded: %+v", ps)
	}
	// the same as one by one: groups, roles, permissions
	one, err := e.app.Auth.LoadPrincipalQ(ctx, e.app.DB, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*one, *ps[1]) {
		t.Fatalf("differs from LoadPrincipalQ:\n%+v\n%+v", one, ps[1])
	}
	if ps, err := e.app.Auth.LoadPrincipalsQ(ctx, e.app.DB, nil); err != nil || len(ps) != 0 {
		t.Fatalf("none: %v %v", ps, err)
	}
}
