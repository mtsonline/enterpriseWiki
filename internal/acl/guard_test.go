package acl_test

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestElementQueriesChecked makes sure that every query reading elements
// (FROM/JOIN elements, the search documents) states how the element's own
// rights are respected: a comment "acl:" on the line or in the lines just
// before it, e.g. "// acl: filtered (ElementFilter)", "// acl: checked by
// requireElement", or "// acl: internal – <reason>". Element rights are
// checked per element (acl.Service.ElementPerms); a query that only checks
// the category would show elements with "own rights only".
func TestElementQueriesChecked(t *testing.T) {
	re := regexp.MustCompile(`\b(FROM|JOIN)\s+(elements|search_docs)\b`)
	var missing []string
	for _, dir := range []string{"../../internal", "../../plugins"} {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := os.Open(path) // #nosec G304 -- source files of the repository (test)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			var lines []string
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 1<<20)
			for sc.Scan() {
				lines = append(lines, sc.Text())
			}
			for i, l := range lines {
				if !re.MatchString(l) {
					continue
				}
				ok := false
				for j := max(0, i-8); j <= i; j++ {
					if strings.Contains(lines[j], "acl:") {
						ok = true
						break
					}
				}
				if !ok {
					missing = append(missing, filepath.ToSlash(strings.TrimPrefix(path, "../../"))+":"+itoa(i+1))
				}
			}
			return sc.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(missing) > 0 {
		t.Errorf("queries on elements without a note how element rights are respected (comment \"acl: …\" before the query):\n%s", strings.Join(missing, "\n"))
	}
}

func itoa(n int) string {
	var b [20]byte
	i := len(b)
	for n > 0 || i == len(b) {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
