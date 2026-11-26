package main

// Coverage check of the developer guide (CLAUDE.md, section 7): the guide
// in both languages links every Go package below internal/ and every
// plugin below plugins/, so nobody adds an area that new developers cannot
// find. That the linked files exist is checked with all links (checkLinks).

import (
	"io/fs"
	"path"
	"strings"
)

// guidePages are the two language versions of the developer guide.
var guidePages = []string{"docs/de/developer/code-guide.md", "docs/en/developer/code-guide.md"}

// guideRoots are the folders whose subfolders the guide must link.
var guideRoots = []string{"internal", "plugins"}

// checkCodeGuide reports every package or plugin folder a guide page does
// not link (a link to the folder or to a file in it counts).
func checkCodeGuide(add func(string, ...any)) {
	var areas []string
	for _, root := range guideRoots {
		entries, err := fs.ReadDir(repo.FS(), root)
		if err != nil {
			add("code guide: cannot list %s: %v", root, err)
			return
		}
		for _, e := range entries {
			if e.IsDir() && hasGo(path.Join(root, e.Name())) {
				areas = append(areas, path.Join(root, e.Name()))
			}
		}
	}
	for _, page := range guidePages {
		b, err := repo.ReadFile(page)
		if err != nil {
			add("%s: missing (the developer guide, CLAUDE.md section 7)", page)
			continue
		}
		linked := linkedPaths(page, string(b))
		for _, area := range areas {
			if !linked[area] {
				add("%s: does not link %s/ (every package and plugin belongs in the developer guide)", page, area)
			}
		}
	}
}

// hasGo reports whether a folder holds Go code of its own (not only data).
func hasGo(dir string) bool {
	entries, err := fs.ReadDir(repo.FS(), dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

// linkedPaths returns the folders a page links, as paths from the root of
// the repository: for a link to a file, every folder above it.
func linkedPaths(page, text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range linkRe.FindAllStringSubmatch(text, -1) {
		target := m[1]
		if strings.Contains(target, "://") {
			continue
		}
		target, _, _ = strings.Cut(target, "#")
		p := path.Clean(path.Join(path.Dir(page), target))
		for p != "." && p != "/" && !strings.HasPrefix(p, "..") {
			out[p] = true
			p = path.Dir(p)
		}
	}
	return out
}
