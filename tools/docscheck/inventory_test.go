package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInventoryFindsInventedIdentifiers: an identifier the documentation
// names must exist; one that sounds right but does not exist is reported
// with file and line (counter-check of the inventory check).
func TestInventoryFindsInventedIdentifiers(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/x/x.go", "package x\n\nconst a = \"core.editor.tags\"\nconst b = \"core.i18n.translate.\"\n")
	write("internal/x/actions.yaml", "- id: core.placements.set\n")
	write("docs/en/page.md", strings.Join([]string{
		"The block `core.editor.tags` exists, so does `core.placements.set`.",
		"`core.i18n.translate.pt_br` belongs to a family, `smtp.host` is a relative key of `mail.smtp.host`.",
		"Files (`config.yaml`), hosts (`wiki.example.com`) and versions (`v1.2.3`) are no identifiers.",
		"But `core.editor.buttons` was invented.",
	}, "\n"))
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := repo
	repo = r
	defer func() { repo = old; _ = r.Close() }()

	var problems []string
	n := checkInventory([]string{"docs/en/page.md"}, func(f string, a ...any) { problems = append(problems, f) })
	if len(problems) != 1 {
		t.Fatalf("problems: %v", problems)
	}
	if n != 6 {
		t.Fatalf("checked %d identifiers, want 6", n)
	}
	// the message names the invented identifier and its line
	msg := ""
	checkInventory([]string{"docs/en/page.md"}, func(f string, a ...any) { msg = fmt.Sprintf(f, a...) })
	if !strings.Contains(msg, "docs/en/page.md:4") || !strings.Contains(msg, "core.editor.buttons") {
		t.Fatalf("message: %s", msg)
	}
}
