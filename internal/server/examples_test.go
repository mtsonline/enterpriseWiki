package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/actions"
	"github.com/mtsonline/enterprisewiki/internal/cli"
)

// examplesMissing lists the actions that have no example yet (phase 4 fills
// them area by area; the list may only shrink).
const examplesMissing = "../actions/examples-missing.txt"

var (
	givenRe   = regexp.MustCompile(`\$\{given\.([^#}]+#[^.}]+)\.([^}]+)\}`)
	httpErrRe = regexp.MustCompile(`HTTP (\d{3})`)
)

// TestActionExamples (phase 4, stage 2): every example of the action
// register runs on the API and on the command line with the same result;
// for examples "as member" a person without the permission (outsider) is
// refused on both ways. Actions without an example are listed in
// examples-missing.txt, which may only shrink.
func TestActionExamples(t *testing.T) {
	all, err := actions.All()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]actions.Action{}
	for _, a := range all {
		byID[a.ID] = a
	}
	checkExampleCoverage(t, all)

	e := setup(t)
	r := &exampleRunner{t: t, e: e, byID: byID}
	r.admin = e.client()
	r.admin.login("admin", adminPW)
	ids := make([]string, 0, len(byID))
	for id, a := range byID {
		if len(a.Examples) > 0 {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		a := byID[id]
		for _, ex := range a.Examples {
			t.Run(id+"#"+ex.Name, func(t *testing.T) {
				r.t = t
				r.run(a, ex)
			})
		}
	}
}

// checkExampleCoverage: every action of the category ACTION has an example
// unless it is listed as missing, and the list holds no action that has
// one (or no longer exists).
func checkExampleCoverage(t *testing.T, all []actions.Action) {
	t.Helper()
	var missing []string
	has := map[string]bool{}
	for _, a := range all {
		if a.Category == "CATEGORY_ACTION" && len(a.Examples) == 0 {
			missing = append(missing, a.ID)
		}
		has[a.ID] = len(a.Examples) > 0 || a.Category != "CATEGORY_ACTION"
	}
	sort.Strings(missing)
	if os.Getenv("EWIKI_ACTIONS_UPDATE") == "1" {
		head := "# Actions without an example (phase 4 adds them area by area; the list may\n# only shrink). Rewritten with EWIKI_ACTIONS_UPDATE=1.\n"
		if err := os.WriteFile(examplesMissing, []byte(head+strings.Join(missing, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(examplesMissing)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	listed := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" && !strings.HasPrefix(l, "#") {
			listed[l] = true
		}
	}
	for _, id := range missing {
		if !listed[id] {
			t.Errorf("%s has no example: add one to its register entry (examples)", id)
		}
	}
	for id := range listed {
		switch got, ok := has[id]; {
		case !ok:
			t.Errorf("%s no longer exists: remove it from %s", id, examplesMissing)
		case got:
			t.Errorf("%s has an example now: remove it from %s", id, examplesMissing)
		}
	}
}

type exampleRunner struct {
	t      *testing.T
	e      *env
	byID   map[string]actions.Action
	admin  *client
	unique int
}

// result of one request
type exResult struct {
	ok     bool // 2xx
	status int  // 0 on the command line when it succeeded (no status)
	body   any
	raw    string
}

// run runs one example on both ways.
func (r *exampleRunner) run(a actions.Action, ex actions.Example) {
	t := r.t
	want := ex.Status // 0: any success
	if want == 0 {
		want = a.SuccessStatus
	}
	for _, way := range []string{"api", "cli"} {
		vars := map[string]any{}
		r.given(ex.Given, vars, 0)
		path, query, body := r.request(a, ex, vars)
		who := r.who(a, ex, path, query, body)
		res := r.do(way, a, who, path, query, body)
		if (want < 300) != res.ok || (way == "api" && want != 0 && res.status != want) || (way == "cli" && !res.ok && res.status != want) {
			t.Fatalf("%s: status %d (ok=%v), want %d: %s", way, res.status, res.ok, want, res.raw)
		}
		if ex.ExpectJSON != "" && want < 300 {
			var exp any
			if err := json.Unmarshal([]byte(r.subst(ex.ExpectJSON, vars)), &exp); err != nil {
				t.Fatalf("expect_json: %v", err)
			}
			if !subset(exp, res.body) {
				t.Fatalf("%s: the answer does not contain %s:\n%s", way, ex.ExpectJSON, res.raw)
			}
		}
		// the same request by someone without the permission is refused
		if ex.As == "member" && a.Permission != nil {
			out := r.outsider()
			if res := r.do(way, a, out, path, query, body); res.ok || (res.status != 401 && res.status != 403 && res.status != 404) {
				t.Fatalf("%s: a person without the permission was not refused: %d %s", way, res.status, res.raw)
			}
		}
	}
}

// given runs the examples ex refers to (as administrator, through the API)
// and stores their answers.
func (r *exampleRunner) given(refs []string, vars map[string]any, depth int) {
	t := r.t
	if depth > 8 {
		t.Fatal("given: too deep")
	}
	for _, ref := range refs {
		if _, done := vars[ref]; done {
			continue
		}
		id, name, _ := strings.Cut(ref, "#")
		a, ok := r.byID[id]
		if !ok {
			t.Fatalf("given %s: no such action", ref)
		}
		var ex *actions.Example
		for i := range a.Examples {
			if a.Examples[i].Name == name {
				ex = &a.Examples[i]
			}
		}
		if ex == nil {
			t.Fatalf("given %s: no such example", ref)
		}
		r.given(ex.Given, vars, depth+1)
		path, query, body := r.request(a, *ex, vars)
		res := r.do("api", a, who{cl: r.admin, name: "admin"}, path, query, body)
		if !res.ok {
			t.Fatalf("given %s: %d %s", ref, res.status, res.raw)
		}
		vars[ref] = res.body
	}
}

// request fills in path, query and body of an example.
func (r *exampleRunner) request(a actions.Action, ex actions.Example, vars map[string]any) (string, map[string]string, string) {
	t := r.t
	path := a.FullPath()
	if ex.PathParamsJSON != "" {
		var pp map[string]any
		if err := json.Unmarshal([]byte(r.subst(ex.PathParamsJSON, vars)), &pp); err != nil {
			t.Fatalf("path_params_json: %v", err)
		}
		for k, v := range pp {
			path = strings.Replace(path, "{"+k+"}", fmt.Sprint(v), 1)
		}
	}
	if strings.Contains(path, "{") {
		t.Fatalf("path %s: a path parameter has no value", path)
	}
	query := map[string]string{}
	if ex.QueryJSON != "" {
		var q map[string]any
		if err := json.Unmarshal([]byte(r.subst(ex.QueryJSON, vars)), &q); err != nil {
			t.Fatalf("query_json: %v", err)
		}
		for k, v := range q {
			query[k] = fmt.Sprint(v)
		}
	}
	body := ""
	if ex.BodyJSON != "" {
		body = r.subst(ex.BodyJSON, vars)
		if !json.Valid([]byte(body)) {
			t.Fatalf("body_json is not valid JSON after filling in: %s", body)
		}
	}
	return path, query, body
}

// subst fills in ${given.REF.FIELD} and ${unique}.
func (r *exampleRunner) subst(s string, vars map[string]any) string {
	s = givenRe.ReplaceAllStringFunc(s, func(m string) string {
		p := givenRe.FindStringSubmatch(m)
		res, ok := vars[p[1]]
		if !ok {
			r.t.Fatalf("%s: %s is not given", m, p[1])
		}
		v := res
		for _, f := range strings.Split(p[2], ".") {
			mm, _ := v.(map[string]any)
			v = mm[f]
		}
		switch x := v.(type) {
		case nil:
			r.t.Fatalf("%s: no such field in %v", m, res)
		case float64:
			return fmt.Sprint(int64(x))
		case string:
			return x
		}
		return fmt.Sprint(v)
	})
	for strings.Contains(s, "${unique}") {
		r.unique++
		s = strings.Replace(s, "${unique}", fmt.Sprintf("x%d", r.unique), 1)
	}
	return s
}

type who struct {
	cl   *client // nil: guest
	name string  // user name for --as ("" = CLI superuser)
}

// who returns the person an example runs as. A member gets exactly the
// permission of the register entry.
func (r *exampleRunner) who(a actions.Action, ex actions.Example, path string, query map[string]string, body string) who {
	switch ex.As {
	case "", "admin":
		return who{cl: r.admin, name: "admin"}
	case "guest":
		return who{cl: r.e.client()}
	case "outsider":
		return r.outsider()
	case "member":
	default:
		r.t.Fatalf("as %q: admin, member, outsider or guest", ex.As)
	}
	name, cl := r.user()
	p := a.Permission
	switch {
	case p == nil:
		// signed_in or public: any signed-in person
	case p.Key == "core.admin":
		r.t.Fatalf("%s needs the administrator role: use as admin", a.ID)
	case p.Scope == "SCOPE_GLOBAL":
		var role struct {
			ID int64 `json:"id"`
		}
		r.admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "role " + name, "permissions": []string{p.Key}}, &role, 200)
		uid := r.userID(name)
		r.admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", uid), map[string]any{"roles": []int64{role.ID}}, nil, 200)
	case p.Scope == "SCOPE_CATEGORY":
		cat := r.categoryOf(p.CategoryParam, a, path, query, body)
		rule := map[string]any{"subject_type": "user", "subject_id": r.userID(name), "permissions": int(acl.View), "inherit": true}
		if bit, ok := map[string]acl.Perm{"core.category.view": acl.View, "core.category.edit": acl.Edit, "core.category.create": acl.Create,
			"core.category.delete": acl.Delete, "core.category.manage": acl.Manage}[p.Key]; ok {
			rule["permissions"] = int(acl.View | bit)
		} else {
			rule["actions"] = []string{p.Key}
		}
		r.admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat), rule, nil, 201)
	}
	cl.login(name, "Example-Passw0rd!"+name) // after the grants: the session carries them
	return who{cl: cl, name: name}
}

func (r *exampleRunner) outsider() who {
	name, cl := r.user()
	cl.login(name, "Example-Passw0rd!"+name)
	return who{cl: cl, name: name}
}

func (r *exampleRunner) user() (string, *client) {
	r.unique++
	name := fmt.Sprintf("ex%d", r.unique)
	r.admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": "Example-Passw0rd!" + name, "display_name": name}, nil, 201)
	return name, r.e.client()
}

func (r *exampleRunner) userID(name string) int64 {
	var id int64
	if err := r.e.app.DB.QueryRow(context.Background(), `SELECT id FROM users WHERE username=$1`, name).Scan(&id); err != nil {
		r.t.Fatal(err)
	}
	return id
}

// categoryOf finds the category a category-scoped permission is checked on.
func (r *exampleRunner) categoryOf(param string, a actions.Action, path string, query map[string]string, body string) int64 {
	el, isElement := strings.CutPrefix(param, "element:")
	var v string
	// path parameter: the value in the filled-in path
	tmpl := strings.Split(a.FullPath(), "/")
	parts := strings.Split(path, "/")
	for i, seg := range tmpl {
		if seg == "{"+el+"}" && i < len(parts) {
			v = parts[i]
		}
	}
	if v == "" {
		v = query[el]
	}
	if v == "" && body != "" {
		var b map[string]any
		_ = json.Unmarshal([]byte(body), &b)
		if x, ok := b[el]; ok {
			v = fmt.Sprint(x)
		}
	}
	if v == "" {
		r.t.Fatalf("%s: no value for the category parameter %s", a.ID, param)
	}
	var id int64
	if _, err := fmt.Sscan(v, &id); err != nil {
		r.t.Fatalf("%s: category parameter %s=%q", a.ID, param, v)
	}
	if isElement {
		if err := r.e.app.DB.QueryRow(context.Background(), `SELECT category_id FROM elements WHERE id=$1`, id).Scan(&id); err != nil {
			r.t.Fatalf("%s: element %d: %v", a.ID, id, err)
		}
	}
	return id
}

// do runs the request on one way as w.
func (r *exampleRunner) do(way string, a actions.Action, w who, path string, query map[string]string, body string) exResult {
	if way == "api" {
		cl := w.cl
		if cl == nil {
			cl = r.e.client()
		}
		u := path
		if len(query) > 0 {
			sep := "?"
			for k, v := range query {
				u += sep + k + "=" + v
				sep = "&"
			}
		}
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, _ := http.NewRequest(a.Method, r.e.srv.URL+u, rd)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "ewiki")
		res, err := cl.http.Do(req)
		if err != nil {
			r.t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		out := exResult{ok: res.StatusCode < 300, status: res.StatusCode, raw: string(b)}
		_ = json.Unmarshal(b, &out.body)
		return out
	}
	// command line: the generated way with --as (guests cannot use it)
	var args []string
	for _, name := range a.PathParamNames() {
		args = append(args, "--"+name, pathValue(a.FullPath(), path, name))
	}
	for k, v := range query {
		if a.Query == nil {
			args = append(args, "--query", k+"="+v)
		} else {
			args = append(args, "--"+k, v)
		}
	}
	if body != "" {
		args = append(args, "--json", body)
	}
	if w.name != "" && w.name != "admin" {
		args = append(args, "--as", w.name)
	}
	if w.cl == nil || w.name == "" { // a guest: the command line always has an account
		return r.do("api", a, w, path, query, body)
	}
	r.t.Setenv("EWIKI_CONFIG", r.e.cfg)
	var stdout, stderr bytes.Buffer
	code := cli.RunAction(a.ID, args, strings.NewReader(""), &stdout, &stderr)
	out := exResult{ok: code == 0, raw: stdout.String() + stderr.String()}
	if m := httpErrRe.FindStringSubmatch(stderr.String()); m != nil {
		_, _ = fmt.Sscan(m[1], &out.status)
	}
	_ = json.Unmarshal(stdout.Bytes(), &out.body)
	return out
}

// pathValue reads the value of parameter name from the filled-in path.
func pathValue(tmpl, path, name string) string {
	ts, ps := strings.Split(tmpl, "/"), strings.Split(path, "/")
	for i, s := range ts {
		if s == "{"+name+"}" && i < len(ps) {
			return ps[i]
		}
	}
	return ""
}

// subset reports whether every value of want is in got (objects partly,
// everything else equal; numbers as JSON numbers).
func subset(want, got any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range w {
			if !subset(v, g[k]) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok {
			return false
		}
		// every wanted item is somewhere in the list
		for _, x := range w {
			found := false
			for _, y := range g {
				if subset(x, y) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(want, got)
}
