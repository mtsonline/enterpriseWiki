package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// services of the app that commands must not call directly: everything
// goes through the API (apiClient, e.call), so the command line has the
// same permission checks, input checks, limits and audit entries
// (CLAUDE.md 1, phase 3 of the action register plan).
var services = []string{"Auth", "Content", "Mail", "Kernel", "DB", "Notify", "I18n", "Media", "Watch", "ACL",
	"Workers", "Signatures", "Cache", "Files", "Webhooks", "Search", "Instance", "Feeds"}

// directAllowed are the functions that may use the services directly,
// with the reason. Approved by the project owner on 15.11.2026: serve,
// setup and migrate (they run or prepare the wiki), and the commands that
// work on files instead of the wiki (config, plugin generate/build,
// signature verify --offline – these do not touch the services at all).
var directAllowed = map[string]string{
	"cli.go:serve":         "runs the server with its background jobs",
	"cli.go:jobs":          "maintenance jobs of the running server",
	"cli.go:watchConfig":   "reloads the configuration of the running server",
	"cli.go:config":        "approved exception: config set records itself in the audit log",
	"setup.go:setup":       "installation: creates the first roles and administrator before there is one",
	"commands.go:callType": "resolves --as to the principal the API request then runs as",
	"inproc.go:lookupUser": "turns a typed name into the account id the API expects (read only)",
}

func TestCommandsUseTheAPI(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	used := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f) // #nosec G304 -- files of this package
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			key := f + ":" + fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				// a.<Service> where a is the app
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "a" && slices.Contains(services, sel.Sel.Name) {
					if _, allowed := directAllowed[key]; !allowed {
						t.Errorf("%s: %s uses a.%s directly: call the API instead (apiClient)", fset.Position(sel.Pos()), fn.Name.Name, sel.Sel.Name)
					}
					used[key] = true
				}
				return true
			})
		}
	}
	for k := range directAllowed {
		if !used[k] {
			t.Errorf("%s no longer uses the services directly: remove it from directAllowed", k)
		}
	}
}
