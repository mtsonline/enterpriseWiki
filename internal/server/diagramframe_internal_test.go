package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// TestDiagramFrame: the frame for diagrams from text has its own strict
// policy: its own script only, no requests, no forms, only inside the wiki.
func TestDiagramFrame(t *testing.T) {
	s := &Server{web: fstest.MapFS{"diagram-frame.html": {Data: []byte(`<!doctype html><script src="/assets/diagram/frame-0123456789.js"></script>`)}}}
	for _, p := range []string{"/diagram-frame", "/diagram-frame.html"} {
		rec := httptest.NewRecorder()
		s.serveFrontend(rec, httptest.NewRequest(http.MethodGet, p, nil))
		csp := rec.Header().Get("Content-Security-Policy")
		for _, want := range []string{"default-src 'none'", "script-src 'self'", "connect-src 'none'", "frame-ancestors 'self'", "form-action 'none'", "base-uri 'none'"} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: policy without %q: %s", p, want, csp)
			}
		}
		if strings.Contains(csp, "unsafe-eval") || strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
			t.Errorf("%s: policy too wide: %s", p, csp)
		}
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "/assets/diagram/frame-") || rec.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body.String())
		}
	}
}
