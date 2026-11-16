// Package actions is the action register: one entry per action of the API,
// in the format of ewiki.plugin.v1.Action (sdk/proto/ewiki/plugin/v1/
// action.proto). The core keeps its entries as YAML files in core/, one
// file per area; core plugins (compiled in, like the core) add theirs with
// Add. SDK plugins list theirs in their manifest and are checked when they
// are installed. From the register the
// wiki checks that every action is reachable by API, CLI and interface
// (stage 1), and the CLI builds its commands for the actions that have no
// hand-written command.
//
// The Go types mirror the proto messages with the proto field names; the
// SDK's contract check (sdk/tools/protocheck) parses the same files with
// the real message descriptors, so the two cannot drift apart.
package actions

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Text is ewiki.plugin.v1.LocalizedText.
type Text struct {
	Text map[string]string `yaml:"text,omitempty"`
}

// In returns the text in lang, else English.
func (t Text) In(lang string) string {
	if s := t.Text[lang]; s != "" {
		return s
	}
	return t.Text["en"]
}

// Permission is ewiki.plugin.v1.Permission.
type Permission struct {
	Key           string `yaml:"key,omitempty"`
	Scope         string `yaml:"scope,omitempty"`
	CategoryParam string `yaml:"category_param,omitempty"`
	Instance      bool   `yaml:"instance,omitempty"`
}

// Schema is ewiki.plugin.v1.Schema (a JSON schema as text).
type Schema struct {
	JSON string `yaml:"json,omitempty"`
}

// Safety is ewiki.plugin.v1.Safety.
type Safety struct {
	Changes      bool     `yaml:"changes,omitempty"`
	RecentSignIn bool     `yaml:"recent_sign_in,omitempty"`
	Idempotent   bool     `yaml:"idempotent,omitempty"`
	BodyLimit    string   `yaml:"body_limit,omitempty"`
	AuditEvent   string   `yaml:"audit_event,omitempty"`
	SecretFields []string `yaml:"secret_fields,omitempty"`
}

// CLI is ewiki.plugin.v1.CliWay.
type CLI struct {
	Command []string `yaml:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"`
	// Builtin: served by a hand-written command of the wiki's CLI with the
	// same words (core and core plugins; SDK plugins never have one).
	Builtin bool `yaml:"builtin,omitempty"`
}

// UI is ewiki.plugin.v1.UiWay.
type UI struct {
	Entry       string `yaml:"entry,omitempty"`
	Control     Text   `yaml:"control,omitempty"`
	WrittenTest bool   `yaml:"written_test,omitempty"`
}

// Example is ewiki.plugin.v1.Example.
type Example struct {
	Name           string   `yaml:"name"`
	Given          []string `yaml:"given,omitempty"`
	As             string   `yaml:"as,omitempty"`
	PathParamsJSON string   `yaml:"path_params_json,omitempty"`
	QueryJSON      string   `yaml:"query_json,omitempty"`
	BodyJSON       string   `yaml:"body_json,omitempty"`
	Status         int      `yaml:"status,omitempty"`
	ExpectJSON     string   `yaml:"expect_json,omitempty"`
	VerifyAction   string   `yaml:"verify_action,omitempty"`
	// Form and Files make the request multipart/form-data (uploads).
	Form  []FormField   `yaml:"form,omitempty"`
	Files []ExampleFile `yaml:"files,omitempty"`
	// BodyText is a body that is not JSON (e.g. JSON Lines).
	BodyText string `yaml:"body_text,omitempty"`
}

// FormField is ewiki.plugin.v1.FormField.
type FormField struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

// ExampleFile is ewiki.plugin.v1.ExampleFile.
type ExampleFile struct {
	Field       string `yaml:"field"`
	Name        string `yaml:"name"`
	ContentType string `yaml:"content_type,omitempty"`
	Text        string `yaml:"text,omitempty"`
	Base64      string `yaml:"base64,omitempty"`
}

// Exception is ewiki.plugin.v1.Exception: a way that is missing on purpose.
type Exception struct {
	Way        string `yaml:"way"`
	Reason     string `yaml:"reason"`
	ApprovedBy string `yaml:"approved_by"`
}

// Inbound is ewiki.plugin.v1.Inbound (kept as a whole, checked by the SDK).
type Inbound map[string]any

// Action is ewiki.plugin.v1.Action.
type Action struct {
	ID              string      `yaml:"id"`
	Category        string      `yaml:"category"`
	Method          string      `yaml:"method"`
	Path            string      `yaml:"path"`
	Summary         Text        `yaml:"summary"`
	Description     Text        `yaml:"description,omitempty"`
	Permission      *Permission `yaml:"permission,omitempty"`
	SignedIn        bool        `yaml:"signed_in,omitempty"`
	Public          bool        `yaml:"public,omitempty"`
	PathParams      *Schema     `yaml:"path_params,omitempty"`
	Query           *Schema     `yaml:"query,omitempty"`
	Body            *Schema     `yaml:"body,omitempty"`
	Response        *Schema     `yaml:"response,omitempty"`
	SuccessStatus   int         `yaml:"success_status,omitempty"`
	Safety          *Safety     `yaml:"safety,omitempty"`
	CLI             *CLI        `yaml:"cli,omitempty"`
	UI              *UI         `yaml:"ui,omitempty"`
	Examples        []Example   `yaml:"examples,omitempty"`
	Exceptions      []Exception `yaml:"exceptions,omitempty"`
	Preview         bool        `yaml:"preview,omitempty"`
	Tool            bool        `yaml:"tool,omitempty"`
	ToolDescription string      `yaml:"tool_description,omitempty"`
	Inbound         Inbound     `yaml:"inbound,omitempty"`

	// Plugin is the id of the core plugin that registered the entry ("" for
	// the core); Source the file it came from.
	Plugin string `yaml:"-"`
	Source string `yaml:"-"`
}

// Route is "METHOD /full/path" as the server registers it.
func (a Action) Route() string { return a.Method + " " + a.FullPath() }

// FullPath is the path below the host: plugin paths are relative to
// /api/v1/x/<plugin>/.
func (a Action) FullPath() string {
	if a.Plugin != "" {
		return "/api/v1/x/" + a.Plugin + "/" + a.Path
	}
	return a.Path
}

// PathParamNames lists the names of the parameters in braces, in order.
func (a Action) PathParamNames() []string {
	var out []string
	for _, m := range paramRe.FindAllStringSubmatch(a.Path, -1) {
		out = append(out, m[1])
	}
	return out
}

// HasException reports whether the way (WAY_CLI, WAY_UI, WAY_API) is
// missing on purpose.
func (a Action) HasException(way string) bool {
	return slices.ContainsFunc(a.Exceptions, func(e Exception) bool { return e.Way == way })
}

// file is one register file.
type file struct {
	Actions []Action `yaml:"actions"`
}

//go:embed core/*.yaml
var coreFiles embed.FS

var (
	mu      sync.Mutex
	plugins = map[string][]byte{} // plugin id -> register file
	loaded  []Action
	loadErr error
	done    bool
)

// Add registers the action file of a core plugin (called from its init).
func Add(plugin string, data []byte) {
	mu.Lock()
	defer mu.Unlock()
	plugins[plugin] = data
	done = false
}

// All returns the whole register (core and core plugins), validated. The
// result is cached until a plugin adds its file.
func All() ([]Action, error) {
	mu.Lock()
	defer mu.Unlock()
	if done {
		return loaded, loadErr
	}
	loaded, loadErr = load()
	done = true
	return loaded, loadErr
}

func load() ([]Action, error) {
	var all []Action
	names, err := fs.Glob(coreFiles, "core/*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	for _, n := range names {
		b, err := coreFiles.ReadFile(n)
		if err != nil {
			return nil, err
		}
		as, err := parse(b, "internal/actions/"+n, "")
		if err != nil {
			return nil, err
		}
		all = append(all, as...)
	}
	ids := make([]string, 0, len(plugins))
	for id := range plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		as, err := parse(plugins[id], "plugins/"+id+"/actions.yaml", id)
		if err != nil {
			return nil, err
		}
		all = append(all, as...)
	}
	return all, Validate(all)
}

// parse reads one register file strictly (unknown fields are errors).
func parse(b []byte, source, plugin string) ([]Action, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var f file
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	for i := range f.Actions {
		f.Actions[i].Plugin = plugin
		f.Actions[i].Source = source
	}
	return f.Actions, nil
}

var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,119}$`)
	wordRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	paramRe   = regexp.MustCompile(`\{([a-z_][a-z0-9_]*)(?:\.\.\.)?\}`)
	methods   = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
	statuses  = map[int]bool{0: true, 200: true, 201: true, 202: true, 204: true}
	ways      = map[string]bool{"WAY_CLI": true, "WAY_UI": true, "WAY_API": true}
	scopes    = map[string]bool{"": true, "SCOPE_GLOBAL": true, "SCOPE_CATEGORY": true}
	langsNeed = []string{"en", "de"}
	langRe    = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z][A-Za-z0-9]{1,7})?$`)
)

// Validate checks the register as a whole: the rules of action.proto plus
// the project's (texts in German and English, approved exceptions).
func Validate(all []Action) error {
	var errs []error
	fail := func(a Action, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s: %s", a.Source, a.ID, fmt.Sprintf(format, args...)))
	}
	ids := map[string]bool{}
	routes := map[string]string{}
	commands := map[string]string{}
	for _, a := range all {
		if !idRe.MatchString(a.ID) {
			fail(a, "id: lower case letters, digits, dots, hyphens, underscores; at most 120 characters")
		}
		switch {
		case a.Plugin == "" && !strings.HasPrefix(a.ID, "core."):
			fail(a, "core ids start with \"core.\"")
		case a.Plugin != "" && !strings.HasPrefix(a.ID, a.Plugin+"."):
			fail(a, "plugin ids start with the plugin id %q", a.Plugin+".")
		}
		if ids[a.ID] {
			fail(a, "id used twice")
		}
		ids[a.ID] = true
		if !methods[a.Method] {
			fail(a, "method %q", a.Method)
		}
		switch {
		case a.Plugin == "" && !strings.HasPrefix(a.Path, "/"):
			fail(a, "core paths are absolute")
		case a.Plugin == "" && a.Category == "CATEGORY_ACTION" && !strings.HasPrefix(a.Path, "/api/v1/"):
			fail(a, "core actions live below /api/v1/")
		case a.Plugin != "" && (strings.HasPrefix(a.Path, "/") || strings.Contains(a.Path, "..")):
			fail(a, "plugin paths are relative to /api/v1/x/%s/", a.Plugin)
		}
		if prev, ok := routes[a.Route()]; ok {
			fail(a, "route %s already belongs to %s", a.Route(), prev)
		}
		routes[a.Route()] = a.ID
		for _, l := range langsNeed {
			if strings.TrimSpace(a.Summary.Text[l]) == "" {
				fail(a, "summary.text.%s is missing", l)
			}
		}
		// every key is a language with a text (a comma in a YAML flow map
		// "{en: a, b, de: c}" would add a key "b" without a text)
		for _, t := range []Text{a.Summary, a.Description, uiControl(a)} {
			for l, v := range t.Text {
				if !langRe.MatchString(l) || strings.TrimSpace(v) == "" {
					fail(a, "text %q: keys are languages (en, de, pt-BR) with a text; quote texts with commas", l)
				}
			}
		}
		if !statuses[a.SuccessStatus] {
			fail(a, "success_status %d (200, 201, 202 or 204)", a.SuccessStatus)
		}
		if a.Permission != nil {
			if a.Permission.Key == "" || !scopes[a.Permission.Scope] {
				fail(a, "permission needs a key and a valid scope")
			}
			if a.Permission.Scope == "SCOPE_CATEGORY" {
				checkCategoryPermission(a, fail)
			}
		}
		access := 0
		if a.Permission != nil {
			access++
		}
		if a.SignedIn {
			access++
		}
		if a.Public {
			access++
		}
		for _, e := range a.Exceptions {
			if !ways[e.Way] || strings.TrimSpace(e.Reason) == "" || strings.TrimSpace(e.ApprovedBy) == "" {
				fail(a, "exceptions need a way, a reason and who approved them")
			}
		}
		switch a.Category {
		case "CATEGORY_ACTION":
			if access != 1 {
				fail(a, "exactly one of permission, signed_in, public")
			}
			if (a.CLI == nil || len(a.CLI.Command) == 0) && !a.HasException("WAY_CLI") {
				fail(a, "no CLI command and no approved exception")
			}
			if (a.UI == nil || a.UI.Entry == "") && !a.HasException("WAY_UI") {
				fail(a, "no entry in the interface and no approved exception")
			}
		case "CATEGORY_PROTOCOL":
			if access > 1 {
				fail(a, "at most one of permission, signed_in, public")
			}
			if a.CLI != nil || a.UI != nil {
				fail(a, "protocol endpoints have no CLI or interface way")
			}
		default:
			fail(a, "category %q (CATEGORY_ACTION or CATEGORY_PROTOCOL)", a.Category)
		}
		if a.CLI != nil {
			for _, w := range a.CLI.Command {
				if !wordRe.MatchString(w) {
					fail(a, "cli.command word %q", w)
				}
			}
			// a hand-written command may serve several actions (show and set)
			key := strings.Join(a.CLI.Command, " ")
			if !a.CLI.Builtin {
				if prev, ok := commands[key]; ok && key != "" {
					fail(a, "cli command %q already belongs to %s", key, prev)
				}
				commands[key] = a.ID
			}
			params := a.PathParamNames()
			for _, arg := range a.CLI.Args {
				if !slices.Contains(params, arg) && !schemaHas(a.Query, arg) && !schemaHas(a.Body, arg) {
					fail(a, "cli.args: %q is no path, query or body parameter", arg)
				}
			}
		}
		for _, ex := range a.Examples {
			if ex.Name == "" {
				fail(a, "examples need a name")
			}
			if ex.BodyText != "" && (ex.BodyJSON != "" || len(ex.Form) > 0 || len(ex.Files) > 0) {
				fail(a, "example %s: body_text excludes body_json and form/files", ex.Name)
			}
			if (len(ex.Form) > 0 || len(ex.Files) > 0) && ex.BodyJSON != "" {
				fail(a, "example %s: form/files (multipart) and body_json exclude each other", ex.Name)
			}
			for _, f := range ex.Files {
				if f.Field == "" || f.Name == "" || (f.Text != "" && f.Base64 != "") {
					fail(a, "example %s: a file needs field and name, and text or base64", ex.Name)
				}
			}
		}
	}
	// a generated command must not be the start of another generated one:
	// the words after it would be read as arguments
	keys := make([]string, 0, len(commands))
	for k := range commands {
		if k != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for i := 0; i+1 < len(keys); i++ {
		if strings.HasPrefix(keys[i+1], keys[i]+" ") {
			errs = append(errs, fmt.Errorf("cli command %q (%s) is the start of %q (%s)", keys[i], commands[keys[i]], keys[i+1], commands[keys[i+1]]))
		}
	}
	return errors.Join(errs...)
}

func uiControl(a Action) Text {
	if a.UI == nil {
		return Text{}
	}
	return a.UI.Control
}

// schemaHas reports whether the JSON schema text names a property (a
// cheap check; the schema itself is checked by the SDK's contract check).
func schemaHas(s *Schema, name string) bool {
	return s != nil && strings.Contains(s.JSON, `"`+name+`"`)
}

// ByCommand finds the action whose CLI command is the longest start of
// words; n is the number of words it took.
func ByCommand(all []Action, words []string) (a Action, n int, ok bool) {
	for _, x := range all {
		if x.CLI == nil || len(x.CLI.Command) == 0 || len(x.CLI.Command) > len(words) || len(x.CLI.Command) <= n {
			continue
		}
		if slices.Equal(x.CLI.Command, words[:len(x.CLI.Command)]) {
			a, n, ok = x, len(x.CLI.Command), true
		}
	}
	return a, n, ok
}

// Commands lists the CLI commands of the register below a start of words.
func Commands(all []Action, prefix []string) []Action {
	var out []Action
	for _, a := range all {
		if a.CLI != nil && len(a.CLI.Command) >= len(prefix) && slices.Equal(a.CLI.Command[:len(prefix)], prefix) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i].CLI.Command, " ") < strings.Join(out[j].CLI.Command, " ")
	})
	return out
}

// MatchRoute reports whether a route pattern of the server ("POST
// /api/v1/pages/{id}") is the route of the action.
func (a Action) MatchRoute(route string) bool { return route == a.Route() }

// CategoryRights are the permission keys of the core for SCOPE_CATEGORY:
// the access bits of a category (see internal/acl).
var CategoryRights = map[string]bool{
	"core.category.view": true, "core.category.edit": true, "core.category.create": true,
	"core.category.delete": true, "core.category.manage": true,
}

// checkCategoryPermission checks a category-scoped permission: a known
// right of the core, and a parameter that the request really carries. A
// parameter that is not in the path is a query or body field.
func checkCategoryPermission(a Action, fail func(Action, string, ...any)) {
	p := a.Permission
	// the core's rights, or (in a plugin) one of the plugin's own rights
	if !CategoryRights[p.Key] && (a.Plugin == "" || !strings.HasPrefix(p.Key, a.Plugin+".")) {
		fail(a, "permission %q is no category right (core.category.view, .edit, .create, .delete, .manage, or one of the plugin)", p.Key)
	}
	param, ok := strings.CutPrefix(p.CategoryParam, "element:")
	if param == "" || strings.ContainsAny(param, "{}/: ") {
		fail(a, "permission.category_param names the category parameter, or element:<param>")
		return
	}
	if ok && !strings.Contains(a.Path, "{"+param+"}") {
		fail(a, "permission.category_param %q: the element is named in the path", p.CategoryParam)
	}
}
