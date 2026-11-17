package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/actions"
)

func testRegister() []actions.Action {
	return []actions.Action{
		{ID: "core.pages.get", Method: "GET", Path: "/api/v1/pages/{id}", UI: &actions.UI{Entry: "/p/{id}"}},
		{ID: "core.pages.history", Method: "GET", Path: "/api/v1/pages/{id}/history", UI: &actions.UI{Entry: "/p/{id}"}},
		{ID: "core.files.get", Method: "GET", Path: "/api/v1/files/{path...}", UI: &actions.UI{Entry: "/"}},
		{ID: "acme.board.move", Plugin: "acme.board", Method: "POST", Path: "cards/{id}/move", UI: &actions.UI{Entry: "x/acme.board"}},
		{ID: "core.setup.cli", Method: "POST", Path: "/api/v1/cli-only"},
	}
}

func TestCoverMapsLikeTheServer(t *testing.T) {
	covered, unknown, err := Cover(testRegister(), []Request{
		{Method: "GET", Path: "/api/v1/pages/12", Status: 200},
		{Method: "GET", Path: "/api/v1/pages/12/history", Status: 403}, // a refusal does not count
		{Method: "GET", Path: "/api/v1/files/a/b/c.png", Status: 304},
		{Method: "POST", Path: "/api/v1/x/acme.board/cards/7/move", Status: 204},
		{Method: "DELETE", Path: "/api/v1/pages/12", Status: 405}, // no action: reported
	})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"core.pages.get": true, "core.pages.history": false, "core.files.get": true, "acme.board.move": true} {
		if covered[id] != want {
			t.Errorf("%s covered = %v, want %v", id, covered[id], want)
		}
	}
	if len(unknown) != 1 || unknown[0] != "DELETE /api/v1/pages/12" {
		t.Errorf("unknown = %v", unknown)
	}
}

func TestRunChecksTheList(t *testing.T) {
	dir := t.TempDir()
	reqs := filepath.Join(dir, "r.json")
	if err := os.WriteFile(reqs, []byte(`[{"method":"GET","path":"/api/v1/categories","status":200}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	all, err := actions.All()
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	missing := filepath.Join(dir, "missing.txt")
	// without a list nearly everything is reported
	if err := run(reqs, missing, false, &sb); err == nil || !strings.Contains(err.Error(), "never used successfully") {
		t.Fatalf("missing actions not reported: %v", err)
	}
	// -write creates the list; then the check passes
	if err := run(reqs, missing, true, &sb); err != nil {
		t.Fatal(err)
	}
	if err := run(reqs, missing, false, &sb); err != nil {
		t.Fatalf("after -write: %v", err)
	}
	// an action that is covered must leave the list
	b, _ := os.ReadFile(missing)
	var used string
	for _, a := range all {
		if a.Route() == "GET /api/v1/categories" {
			used = a.ID
		}
	}
	if used == "" {
		t.Fatal("no action for GET /api/v1/categories")
	}
	if err := os.WriteFile(missing, append(b, []byte(used+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(reqs, missing, false, &sb); err == nil || !strings.Contains(err.Error(), used+" is covered now") {
		t.Fatalf("covered action in the list not reported: %v", err)
	}
	// -write never grows the list
	if err := os.WriteFile(missing, []byte("# empty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(reqs, missing, true, &sb); err == nil {
		t.Fatal("-write grew the list")
	}
	// an empty recording means the browser test did not run
	if err := os.WriteFile(reqs, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(reqs, missing, false, &sb); err == nil {
		t.Fatal("empty recording accepted")
	}
}
