package cli

import (
	"regexp"
	"strings"
)

var wordTokenRe = regexp.MustCompile(`^[a-z][a-z0-9-]*(\|[a-z][a-z0-9-]*)*$`)

// BuiltinCommands lists the hand-written commands as word sequences, read
// from the usage text (e.g. ["user", "create"], ["plugin", "sources",
// "add"]). The action register's generated commands must never start with
// one of them, so they cannot hide a hand-written command.
func BuiltinCommands() [][]string {
	var out [][]string
	for _, line := range strings.Split(usage, "\n") {
		if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			continue
		}
		top := ""
		var parent []string
		for _, seg := range strings.Split(strings.TrimSpace(line), " | ") {
			// the description after the command: two or more spaces
			if i := strings.Index(seg, "  "); i >= 0 {
				seg = seg[:i]
			}
			var paths [][]string
			for _, tok := range strings.Fields(seg) {
				if !wordTokenRe.MatchString(tok) {
					break
				}
				alts := strings.Split(tok, "|")
				if paths == nil {
					for _, a := range alts {
						paths = append(paths, []string{a})
					}
					continue
				}
				var next [][]string
				for _, p := range paths {
					for _, a := range alts {
						next = append(next, append(append([]string{}, p...), a))
					}
				}
				paths = next
			}
			if len(paths) == 0 {
				continue
			}
			// "site list | create NAME": the second part belongs to the parent
			// of the first ("site"; "plugin sources" for "plugin sources list |
			// add PREFIX")
			if top != "" && paths[0][0] != top {
				for i := range paths {
					paths[i] = append(append([]string{}, parent...), paths[i]...)
				}
			}
			top = paths[0][0]
			parent = paths[0][:len(paths[0])-1]
			if len(parent) == 0 {
				parent = []string{top}
			}
			out = append(out, paths...)
		}
	}
	return out
}
