package main

import "testing"

// TestTrapIDs: every trap has its own id (runs without a database, so a
// repeated id fails the ordinary test run, not only the trap run).
func TestTrapIDs(t *testing.T) {
	if err := checkIDs(traps); err != nil {
		t.Fatal(err)
	}
	if checkIDs([]trap{{ID: "a"}, {ID: "a"}}) == nil {
		t.Fatal("a repeated id was accepted")
	}
	if checkIDs([]trap{{ID: ""}}) == nil {
		t.Fatal("an empty id was accepted")
	}
}
