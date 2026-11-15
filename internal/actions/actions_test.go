package actions

import (
	"strings"
	"testing"
)

func valid() Action {
	return Action{
		ID: "core.things.make", Category: "CATEGORY_ACTION", Method: "POST", Path: "/api/v1/things/{id}",
		Summary:    Text{Text: map[string]string{"en": "Make a thing", "de": "Ein Ding machen"}},
		Permission: &Permission{Key: "core.things.manage", Scope: "SCOPE_GLOBAL"},
		CLI:        &CLI{Command: []string{"thing", "make"}, Args: []string{"id"}},
		UI:         &UI{Entry: "/things"},
		Source:     "test.yaml",
	}
}

// TestValidate: every rule of the register refuses what it should, and a
// correct entry passes.
func TestValidate(t *testing.T) {
	if err := Validate([]Action{valid()}); err != nil {
		t.Fatalf("valid entry refused: %v", err)
	}
	cases := map[string]struct {
		change func(*Action)
		want   string
	}{
		"id":                {func(a *Action) { a.ID = "Core.Things" }, "id:"},
		"core namespace":    {func(a *Action) { a.ID = "things.make" }, `start with "core."`},
		"plugin namespace":  {func(a *Action) { a.Plugin = "acme.kanban"; a.Path = "things/{id}" }, `"acme.kanban."`},
		"plugin path":       {func(a *Action) { a.ID = "acme.kanban.x"; a.Plugin = "acme.kanban"; a.Path = "/api/v1/x" }, "relative"},
		"plugin dots":       {func(a *Action) { a.ID = "acme.kanban.x"; a.Plugin = "acme.kanban"; a.Path = "../admin" }, "relative"},
		"core path":         {func(a *Action) { a.Path = "/other" }, "below /api/v1/"},
		"method":            {func(a *Action) { a.Method = "TRACE" }, "method"},
		"german":            {func(a *Action) { delete(a.Summary.Text, "de") }, "summary.text.de"},
		"english":           {func(a *Action) { a.Summary.Text["en"] = " " }, "summary.text.en"},
		"comma in flow map": {func(a *Action) { a.Summary.Text["redirect"] = "" }, "keys are languages"},
		"status":            {func(a *Action) { a.SuccessStatus = 302 }, "success_status"},
		"no access":         {func(a *Action) { a.Permission = nil }, "exactly one"},
		"two accesses":      {func(a *Action) { a.Public = true }, "exactly one"},
		"scope":             {func(a *Action) { a.Permission.Scope = "SCOPE_SITE" }, "valid scope"},
		"category param":    {func(a *Action) { a.Permission.Scope = "SCOPE_CATEGORY" }, "category_param"},
		"category right": {func(a *Action) {
			*a.Permission = Permission{Key: "core.things.make", Scope: "SCOPE_CATEGORY", CategoryParam: "id"}
		}, "no category right"},
		"foreign plugin right": {func(a *Action) {
			a.ID, a.Plugin, a.Path = "acme.kanban.x", "acme.kanban", "boards/{id}"
			*a.Permission = Permission{Key: "other.plugin.edit", Scope: "SCOPE_CATEGORY", CategoryParam: "element:id"}
		}, "no category right"},
		"element param": {func(a *Action) {
			*a.Permission = Permission{Key: "core.category.view", Scope: "SCOPE_CATEGORY", CategoryParam: "element:eid"}
		}, "named in the path"},
		"param syntax": {func(a *Action) {
			*a.Permission = Permission{Key: "core.category.view", Scope: "SCOPE_CATEGORY", CategoryParam: "{id}"}
		}, "category_param"},
		"no cli":        {func(a *Action) { a.CLI = nil }, "no CLI command"},
		"no ui":         {func(a *Action) { a.UI = nil }, "no entry in the interface"},
		"bad exception": {func(a *Action) { a.CLI = nil; a.Exceptions = []Exception{{Way: "WAY_CLI", Reason: "later"}} }, "who approved"},
		"cli word":      {func(a *Action) { a.CLI.Command = []string{"Thing"} }, "word"},
		"cli arg":       {func(a *Action) { a.CLI.Args = []string{"name"} }, "no path, query or body parameter"},
		"category":      {func(a *Action) { a.Category = "CATEGORY_OTHER" }, "category"},
		"protocol ways": {func(a *Action) { a.Category = "CATEGORY_PROTOCOL" }, "no CLI or interface way"},
		"example name":  {func(a *Action) { a.Examples = []Example{{Status: 200}} }, "need a name"},
	}
	for name, c := range cases {
		a := valid()
		a.Summary.Text = map[string]string{"en": "Make a thing", "de": "Ein Ding machen"}
		c.change(&a)
		err := Validate([]Action{a})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", name, err, c.want)
		}
	}
	// approved exception instead of a way
	a := valid()
	a.CLI = nil
	a.Exceptions = []Exception{{Way: "WAY_CLI", Reason: "needs the server's files", ApprovedBy: "project owner, 2026-11-15"}}
	if err := Validate([]Action{a}); err != nil {
		t.Errorf("approved exception refused: %v", err)
	}
	// duplicates and command conflicts across entries
	b := valid()
	if err := Validate([]Action{valid(), b}); err == nil || !strings.Contains(err.Error(), "id used twice") || !strings.Contains(err.Error(), "already belongs") {
		t.Errorf("duplicates: %v", err)
	}
	b.ID, b.Path, b.CLI = "core.things.list", "/api/v1/things", &CLI{Command: []string{"thing"}}
	if err := Validate([]Action{valid(), b}); err == nil || !strings.Contains(err.Error(), "is the start of") {
		t.Errorf("prefix: %v", err)
	}
	// a hand-written command may serve two actions
	b.CLI = &CLI{Command: []string{"thing", "make"}, Builtin: true}
	c := valid()
	c.ID, c.Path, c.Method = "core.things.show", "/api/v1/things/{id}/x", "GET"
	c.CLI = &CLI{Command: []string{"thing", "make"}, Builtin: true}
	b.CLI.Builtin, c.CLI.Builtin = true, true
	x := valid()
	x.CLI.Builtin = true
	if err := Validate([]Action{x, c}); err != nil {
		t.Errorf("builtin command for two actions refused: %v", err)
	}
}

// TestRegisterLoads: the embedded files of the core are valid.
func TestRegisterLoads(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("empty register")
	}
}

// TestByCommand: the longest command wins.
func TestByCommand(t *testing.T) {
	a := valid()
	b := valid()
	b.ID, b.CLI = "core.things.make.fast", &CLI{Command: []string{"thing", "make", "fast"}}
	got, n, ok := ByCommand([]Action{a, b}, []string{"thing", "make", "fast", "7"})
	if !ok || got.ID != b.ID || n != 3 {
		t.Fatalf("got %s %d %v", got.ID, n, ok)
	}
	got, n, ok = ByCommand([]Action{a, b}, []string{"thing", "make", "7"})
	if !ok || got.ID != a.ID || n != 2 {
		t.Fatalf("got %s %d %v", got.ID, n, ok)
	}
	if _, _, ok := ByCommand([]Action{a}, []string{"thing"}); ok {
		t.Fatal("matched a shorter input")
	}
}

// TestUnknownField: register files are read strictly.
func TestUnknownField(t *testing.T) {
	if _, err := parse([]byte("actions:\n  - id: core.x\n    summmary: {}\n"), "x.yaml", ""); err == nil {
		t.Fatal("unknown field accepted")
	}
}
