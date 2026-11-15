package collab

import (
	"errors"
	"testing"
)

// S-06: one person cannot fill a document with connections; persons are
// counted, not connections.
func TestJoinLimits(t *testing.T) {
	h := NewHub()
	n, perPerson := 4, 2 // the limits of this hub (expert settings of its app)
	h.Limits = func() (int, int) { return n, perPerson }
	persons := int64(n)
	for i := range perPerson {
		if _, err := h.join("doc", User{ID: 1}); err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
	}
	if _, err := h.join("doc", User{ID: 1}); !errors.Is(err, ErrTooManyConnections) {
		t.Fatalf("connection %d of one person: %v", perPerson+1, err)
	}
	for id := int64(2); id <= persons; id++ {
		if _, err := h.join("doc", User{ID: id}); err != nil {
			t.Fatalf("person %d: %v", id, err)
		}
	}
	if _, err := h.join("doc", User{ID: persons + 1}); !errors.Is(err, ErrFull) {
		t.Fatalf("person %d: %v", persons+1, err)
	}
	// a further tab of someone already there is fine
	if _, err := h.join("doc", User{ID: 2}); err != nil {
		t.Fatalf("second tab: %v", err)
	}
}
