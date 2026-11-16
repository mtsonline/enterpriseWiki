package httpx

import (
	"io"
	"log/slog"
	"sync"
	"testing"
)

// TestSetLoggerConcurrent: opening an instance sets the logger while
// requests of another app log errors (16.11.2026: a data race).
func TestSetLoggerConcurrent(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() { defer wg.Done(); SetLogger(l) }()
		go func() { defer wg.Done(); logErr().Debug("x") }()
	}
	wg.Wait()
	if logErr() != l {
		t.Fatal("logger not set")
	}
}
