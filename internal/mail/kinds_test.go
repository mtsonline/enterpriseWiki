package mail

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

// TestKindsConcurrent: plugins register kinds while a site is set up and
// other sites read them (16.11.2026: a data race on plain maps).
func TestKindsConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			RegisterKind(fmt.Sprintf("test.kind%d", i), "label", i%2 == 0)
		}()
		go func() {
			defer wg.Done()
			_ = NotificationKinds()
			_ = KindLabels()
			_ = DefaultOn("test.kind0")
		}()
	}
	wg.Wait()
	if !slices.Contains(NotificationKinds(), "test.kind3") || DefaultOn("test.kind3") || !DefaultOn("test.kind2") {
		t.Fatalf("kinds %v", NotificationKinds())
	}
}
