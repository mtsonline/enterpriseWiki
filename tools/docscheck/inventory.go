package main

// Inventory check: every identifier the documentation names in backticks
// (`core.editor.tags`, `server.trusted_proxies`, `content.blog` …) must exist.
// An identifier exists if the source has it as a string literal (Go,
// TypeScript, JSON, YAML, Protobuf: action ids, permissions, blocks, limits,
// audit and event names, locale keys, plugin ids), if it is a key of the
// configuration (config.yaml, read from the yaml tags of config.Config) or
// of the action register, or the end of such a key (tables name keys
// relative to their section: `smtp.host` for `mail.smtp.host`). A family of
// identifiers built at run time exists if its prefix is a literal ending in
// a dot ("core.i18n.translate." for `core.i18n.translate.pt_br`).
// Documentation that sounds right but names something that does not exist
// fails here.

import (
	"bufio"
	"io/fs"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/actions"
	"github.com/mtsonline/enterprisewiki/internal/config"
)

var (
	// idRe is a namespaced identifier in backticks: two or more dotted parts,
	// lower case, optionally an instance (`core.menu/3`).
	docIDRe = regexp.MustCompile("`([a-z][a-z0-9_-]*(?:\\.[a-z0-9_-]+)+)(?:/[a-z0-9_.-]+)?`")
	// litRe is the same identifier as a string literal in the source.
	litRe = regexp.MustCompile(`["'` + "`" + `]([a-z][a-z0-9_-]*(?:\.[a-z0-9_-]+)+)(?:/[a-z0-9_.${}-]*)?["'` + "`" + `]`)
	// yamlIDRe is an unquoted id in YAML (the action register: `- id: core.x`).
	yamlIDRe = regexp.MustCompile(`^\s*(?:-\s*)?[a-z_]+:\s*([a-z][a-z0-9_-]*(?:\.[a-z0-9_-]+)+)\s*$`)
	// fileRe: names of files are not identifiers.
	fileRe = regexp.MustCompile(`\.(ya?ml|json|ts|go|md|css|mjs|js|sql|html|txt|pem|proto|sh|env|key|crt|toml|xml|png|svg|woff2|tgz|zip|csv|xlsx|docx|pdf|ics|exe|service|conf|log|mod|sum|db|sock)$`)
	// hostRe: host names (example domains, services) are not identifiers.
	hostRe = regexp.MustCompile(`\.(com|org|net|io|de|at|eu|invalid|example|test|localhost|local)$`)
)

// notIDs are words in backticks that look like identifiers but name
// something else; each with the reason.
var notIDs = map[string]string{
	"deliveries.0.id":           "JSON path in an example answer",
	"entries.0.id":              "JSON path in an example answer",
	"emails.value":              "JSON path in an example",
	"el.permissions":            "expression in a code example",
	"p.host":                    "expression in a code example",
	"data.status":               "expression in a code example (English)",
	"daten.status":              "expression in a code example (German)",
	"hersteller.name":           "placeholder in an example (German)",
	"vendor.name":               "placeholder in an example (English)",
	"ewiki.plugin.v1":           "Protobuf package of the plugin contract",
	"category.permissions":      "expression in a code example",
	"server.wrap":               "Go function (s *Server).wrap in the architecture overview",
	"storage.wopi.alias_groups": "setting of the office server (coolwsd.xml), not of the wiki",
	"watermark.mode":            "field of the media API (watermark object)",
}

// versionRe: version numbers (`v7.5.0`) are not identifiers.
var versionRe = regexp.MustCompile(`^v[0-9]+(\.[0-9]+)+$`)

// prefixRe: a literal ending in a dot is the prefix of a family of identifiers.
var prefixRe = regexp.MustCompile(`["'` + "`" + `]([a-z][a-z0-9_-]*(?:\.[a-z0-9_-]+)*\.)["'` + "`" + `$]`)

// inventoryRoots are searched for string literals.
var inventoryRoots = []string{"internal", "plugins", "web/src", "sdk", "cmd"}

func checkInventory(files []string, add func(string, ...any)) int {
	known := map[string]bool{}
	var prefixes []string
	for _, root := range inventoryRoots {
		_ = fs.WalkDir(repo.FS(), root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if n := d.Name(); n == "node_modules" || n == "dist" || n == "testdata" {
					return fs.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".go", ".ts", ".json", ".yaml", ".yml", ".proto", ".mjs":
			default:
				return nil
			}
			f, err := repo.Open(path)
			if err != nil {
				return nil
			}
			defer func() { _ = f.Close() }()
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 16<<20)
			yml := strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")
			for sc.Scan() {
				line := sc.Text()
				for _, m := range litRe.FindAllStringSubmatch(line, -1) {
					known[m[1]] = true
				}
				for _, m := range prefixRe.FindAllStringSubmatch(line, -1) {
					if strings.Count(m[1], ".") >= 2 { // not a bare "core."
						prefixes = append(prefixes, m[1])
					}
				}
				if yml {
					if m := yamlIDRe.FindStringSubmatch(line); m != nil {
						known[m[1]] = true
					}
				}
			}
			return nil
		})
	}
	keys := map[string]bool{}
	configKeys(reflect.TypeOf(config.Config{}), "", keys)
	configKeys(reflect.TypeOf(actions.Action{}), "", keys)
	exists := func(id string) bool {
		if known[id] || keys[id] {
			return true
		}
		for k := range keys {
			if strings.HasSuffix(k, "."+id) {
				return true
			}
		}
		// a key of a map in the configuration followed by a known identifier
		if i := strings.Index(id, "."); i > 0 && keys[id[:i]+".*"] && known[id[i+1:]] {
			return true
		}
		for _, p := range prefixes {
			if strings.HasPrefix(id, p) && len(id) > len(p) {
				return true
			}
		}
		return false
	}

	n := 0
	for _, file := range files {
		f, err := repo.Open(file)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for line := 1; sc.Scan(); line++ {
			for _, m := range docIDRe.FindAllStringSubmatch(sc.Text(), -1) {
				id := m[1]
				if fileRe.MatchString(id) || hostRe.MatchString(id) || versionRe.MatchString(id) || notIDs[id] != "" {
					continue
				}
				n++
				if !exists(id) {
					add("%s:%d: `%s` does not exist (no string literal in the source, no configuration key)", file, line, id)
				}
			}
		}
		_ = f.Close()
	}
	return n
}

// configKeys adds the dotted keys of config.yaml (yaml tags) below prefix.
func configKeys(t reflect.Type, prefix string, known map[string]bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if tag == "-" || !f.IsExported() {
			continue
		}
		if tag == "" { // inline
			configKeys(f.Type, prefix, known)
			continue
		}
		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}
		known[key] = true
		if f.Type.Kind() == reflect.Map { // keyed by an identifier (plugin_settings.<plugin id>)
			known[key+".*"] = true
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		configKeys(ft, key, known)
	}
}
