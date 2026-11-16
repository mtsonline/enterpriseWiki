package server_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/app"
)

// TestTwoAppsOfOneSite: a second app of the same site in the process (a
// command line inside a server process, tests) has its own plugin
// instances, and closing it leaves the first app working. Before
// 15.11.2026 the second app took over the plugin instances of the site
// and the first one failed with "closed pool" after it closed.
func TestTwoAppsOfOneSite(t *testing.T) {
	e := setup(t)
	second, err := app.New(context.Background(), app.Options{ConfigPath: e.cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	// plugins with state
	for _, id := range []string{"content.forms", "content.events", "core.properties", "macro.shell"} {
		a, _ := e.app.Kernel.Get(id)
		b, _ := second.Kernel.Get(id)
		if a == nil || a == b {
			t.Fatalf("%s: the apps share a plugin instance", id)
		}
	}
	if e.app.Scope == second.Scope {
		t.Fatal("the apps share a scope")
	}
	second.Close()

	admin := e.client()
	admin.login("admin", adminPW)
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Still here"}}, &c, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Page", "body": htmlBody("<p>x</p>")}, nil, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.events", "title": "Calendar", "body": map[string]any{}}, nil, 201)
	// the check of the workflow type used the host of the app set up last
	// (16.11.2026: refused with 403 after the second app closed)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "workflow.process", "title": "Request",
		"body": map[string]any{"fields": []map[string]any{{"id": "subject", "label": "Subject", "type": "text"}},
			"steps": []map[string]any{{"id": "done", "name": "Done", "actions": []any{}, "outcome": "success"}}}}, nil, 201)
}
