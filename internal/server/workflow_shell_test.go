package server_test

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// TestWorkflowShellStep: the step "Shell command" (decision of the project
// owner of 26.11.2026). Saving the workflow asks for the approval at once;
// a case waits until a superadmin approves the exact command; values of the
// case reach the command only as environment variables, so a value that
// looks like a command stays data; exit code and output land in fields, an
// exit code other than 0 leads to the step "otherwise"; a revoked command
// fails the step; test runs never run it; the server must allow shell
// commands at all.
func TestWorkflowShellStep(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell")
	}
	e := setup(t)
	ctx := context.Background()
	admin := e.client()
	admin.login("admin", adminPW)
	var w cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Ops flows"}}, &w, 201)
	const cmd = `printf '%s' "$EWIKI_FIELD_NAME" | wc -c; [ "$EWIKI_FIELD_NAME" != fail ]`
	def := func(command string, fields []string) map[string]any {
		return map[string]any{"description": "", "fields": []map[string]any{{"id": "name", "label": "Name", "type": "text"}, {"id": "note", "label": "Note", "type": "text"}},
			"steps": []map[string]any{
				{"id": "run", "name": "Count", "type": "shell", "next": "done", "next_else": "failed",
					"shell": map[string]any{"command": command, "fields": fields, "timeout": 10, "output": "out", "exit": "code"}},
				{"id": "done", "name": "Done", "type": "end", "outcome": "success"},
				{"id": "failed", "name": "Failed", "type": "end", "outcome": "failure"}}}
	}
	// checked when saved: a command, known fields, one variable per field
	same := def(cmd, []string{"na-me", "na_me"})
	same["fields"] = []map[string]any{{"id": "na-me", "label": "A", "type": "text"}, {"id": "na_me", "label": "B", "type": "text"}}
	for name, body := range map[string]map[string]any{
		"empty command": def("  ", nil), "unknown field": def(cmd, []string{"nope"}), "same variable": same,
	} {
		if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Bad", "body": body}, nil); code != 400 {
			t.Fatalf("%s: %d", name, code)
		}
	}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Counter", "body": def(cmd, []string{"name"})}, &wf, 201)

	// saving asked for the approval: the command is listed as pending, with its step
	var list struct {
		Commands []struct {
			Hash, Command, Status, Source string
			ElementID                     *int64 `json:"element_id"`
		} `json:"commands"`
	}
	admin.must("GET", "/api/v1/x/macro.shell/commands", nil, &list, 200)
	if len(list.Commands) != 1 || list.Commands[0].Status != "pending" || list.Commands[0].Source != "workflow:run" ||
		list.Commands[0].ElementID == nil || *list.Commands[0].ElementID != wf.ID || list.Commands[0].Command != cmd {
		t.Fatalf("approval not requested on save: %+v", list.Commands)
	}
	hash := list.Commands[0].Hash

	timers := func() {
		t.Helper()
		// waiting cases look again after a minute: now
		if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_workflow_instances SET wake_at=now() WHERE status='open' AND wake_at IS NOT NULL`); err != nil {
			t.Fatal(err)
		}
		if err := e.app.Workers.RunOnce(ctx, "workflow.process.timers"); err != nil {
			t.Fatal(err)
		}
	}
	start := func(name string, test bool) wfCase {
		t.Helper()
		var c wfCase
		admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{"name": name}, "test": test}, &c, 201)
		return c
	}
	get := func(c wfCase) wfCase {
		t.Helper()
		var x wfCase
		admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", c.ID), nil, &x, 200)
		return x
	}

	// shell commands switched off on the server: the step fails
	c := start("Ann", false)
	timers()
	if x := get(c); x.Status != "open" || !strings.Contains(x.LastError, "switched off") {
		t.Fatalf("ran while shell commands are off: %+v", x)
	}
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Plugins.Macros.ShellEnabled, c.Plugins.Macros.ShellApprovers = true, []string{"admin@local"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// not approved yet: the case waits, nothing ran
	value := `Ann'; touch /tmp/ewiki-injected; echo '`
	c = start(value, false)
	timers()
	if x := get(c); x.Status != "open" || x.Step != "run" || x.Data["out"] != nil {
		t.Fatalf("ran without approval: %+v", x)
	}
	admin.must("POST", "/api/v1/x/macro.shell/commands/"+hash+"/approve", nil, nil, 204)
	timers()
	x := get(c)
	if x.Status != "done" || x.Outcome != "success" || x.Data["code"] != "0" || strings.TrimSpace(fmt.Sprint(x.Data["out"])) != fmt.Sprint(len(value)) {
		t.Fatalf("approved command: %+v", x)
	}
	if _, err := exec.Command("test", "-e", "/tmp/ewiki-injected").Output(); err == nil {
		t.Fatal("a value of the case was run as a command")
	}
	// exit code other than 0: the step "otherwise"
	c = start("fail", false)
	timers()
	if x := get(c); x.Status != "done" || x.Outcome != "failure" || x.Data["code"] != "1" {
		t.Fatalf("exit code 1: %+v", x)
	}
	// a test run never runs it
	c = start("Ann", true)
	timers()
	if x := get(c); x.Data["out"] != nil {
		t.Fatalf("a test run ran the command: %+v", x)
	}
	// revoked: the step fails
	admin.must("POST", "/api/v1/x/macro.shell/commands/"+hash+"/revoke", nil, nil, 204)
	c = start("Ann", false)
	timers()
	if x := get(c); x.Status != "open" || !strings.Contains(x.LastError, "revoked") {
		t.Fatalf("revoked command: %+v", x)
	}
	// a changed command needs a new approval, the old one stays as it was
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", wf.ID), map[string]any{"lang": "en", "title": "Counter", "body": def("echo changed", nil)}, nil, 200)
	admin.must("GET", "/api/v1/x/macro.shell/commands", nil, &list, 200)
	status := map[string]string{}
	for _, c := range list.Commands {
		status[c.Command] = c.Status
	}
	if status["echo changed"] != "pending" || status[cmd] != "revoked" {
		t.Fatalf("after a change: %+v", status)
	}
}
