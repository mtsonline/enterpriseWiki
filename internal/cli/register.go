package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/actions"
)

// Commands of the action register: every action without a hand-written
// command becomes "ewiki <words> [ARGS] [--PARAM VALUE] [--json BODY]
// [--as USER]". They always go through the API (in-process, as the
// superadmin or as --as USER), so rights, checks and audit are the same as
// in the interface.

// generated finds the register action for the words, if it has no
// hand-written command.
func generated(args []string) (actions.Action, int, bool) {
	all, err := actions.All()
	if err != nil {
		return actions.Action{}, 0, false
	}
	a, n, ok := actions.ByCommand(all, args)
	if !ok || a.CLI.Builtin {
		return actions.Action{}, 0, false
	}
	return a, n, true
}

// schemaProps reads the properties of a JSON schema (name -> type).
func schemaProps(s *actions.Schema) map[string]string {
	out := map[string]string{}
	if s == nil || s.JSON == "" {
		return out
	}
	var sch struct {
		Properties map[string]struct {
			Type any `json:"type"`
		} `json:"properties"`
	}
	if json.Unmarshal([]byte(s.JSON), &sch) != nil {
		return out
	}
	for name, p := range sch.Properties {
		t, _ := p.Type.(string)
		out[name] = t
	}
	return out
}

// typed turns a flag value into the JSON type of the schema.
func typed(name, typ, v string) (any, error) {
	switch typ {
	case "integer":
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("--%s: a whole number", name)
		}
		return n, nil
	case "number":
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("--%s: a number", name)
		}
		return f, nil
	case "boolean":
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("--%s: true or false", name)
		}
		return b, nil
	case "array", "object":
		var x any
		if err := json.Unmarshal([]byte(v), &x); err != nil {
			return nil, fmt.Errorf("--%s: JSON", name)
		}
		return x, nil
	}
	return v, nil
}

// actionUsage describes the command of an action.
func actionUsage(a actions.Action) string {
	var b strings.Builder
	fmt.Fprintf(&b, "usage: ewiki %s", strings.Join(a.CLI.Command, " "))
	for _, arg := range a.CLI.Args {
		fmt.Fprintf(&b, " %s", strings.ToUpper(arg))
	}
	params := a.PathParamNames()
	query, body := schemaProps(a.Query), schemaProps(a.Body)
	for _, p := range params {
		if !slices.Contains(a.CLI.Args, p) {
			fmt.Fprintf(&b, " --%s VALUE", p)
		}
	}
	for _, set := range []map[string]string{query, body} {
		names := make([]string, 0, len(set))
		for n := range set {
			if !slices.Contains(a.CLI.Args, n) {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&b, " [--%s VALUE]", n)
		}
	}
	if len(query) == 0 && a.Method == "GET" {
		b.WriteString(" [--query NAME=VALUE]...")
	}
	if a.Method != "GET" && a.Method != "DELETE" {
		b.WriteString(" [--json BODY|@FILE|-] [--form NAME=VALUE]... [--file FIELD=@PATH]...")
	}
	b.WriteString(" [--as USER]\n\n")
	fmt.Fprintf(&b, "%s\n\n%s %s\n", a.Summary.In("en"), a.Method, a.FullPath())
	switch {
	case a.Permission != nil:
		fmt.Fprintf(&b, "Permission: %s\n", a.Permission.Key)
	case a.Public:
		b.WriteString("Permission: none (public)\n")
	case a.SignedIn:
		b.WriteString("Permission: signed-in person\n")
	}
	return b.String()
}

// runAction runs a register action with its arguments.
func (e *env) runAction(a actions.Action, args []string) error {
	if a.CLI == nil { // an approved exception: no command line way
		return fmt.Errorf("%s has no command (exception: %s)", a.ID, exceptionReason(a, "WAY_CLI"))
	}
	params := a.PathParamNames()
	query, body := schemaProps(a.Query), schemaProps(a.Body)
	values := map[string]string{} // path parameters
	q := url.Values{}
	fields := map[string]any{}
	var rawBody []byte
	as := ""
	// uploads: text fields and files of a multipart body, in this order
	var form [][2]string
	var files [][2]string // field, path
	var pos []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-h" || arg == "--help" || arg == "help" && len(pos) == 0 {
			e.printf("%s", actionUsage(a))
			return nil
		}
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			pos = append(pos, arg)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if !hasVal {
			if i+1 >= len(args) {
				return fmt.Errorf("--%s needs a value\n\n%s", name, actionUsage(a))
			}
			i++
			val = args[i]
		}
		switch {
		case name == "as":
			as = val
		case name == "json" && a.Method != "GET" && a.Method != "DELETE":
			b, err := readBody(val, e.stdin)
			if err != nil {
				return err
			}
			rawBody = b
		case name == "form" && a.Method != "GET" && a.Method != "DELETE":
			k, v, ok := strings.Cut(val, "=")
			if !ok || k == "" {
				return errors.New("--form NAME=VALUE")
			}
			form = append(form, [2]string{k, v})
		case name == "file" && a.Method != "GET" && a.Method != "DELETE":
			k, v, ok := strings.Cut(val, "=@")
			if !ok || k == "" || v == "" {
				return errors.New("--file FIELD=@PATH")
			}
			files = append(files, [2]string{k, v})
		case name == "query" && len(query) == 0:
			k, v, ok := strings.Cut(val, "=")
			if !ok {
				return errors.New("--query NAME=VALUE")
			}
			q.Add(k, v)
		case slices.Contains(params, name):
			values[name] = val
		case query[name] != "" || hasKey(query, name):
			q.Set(name, val)
		case hasKey(body, name):
			v, err := typed(name, body[name], val)
			if err != nil {
				return err
			}
			fields[name] = v
		default:
			return fmt.Errorf("unknown option --%s\n\n%s", name, actionUsage(a))
		}
	}
	// positional arguments in the order of cli.args
	if len(pos) > len(a.CLI.Args) {
		return fmt.Errorf("too many arguments\n\n%s", actionUsage(a))
	}
	for i, v := range pos {
		name := a.CLI.Args[i]
		switch {
		case slices.Contains(params, name):
			values[name] = v
		case hasKey(query, name):
			q.Set(name, v)
		default:
			t, err := typed(name, body[name], v)
			if err != nil {
				return err
			}
			fields[name] = t
		}
	}
	p := a.FullPath()
	for _, name := range params {
		v, ok := values[name]
		if !ok || v == "" {
			return fmt.Errorf("%s is missing\n\n%s", strings.ToUpper(name), actionUsage(a))
		}
		p = strings.Replace(p, "{"+name+"}", url.PathEscape(v), 1)
	}
	if len(q) > 0 {
		p += "?" + q.Encode()
	}
	if len(fields) > 0 {
		if rawBody != nil {
			return errors.New("give the body either with --json or with single options, not both")
		}
		b, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		rawBody = b
	}
	if len(form) > 0 || len(files) > 0 {
		if rawBody != nil {
			return errors.New("give either a JSON body (--json, options) or an upload (--form, --file), not both")
		}
		body, ct, err := multipartBody(form, files)
		if err != nil {
			return err
		}
		return e.apiBodyType(a.Method, p, body, ct, as)
	}
	return e.apiBody(a.Method, p, rawBody, as)
}

func hasKey(m map[string]string, k string) bool {
	_, ok := m[k]
	return ok
}

// readBody: inline JSON, @FILE or - (standard input).
func readBody(v string, stdin io.Reader) ([]byte, error) {
	switch {
	case v == "-":
		return io.ReadAll(io.LimitReader(stdin, 64<<20))
	case strings.HasPrefix(v, "@"):
		return os.ReadFile(strings.TrimPrefix(v, "@")) // #nosec G304 -- a file the person names on their own command line
	}
	if !json.Valid([]byte(v)) {
		return nil, errors.New("--json: not valid JSON")
	}
	return []byte(v), nil
}

// registerHelp lists the commands of the action register.
func registerHelp() string {
	all, err := actions.All()
	if err != nil {
		return "\nCommands of the action register: not available (" + err.Error() + ")\n"
	}
	var b strings.Builder
	b.WriteString("\nCommands from the action register (ewiki COMMAND --help for details):\n")
	for _, a := range actions.Commands(all, nil) {
		if a.CLI.Builtin {
			continue
		}
		fmt.Fprintf(&b, "  %-36s %s\n", strings.Join(a.CLI.Command, " "), a.Summary.In("en"))
	}
	return b.String()
}

// RunAction runs the register action id with command-line arguments
// ([--PARAM VALUE]... [--json BODY] [--as USER]) the way generated
// commands do, also for actions with a hand-written command. It returns
// the exit code like Main. Used by the example runner of the parity check
// (phase 4), which runs every example on the API and on the command line.
func RunAction(id string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	e := &env{stdout: stdout, stderr: stderr, stdin: stdin, configPath: os.Getenv("EWIKI_CONFIG"), log: newLogger()}
	all, err := actions.All()
	if err != nil {
		e.eprintln("error:", err)
		return 1
	}
	i := slices.IndexFunc(all, func(a actions.Action) bool { return a.ID == id })
	if i < 0 {
		e.eprintln("error: unknown action", id)
		return 2
	}
	if err := e.runAction(all[i], args); err != nil {
		e.eprintln("error:", err)
		return 1
	}
	return 0
}

// multipartBody builds a multipart/form-data body: the text fields in the
// given order, then the files (uploads of generated commands).
func multipartBody(form, files [][2]string) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range form {
		if err := w.WriteField(f[0], f[1]); err != nil {
			return nil, "", err
		}
	}
	for _, f := range files {
		data, err := os.ReadFile(f[1]) // #nosec G304 -- a file the person names on their own command line
		if err != nil {
			return nil, "", err
		}
		part, err := w.CreateFormFile(f[0], filepath.Base(f[1]))
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(data); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// exceptionReason returns the reason of the approved exception for way.
func exceptionReason(a actions.Action, way string) string {
	for _, x := range a.Exceptions {
		if x.Way == way {
			return x.Reason
		}
	}
	return "none registered"
}
