package identity

import (
	"context"
	"testing"
)

// Two apps of the same site in one process: each finds its own value; a
// context without a scope gets the newest open one; a closed app no longer
// shadows the other (the "closed pool" of 15.11.2026).
func TestPerScope(t *testing.T) {
	var p PerScope[string]
	a, b := NewScope(), NewScope()
	p.Set(a, "main", "first")
	p.Set(b, "main", "second")
	ctxA := WithScope(WithSite(context.Background(), "main"), a)
	ctxB := WithScope(WithSite(context.Background(), "main"), b)
	if v, _ := p.Get(ctxA); v != "first" {
		t.Fatalf("a: %q", v)
	}
	if v, _ := p.Get(ctxB); v != "second" {
		t.Fatalf("b: %q", v)
	}
	if v, _ := p.Get(context.Background()); v != "second" {
		t.Fatalf("without scope: %q", v)
	}
	p.Remove(b)
	if v, _ := p.Get(context.Background()); v != "first" {
		t.Fatalf("after closing b: %q", v)
	}
	if v, _ := p.Get(ctxB); v != "first" { // b is closed: the open app of the site
		t.Fatalf("closed scope: %q", v)
	}
	if _, ok := p.Get(WithSite(context.Background(), "other")); ok {
		t.Fatal("another site")
	}
	p.Remove(a)
	if _, ok := p.Get(ctxA); ok {
		t.Fatal("all closed")
	}
}
