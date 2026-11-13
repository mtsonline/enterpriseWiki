// Command docscheck checks that the documentation exists in German and
// English with the same structure, and that its links work:
//
//   - every page under docs/de has a page under docs/en with the same path,
//     and the other way round; the same holds for the files in the root of
//     the repository (X.md in English, X.de.md in German; CLAUDE.md in
//     German, CLAUDE.en.md in English)
//   - both pages have the same headings (levels in the same order), the
//     same number of code blocks and table rows
//   - relative links point to existing files and, with #anchor, to an
//     existing heading of that file
//
// Run from the root of the repository: go run ./tools/docscheck
package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// pair is a German page and its English counterpart.
type pair struct{ de, en string }

// shape is what both languages must have in common.
type shape struct {
	headings []int // levels in order
	blocks   int   // fenced code blocks
	rows     int   // table rows (lines starting with |)
}

var (
	linkRe    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	// repo confines every file access to the repository: a link that leads
	// outside cannot be opened and counts as broken.
	repo *os.Root
)

func main() {
	r, err := os.OpenRoot(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "docscheck:", err)
		os.Exit(1)
	}
	repo = r
	defer func() { _ = repo.Close() }()
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }

	pairs := findPairs(add)
	var files []string
	for _, p := range pairs {
		ds, err := read(p.de)
		if err != nil {
			add("%s: %v", p.de, err)
			continue
		}
		es, err := read(p.en)
		if err != nil {
			add("%s: %v", p.en, err)
			continue
		}
		if !equalInts(ds.headings, es.headings) {
			add("%s and %s: different headings (de %v, en %v)", p.de, p.en, ds.headings, es.headings)
		}
		if ds.blocks != es.blocks {
			add("%s and %s: different number of code blocks (de %d, en %d)", p.de, p.en, ds.blocks, es.blocks)
		}
		if ds.rows != es.rows {
			add("%s and %s: different number of table rows (de %d, en %d)", p.de, p.en, ds.rows, es.rows)
		}
		files = append(files, p.de, p.en)
	}
	for _, f := range files {
		checkLinks(f, add)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		fmt.Fprintf(os.Stderr, "docscheck: %d problem(s)\n", len(problems))
		os.Exit(1)
	}
	fmt.Printf("docscheck: %d pages in German and English, structure and links ok\n", len(pairs))
}

// findPairs lists the pages that must exist in both languages.
func findPairs(add func(string, ...any)) []pair {
	var out []pair
	seen := map[string]bool{}
	walk := func(root, other string, de bool) {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			q := filepath.Join(other, rel)
			if _, err := repo.Stat(q); err != nil {
				add("%s: missing translation %s", p, q)
				return nil
			}
			pr := pair{de: p, en: q}
			if !de {
				pr = pair{de: q, en: p}
			}
			if !seen[pr.de] {
				seen[pr.de] = true
				out = append(out, pr)
			}
			return nil
		})
	}
	walk(filepath.Join("docs", "de"), filepath.Join("docs", "en"), true)
	walk(filepath.Join("docs", "en"), filepath.Join("docs", "de"), false)
	// files in the root and in tests: X.md (English) and X.de.md (German)
	roots, _ := filepath.Glob("*.md")
	more, _ := filepath.Glob(filepath.Join("tests", "*", "*.md"))
	roots = append(roots, more...)
	for _, f := range roots {
		switch {
		case f == "CLAUDE.md":
			out = append(out, checkPair(f, "CLAUDE.en.md", add)...)
		case f == "CLAUDE.en.md" || f == "AGENTS.md":
			// CLAUDE.en.md is paired above; AGENTS.md is bilingual itself
		case strings.HasSuffix(f, ".de.md"):
			en := strings.TrimSuffix(f, ".de.md") + ".md"
			if _, err := repo.Stat(en); err != nil {
				add("%s: missing English version %s", f, en)
			}
		default:
			out = append(out, checkPair(strings.TrimSuffix(f, ".md")+".de.md", f, add)...)
		}
	}
	return out
}

func checkPair(de, en string, add func(string, ...any)) []pair {
	for _, f := range []string{de, en} {
		if _, err := repo.Stat(f); err != nil {
			add("missing %s (every page exists in German and English)", f)
			return nil
		}
	}
	return []pair{{de: de, en: en}}
}

func read(path string) (shape, error) {
	var s shape
	f, err := repo.Open(path)
	if err != nil {
		return s, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	inBlock := false
	for sc.Scan() {
		line := sc.Text()
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			if !inBlock {
				s.blocks++
			}
			inBlock = !inBlock
			continue
		}
		if inBlock {
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			s.headings = append(s.headings, len(m[1]))
		}
		if strings.HasPrefix(t, "|") {
			s.rows++
		}
	}
	return s, sc.Err()
}

// anchors returns the GitHub anchors of the headings of a file.
func anchors(path string) (map[string]bool, error) {
	f, err := repo.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]bool{}
	count := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	inBlock := false
	for sc.Scan() {
		line := sc.Text()
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inBlock = !inBlock
			continue
		}
		if inBlock {
			continue
		}
		m := headingRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		a := slug(m[2])
		if n := count[a]; n > 0 {
			out[a+"-"+strconv.Itoa(n)] = true
		} else {
			out[a] = true
		}
		count[a]++
	}
	return out, sc.Err()
}

// slug builds the anchor GitHub gives a heading: lower case, letters,
// digits, spaces (as -), - and _ kept, everything else removed.
func slug(h string) string {
	h = regexp.MustCompile("`([^`]*)`").ReplaceAllString(h, "$1")
	h = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`).ReplaceAllString(h, "$1")
	var b strings.Builder
	for _, r := range strings.ToLower(h) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

func checkLinks(file string, add func(string, ...any)) {
	b, err := repo.ReadFile(file)
	if err != nil {
		return
	}
	text := regexp.MustCompile("(?s)```.*?```").ReplaceAllString(string(b), "")
	text = regexp.MustCompile("`[^`\n]*`").ReplaceAllString(text, "")
	for _, m := range linkRe.FindAllStringSubmatch(text, -1) {
		target := m[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		path, anchor, _ := strings.Cut(target, "#")
		dest := file
		if path != "" {
			dest = filepath.Join(filepath.Dir(file), path)
			st, err := repo.Stat(dest)
			if err != nil {
				add("%s: broken link %s", file, target)
				continue
			}
			if st.IsDir() {
				continue
			}
		}
		if anchor == "" || !strings.HasSuffix(dest, ".md") {
			continue
		}
		as, err := anchors(dest)
		if err == nil && !as[anchor] {
			add("%s: link %s: no heading #%s in %s", file, target, anchor, dest)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
