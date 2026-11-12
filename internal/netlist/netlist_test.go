package netlist

import (
	"context"
	"errors"
	"testing"
)

func TestParseAndAllowed(t *testing.T) {
	list, err := Parse([]string{" 192.0.2.0/24 ", "2001:db8::1", "Proxy.Example.org", ""})
	if err != nil || len(list) != 3 || list[2] != "proxy.example.org" {
		t.Fatalf("parse: %v %v", list, err)
	}
	for _, bad := range []string{"300.1.1.1/8", "a b", "http://x"} {
		if _, err := Parse([]string{bad}); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	calls := 0
	Lookup = func(ctx context.Context, host string) ([]string, error) {
		calls++
		if host == "proxy.example.org" {
			return []string{"198.51.100.9"}, nil
		}
		return nil, errors.New("no such host")
	}
	ctx := context.Background()
	for ip, want := range map[string]bool{"192.0.2.77": true, "192.0.3.1": false, "2001:db8::1": true, "198.51.100.9": true,
		"::ffff:192.0.2.5": true, "garbage": false} {
		if got := Allowed(ctx, list, ip); got != want {
			t.Errorf("%s: %v, want %v", ip, got, want)
		}
	}
	_ = Allowed(ctx, list, "198.51.100.9")
	if calls != 1 {
		t.Errorf("host looked up %d times, want 1 (cached)", calls)
	}
	if Allowed(ctx, nil, "192.0.2.1") {
		t.Error("an empty list allows nobody")
	}
}
