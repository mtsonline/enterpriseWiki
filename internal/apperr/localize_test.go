package apperr

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// messagesOfCode collects the messages written in the code: the formats of
// apperr.BadRequest, Forbidden …, the subjects of NotFound ("… not found"),
// the messages of New and of &apperr.Error{Msg: …}, and those of this
// package itself.
func messagesOfCode(t *testing.T) map[string]string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	add := func(s, where string) {
		if _, ok := out[s]; !ok {
			out[s] = where
		}
	}
	str := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(lit.Value)
		return s, err == nil
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "testdata", "dist", "sdk":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		inApperr := f.Name.Name == "apperr"
		ast.Inspect(f, func(n ast.Node) bool {
			where := func() string { return fmt.Sprintf("%s:%d", rel, fset.Position(n.Pos()).Line) }
			switch x := n.(type) {
			case *ast.CallExpr:
				var name string
				switch fn := x.Fun.(type) {
				case *ast.SelectorExpr:
					if id, ok := fn.X.(*ast.Ident); ok && id.Name == "apperr" {
						name = fn.Sel.Name
					}
				case *ast.Ident:
					if inApperr {
						name = fn.Name
					}
				}
				switch name {
				case "BadRequest", "Forbidden", "Conflict", "Busy", "TooLarge", "TooMany":
					if len(x.Args) > 0 {
						if s, ok := str(x.Args[0]); ok {
							add(s, where())
						}
					}
				case "NotFound":
					if len(x.Args) > 0 {
						if s, ok := str(x.Args[0]); ok {
							add(strings.ReplaceAll(s+" not found", "%", "%%"), where())
						}
					}
				case "New":
					if len(x.Args) > 2 {
						if s, ok := str(x.Args[2]); ok {
							add(strings.ReplaceAll(s, "%", "%%"), where())
						}
					}
				}
			case *ast.CompositeLit:
				// &apperr.Error{…, Msg: "…"} (and Error{…} in this package)
				isErr := false
				if sel, ok := x.Type.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "apperr" && sel.Sel.Name == "Error" {
						isErr = true
					}
				} else if id, ok := x.Type.(*ast.Ident); ok && inApperr && id.Name == "Error" {
					isErr = true
				}
				if isErr {
					for _, el := range x.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Msg" {
								if s, ok := str(kv.Value); ok {
									add(strings.ReplaceAll(s, "%", "%%"), where())
								}
							}
						}
					}
				}
			case *ast.ReturnStmt:
				// the messages of Status for errors of other packages
				if inApperr && len(x.Results) == 3 {
					if s, ok := str(x.Results[2]); ok {
						add(strings.ReplaceAll(s, "%", "%%"), where())
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestCatalogsComplete: every message of the code has a text in every
// language, and no text is left over from a message that is gone.
func TestCatalogsComplete(t *testing.T) {
	code := messagesOfCode(t)
	if len(code) < 900 {
		t.Fatalf("only %d messages found: does the scan still work?", len(code))
	}
	if len(Languages()) == 0 {
		t.Fatal("no catalogs")
	}
	for _, lang := range Languages() {
		cat := Catalog(lang)
		var missing, stale []string
		for m, where := range code {
			if _, ok := cat[m]; !ok {
				missing = append(missing, fmt.Sprintf("%q (%s)", m, where))
			}
		}
		for m := range cat {
			if _, ok := code[m]; !ok {
				stale = append(stale, strconv.Quote(m))
			}
		}
		sort.Strings(missing)
		sort.Strings(stale)
		if len(missing) > 0 {
			t.Errorf("%s: %d messages without a text in locales/%s.json:\n%s", lang, len(missing), lang, strings.Join(missing, "\n"))
		}
		if len(stale) > 0 {
			t.Errorf("%s: %d texts for messages that are no longer in the code:\n%s", lang, len(stale), strings.Join(stale, "\n"))
		}
	}
}

var verbRe = regexp.MustCompile(`%(\[\d+\])?[-+# 0]*\d*(\.\d+)?(\[\d+\])?[a-zA-Z%]`)

// sampleArgs are values that fit the verbs of an English format.
func sampleArgs(format string) []any {
	var args []any
	for _, v := range verbRe.FindAllString(format, -1) {
		switch v[len(v)-1] {
		case '%':
		case 'd':
			args = append(args, len(args)+1)
		case 'f':
			args = append(args, 1.5+float64(len(args)))
		default:
			args = append(args, fmt.Sprintf("x%d", len(args)+1))
		}
	}
	return args
}

// TestCatalogsFormats: every text takes the values of its message, each one
// exactly once, and is not empty.
func TestCatalogsFormats(t *testing.T) {
	for _, lang := range Languages() {
		for en, tr := range Catalog(lang) {
			if strings.TrimSpace(tr) == "" {
				t.Errorf("%s: empty text for %q", lang, en)
				continue
			}
			args := sampleArgs(en)
			got := fmt.Sprintf(tr, args...)
			if strings.Contains(got, "%!") {
				t.Errorf("%s: %q does not fit the values of %q: %s", lang, tr, en, got)
				continue
			}
			for _, a := range args {
				if !strings.Contains(got, fmt.Sprint(a)) && !containsNumber(got, a) {
					t.Errorf("%s: %q leaves out the value %v of %q", lang, tr, a, en)
				}
			}
		}
	}
}

func containsNumber(s string, a any) bool {
	f, ok := a.(float64)
	if !ok {
		return false
	}
	return strings.Contains(s, fmt.Sprintf("%.0f", f)) || strings.Contains(s, fmt.Sprintf("%.1f", f)) || strings.Contains(s, fmt.Sprintf("%.2f", f))
}

// TestLocalized: messages in German, values kept, unknown messages and
// languages in English, errors wrapped and errors of other packages too.
func TestLocalized(t *testing.T) {
	cases := []struct {
		err  error
		lang string
		want string
	}{
		{NotFound("page"), "de", "Seite nicht gefunden"},
		{BadRequest("invalid language %q", "xx"), "de", `Ungültige Sprache "xx"`},
		{fmt.Errorf("saving: %w", Forbidden("writing here needs the permission %q of this %s", "core.edit", "category")), "de", `Schreiben braucht hier das Recht "core.edit" dieser category`},
		{ErrForbidden, "de", "permission denied"},
		{BadRequest("a message nobody wrote %d", 3), "de", "a message nobody wrote 3"},
		{NotFound("page"), "en", "page not found"},
		{NotFound("page"), "xx", "page not found"},
		{errors.New("boom"), "de", "internal server error"},
	}
	// the sentinel has a German text if the catalog has one
	if tr, ok := Catalog("de")["permission denied"]; ok {
		cases[3].want = tr
	}
	if tr, ok := Catalog("de")["internal server error"]; ok {
		cases[7].want = tr
	}
	for _, c := range cases {
		if got := Localized(c.err, c.lang); got != c.want {
			t.Errorf("%v (%s): %q, want %q", c.err, c.lang, got, c.want)
		}
	}
}

func TestRequestLanguage(t *testing.T) {
	for al, want := range map[string]string{"de-AT,de;q=0.9,en;q=0.5": "de", "DE": "de", "en-GB": "en", "": "en", "fr": "en", "de;q=0.8": "de"} {
		r, _ := httpRequest(al)
		if got := RequestLanguage(r); got != want {
			t.Errorf("%q: %s, want %s", al, got, want)
		}
	}
}

func httpRequest(acceptLanguage string) (*http.Request, error) {
	r, err := http.NewRequest(http.MethodGet, "/", nil)
	if err == nil && acceptLanguage != "" {
		r.Header.Set("Accept-Language", acceptLanguage)
	}
	return r, err
}
