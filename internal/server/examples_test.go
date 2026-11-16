package server_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/actions"
	"github.com/mtsonline/enterprisewiki/internal/cli"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/license"
	"github.com/mtsonline/enterprisewiki/plugins/content_forms"
	"github.com/mtsonline/enterprisewiki/plugins/mfa_totp"
)

// examplesMissing lists the actions that have no example yet (phase 4 fills
// them area by area; the list may only shrink).
const examplesMissing = "../actions/examples-missing.txt"

var (
	totpRe        = regexp.MustCompile(`\$\{totp:([A-Za-z2-7= ]+)\}`)
	webauthnRe    = regexp.MustCompile(`\$\{webauthn\.(create|get):([^#}]+#[^.}]+)\.?([^}]*)\}`)
	mailLinkRe    = regexp.MustCompile(`\$\{mail_link:([^:}]+):([a-z_]+)\}`)
	pkcs12Re      = regexp.MustCompile(`\$\{pkcs12:([^}]+)\}`)
	localPluginRe = regexp.MustCompile(`\$\{local_plugin:([a-z0-9.]+)\}`)
	systemRe      = regexp.MustCompile(`\$\{system\.([a-z0-9]+)\.([a-z_]+)\}`)
	personRe      = regexp.MustCompile(`\$\{(self|other)\.([a-z]+)\}`)
	givenRe       = regexp.MustCompile(`\$\{given\.([^#}]+#[^.}]+)\.([^}]+)\}`)
	httpErrRe     = regexp.MustCompile(`HTTP (\d{3})`)
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
	for sys := range actions.TestSystems {
		if _, ok := systemPlugins[sys]; !ok {
			t.Errorf("test system %s: add the plugins its examples configure to systemPlugins", sys)
		}
	}
	// the example of core.license.install uses a key signed for the tests
	// only; its private key was thrown away after signing
	old := license.PublicKeys
	license.PublicKeys = append([]string{"Z4F+JgROb9aqvWoHoiAuUDQgz3wu5ahthXBydt4/XbQ="}, old...)
	t.Cleanup(func() { license.PublicKeys = old })
	// form examples submit right after opening the form; the waiting time
	// against bots is checked in TestForms
	oldAge := content_forms.MinAge
	content_forms.MinAge = 0
	t.Cleanup(func() { content_forms.MinAge = oldAge })

	// the administrator approves shell commands (plugins.macros.shell_approvers);
	// a member who may manage them but is not listed is refused
	smtp := newSMTPFake(t) // receives the e-mails of the examples (links in them)
	e := setupWith(t, func(c *config.Config) {
		c.Login.Registration.Enabled = true // people register themselves (with a link by e-mail)
		c.Login.Registration.Domains = []string{"example.com"}
		c.Plugins.Macros.ShellApprovers = []string{"admin@local"}
		c.Mail.From = "wiki@example.com" // the address of S/MIME certificates of the organisation
	})
	t.Setenv("EWIKI_CONFIG", e.cfg) // the command line way (set before the parallel examples)
	r := &exampleRunner{t: t, e: e, byID: byID, seq: new(atomic.Int64), out: &sharedWho{}, smtp: smtp, systems: testSystems(), pluginRoot: t.TempDir(),
		shared: &struct {
			caOnce sync.Once
			ca     *testCA
		}{}}
	// passkeys (setting up, signing): the authenticator of the tests
	r.origin = "http://localhost:" + mustPort(e.srv.URL)
	e.setPluginSettings(t, "mfa.webauthn", map[string]any{"rp_id": "localhost", "origins": []any{r.origin}})
	r.admin = e.client()
	r.admin.login("admin", adminPW)
	var perms struct {
		Authenticated []string `json:"authenticated"`
	}
	r.admin.must("GET", "/api/v1/admin/permissions", nil, &perms, 200)
	r.defaults = map[string]bool{}
	for _, k := range perms.Authenticated {
		r.defaults[k] = true
	}
	ids := make([]string, 0, len(byID))
	for id, a := range byID {
		if len(a.Examples) > 0 {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	// examples that change settings of the whole wiki (or build on such a
	// change) run one after the other first; all others run in parallel,
	// each in its own categories and accounts
	type item struct {
		a  actions.Action
		ex actions.Example
	}
	// examples that need real test systems run where they are started;
	// with EWIKI_TEST_EXAMPLE_SYSTEMS (CI jobs with real systems) only those
	// for the listed systems run, and the systems must be there
	only := map[string]bool{}
	for _, sys := range strings.Split(os.Getenv("EWIKI_TEST_EXAMPLE_SYSTEMS"), ",") {
		if sys = strings.TrimSpace(sys); sys != "" {
			only[sys] = true
			if _, ok := r.systems[sys]; !ok {
				t.Fatalf("test system %s is not running (EWIKI_TEST_%s_…, see actions.TestSystems)", sys, strings.ToUpper(sys))
			}
		}
	}
	var skipped []string
	var serial, parallel []item
	used := map[string]bool{} // test systems of the examples that run
	for _, id := range ids {
		a := byID[id]
		for _, ex := range a.Examples {
			needs := r.needs(a.ID, ex, 0)
			if len(only) > 0 {
				match := false
				for sys := range needs {
					match = match || only[sys]
				}
				if !match {
					continue
				}
			}
			missing := ""
			for sys := range needs {
				if _, ok := r.systems[sys]; !ok {
					missing = sys
				}
			}
			if missing != "" {
				skipped = append(skipped, a.ID+"#"+ex.Name+" ("+missing+")")
				continue
			}
			for sys := range needs {
				used[sys] = true
			}
			if r.global(a.ID, ex, 0) {
				serial = append(serial, item{a, ex})
			} else {
				parallel = append(parallel, item{a, ex})
			}
		}
	}
	if len(skipped) > 0 {
		t.Logf("%d examples need test systems that are not running: %s", len(skipped), strings.Join(skipped, ", "))
	}
	t.Run("serial", func(t *testing.T) {
		for _, it := range serial {
			t.Run(it.a.ID+"#"+it.ex.Name, func(t *testing.T) { r.with(t).run(it.a, it.ex) })
		}
	})
	// the examples with test systems connect plugins to them: the examples
	// that follow run without (built-in search, no office program, …)
	reset := map[string]any{}
	for sys := range used {
		for _, plugin := range systemPlugins[sys] {
			reset[plugin] = map[string]any{}
		}
	}
	if len(reset) > 0 {
		r.admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": reset}, nil, 200)
	}
	t.Run("parallel", func(t *testing.T) {
		for _, it := range parallel {
			t.Run(it.a.ID+"#"+it.ex.Name, func(t *testing.T) {
				t.Parallel()
				r.with(t).run(it.a, it.ex)
			})
		}
	})
}

// globalActions change settings of the whole wiki (mail transport,
// default permissions, languages, license, …): their examples, and those
// that build on them, do not run beside others.
var globalActions = map[string]bool{
	"core.settings.update": true, "core.limits.update": true, "core.permissions.defaults": true,
	"core.mfa.settings.set": true, "core.audit.settings.update": true, "core.webhooks.options": true,
	"core.languages.add": true, "core.languages.remove": true, "core.i18n.set": true, "core.i18n.import": true,
	"core.license.install": true, "core.license.remove": true, "core.license.get": true,
	"core.secrets.rewrap": true, "core.cache.clear": true, "core.retention.run": true, "core.jobs.run": true,
	"core.demo.create": true, "core.demo.remove": true, "core.demo.reset": true, "core.demo.status": true,
	"core.instance.sites.create": true, "core.instance.sites.update": true, "core.instance.sites.delete": true,
	"core.instance.admins.add": true, "core.instance.admins.remove": true,
	"core.mail.status": true, "core.mail.test": true, "core.mail.list": true, "core.mail.get": true,
	"core.mail.retry": true, "core.mail.delete": true,
	"core.plugins.sources.add": true, "core.plugins.sources.remove": true, "core.plugins.sources.external": true, "core.plugins.sources.check": true,
	"core.plugins.install": true, "core.plugins.remove": true,
	"signature.smime.settings.set": true, "core.auth.login_page.update": true, "core.plugins.options": true, "core.plugins.enable": true, "core.plugins.disable": true,
}

// global reports whether an example changes settings of the whole wiki
// or builds on such a change.
func (r *exampleRunner) global(id string, ex actions.Example, depth int) bool {
	if globalActions[id] || depth > 8 {
		return true
	}
	for _, ref := range ex.Given {
		gid, name, _ := strings.Cut(ref, "#")
		for _, gex := range r.byID[gid].Examples {
			if gex.Name == name && r.global(gid, gex, depth+1) {
				return true
			}
		}
	}
	return false
}

// systemPlugins are the plugins the examples of a test system configure.
var systemPlugins = map[string][]string{
	"opensearch": {"search.external"}, "elasticsearch": {"search.external"}, "meilisearch": {"search.external"},
	"ldap": {"auth.ldap"}, "keycloak": {"auth.oidc"},
	"collabora": {"office.collabora"}, "onlyoffice": {"office.onlyoffice"}, "microsoft365": {"office.m365"},
}

// needs returns the test systems an example and its givens require.
func (r *exampleRunner) needs(id string, ex actions.Example, depth int) map[string]bool {
	out := map[string]bool{}
	for _, sys := range ex.Requires {
		out[sys] = true
	}
	if depth > 8 {
		return out
	}
	for _, ref := range ex.Given {
		gid, name, _ := strings.Cut(ref, "#")
		for _, gex := range r.byID[gid].Examples {
			if gex.Name == name {
				for sys := range r.needs(gid, gex, depth+1) {
					out[sys] = true
				}
			}
		}
	}
	return out
}

// testSystems reads the running test systems from the environment
// (EWIKI_TEST_<SYSTEM>_<FIELD>, as printed by the start scripts).
func testSystems() map[string]map[string]string {
	out := map[string]map[string]string{}
	for name, fields := range actions.TestSystems {
		vals := map[string]string{}
		for _, f := range fields {
			if v := os.Getenv("EWIKI_TEST_" + strings.ToUpper(name) + "_" + strings.ToUpper(f)); v != "" {
				vals[f] = v
			}
		}
		if len(vals) == len(fields) {
			out[name] = vals
		}
	}
	return out
}

// with returns a copy of the runner for one (sub)test.
func (r *exampleRunner) with(t *testing.T) *exampleRunner {
	c := *r
	c.t = t
	return &c
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
	t     *testing.T
	e     *env
	byID  map[string]actions.Action
	admin *client
	seq   *atomic.Int64 // shared by the parallel examples: unique names
	out   *sharedWho    // the outsider (one for the run)
	// shared by the parallel examples
	shared *struct {
		caOnce sync.Once
		ca     *testCA
	}
	origin string // of the passkeys (the server is reached as localhost)
	// pluginRoot holds the local plugins of the examples (${local_plugin:ID})
	pluginRoot string
	// systems are the real test systems that are running (field -> value)
	systems map[string]map[string]string
	smtp    *smtpFake
	// permissions every signed-in person has by default
	defaults map[string]bool
}

type sharedWho struct {
	once sync.Once
	who  who
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
	ways := []string{"api", "cli"}
	if a.HasException("WAY_CLI") { // approved: no command line way (TestCLIWithoutCommand)
		ways = ways[:1]
	}
	for _, way := range ways {
		vars := map[string]any{}
		if ex.As == "self" { // a fresh account of its own; its givens run as it too
			vars[selfKey] = r.person()
		}
		r.given(ex.Given, vars, 0)
		path, query, body, up := r.request(a, ex, vars)
		who := r.who(a, ex, path, query, body, way == "api")
		switch ex.As {
		case "self":
			who = vars[selfKey].(*person).who
		case "other":
			who = r.other(vars).who
		}
		critical := a.Safety != nil && a.Safety.RecentSignIn && (ex.As == "" || ex.As == "admin")
		if critical && way == "cli" {
			// --as is no sign-in: refused; the command line on the server may
			if res := r.do(way, a, who, path, query, body, up); res.ok || res.status != 403 {
				t.Fatalf("cli --as on a critical action: %d %s", res.status, res.raw)
			}
			who.server = true
		}
		res := r.do(way, a, who, path, query, body, up)
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
		// (not for permissions every signed-in person has by default)
		if ex.As == "member" && a.Permission != nil && !(a.Permission.Scope == "SCOPE_GLOBAL" && r.defaults[a.Permission.Key]) {
			out := r.outsider()
			if res := r.do(way, a, out, path, query, body, up); res.ok || (res.status != 401 && res.status != 403 && res.status != 404) {
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
		path, query, body, up := r.request(a, *ex, vars)
		w := who{cl: r.admin, name: "admin"}
		switch ex.As {
		case "self":
			p, ok := vars[selfKey].(*person)
			if !ok {
				t.Fatalf("given %s runs as self: the example needs as: self", ref)
			}
			w = p.who
		case "other": // another signed-in person (${other.…})
			w = r.other(vars).who
		}
		res := r.do("api", a, w, path, query, body, up)
		if !res.ok {
			t.Fatalf("given %s: %d %s", ref, res.status, res.raw)
		}
		vars[ref] = res.body
		vars[ref+rawSuffix] = res.raw      // ${given.REF.$raw}
		if res.body == nil && body != "" { // no answer (204): the request names what was done
			var in any
			_ = json.Unmarshal([]byte(body), &in)
			vars[ref] = in
		}
	}
}

// request fills in path, query and body of an example.
func (r *exampleRunner) request(a actions.Action, ex actions.Example, vars map[string]any) (string, map[string]string, string, *upload) {
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
	if ex.BodyText != "" { // not JSON (e.g. JSON Lines of an export)
		body = r.subst(ex.BodyText, vars)
	}
	var up *upload
	if len(ex.Form) > 0 || len(ex.Files) > 0 {
		up = &upload{}
		for _, f := range ex.Form {
			up.form = append(up.form, [2]string{f.Name, r.subst(f.Value, vars)})
		}
		for _, f := range ex.Files {
			data := []byte(f.Text)
			if f.Base64 != "" {
				b, err := base64.StdEncoding.DecodeString(f.Base64)
				if err != nil {
					t.Fatalf("file %s: base64: %v", f.Name, err)
				}
				data = b
			}
			up.files = append(up.files, upFile{field: f.Field, name: f.Name, ctype: f.ContentType, data: data})
		}
	}
	return path, query, body, up
}

// upload is the multipart body of an example.
type upload struct {
	form  [][2]string
	files []upFile
}

type upFile struct {
	field, name, ctype string
	data               []byte
}

// encode builds the multipart body (text fields first, then files).
func (u *upload) encode() ([]byte, string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range u.form {
		_ = w.WriteField(f[0], f[1])
	}
	for _, f := range u.files {
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, f.field, f.name))
		ct := f.ctype
		if ct == "" {
			ct = "application/octet-stream"
		}
		hdr.Set("Content-Type", ct)
		part, _ := w.CreatePart(hdr)
		_, _ = part.Write(f.data)
	}
	_ = w.Close()
	return buf.Bytes(), w.FormDataContentType()
}

// field follows a path (a.b.0.c; -1 is the last element of a list) in
// an answer.
func (r *exampleRunner) field(m string, res any, path string) any {
	v := res
	if path == "" {
		return v
	}
	for _, f := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			v = x[f]
		case []any: // list element by position
			i, err := strconv.Atoi(f)
			if err == nil && i < 0 {
				i += len(x)
			}
			if err != nil || i < 0 || i >= len(x) {
				r.t.Fatalf("%s: no element %s in %v", m, f, res)
			}
			v = x[i]
		default:
			v = nil
		}
	}
	return v
}

// authenticator is the software passkey of the example's own account.
func (r *exampleRunner) authenticator(vars map[string]any) *authenticator {
	p, ok := vars[selfKey].(*person)
	if !ok {
		r.t.Fatal("webauthn placeholders need an example as self")
	}
	if p.key == nil {
		p.key = newAuthenticator(r.t, r.origin)
	}
	return p.key
}

// ca is the certification authority of the tests (one for the run).
func (r *exampleRunner) ca() *testCA {
	r.shared.caOnce.Do(func() { r.shared.ca = newTestCA(r.t) })
	return r.shared.ca
}

// subst fills in ${given.REF.FIELD}, ${self.FIELD}, ${other.FIELD} and
// ${unique}.
func (r *exampleRunner) subst(s string, vars map[string]any) string {
	s = systemRe.ReplaceAllStringFunc(s, func(m string) string {
		sm := systemRe.FindStringSubmatch(m)
		v, ok := r.systems[sm[1]][sm[2]]
		if !ok {
			r.t.Fatalf("%s: test system or field unknown (add it to requires)", m)
		}
		return v
	})
	if strings.Contains(s, "${system.") { // a name or field the pattern does not match
		r.t.Fatalf("placeholder not filled in: %s", s)
	}
	s = personRe.ReplaceAllStringFunc(s, func(m string) string {
		sm := personRe.FindStringSubmatch(m)
		key := map[string]string{"self": selfKey, "other": otherKey}[sm[1]]
		p, ok := vars[key].(*person)
		if !ok { // run on its own (not as a given of an example as self)
			p = r.person()
			vars[key] = p
		}
		switch sm[2] {
		case "id":
			return fmt.Sprint(p.id)
		case "name":
			return p.name
		case "password":
			return p.password
		}
		r.t.Fatalf("%s: id, name or password", m)
		return ""
	})
	s = givenRe.ReplaceAllStringFunc(s, func(m string) string {
		p := givenRe.FindStringSubmatch(m)
		res, ok := vars[p[1]]
		if !ok {
			r.t.Fatalf("%s: %s is not given", m, p[1])
		}
		if p[2] == "$raw" { // the whole answer as it came
			raw, _ := vars[p[1]+rawSuffix].(string)
			return raw
		}
		v := r.field(m, res, p[2])
		switch x := v.(type) {
		case nil:
			r.t.Fatalf("%s: no such field in %v", m, res)
		case float64:
			return fmt.Sprint(int64(x))
		case string:
			return x
		case []any, map[string]any: // a list or object as JSON (outside quotes)
			b, err := json.Marshal(x)
			if err != nil {
				r.t.Fatal(err)
			}
			return string(b)
		}
		return fmt.Sprint(v)
	})
	// ${webauthn.create:REF} and ${webauthn.get:REF.PATH}: the answer of
	// the software authenticator of the example's own account to the
	// options of a given example (setting up a passkey, signing)
	s = webauthnRe.ReplaceAllStringFunc(s, func(m string) string {
		sm := webauthnRe.FindStringSubmatch(m)
		res, ok := vars[sm[2]]
		if !ok {
			r.t.Fatalf("%s: %s is not given", m, sm[2])
		}
		opts, _ := r.field(m, res, sm[3]).(map[string]any)
		if opts == nil {
			r.t.Fatalf("%s: no options", m)
		}
		key := r.authenticator(vars)
		var answer map[string]any
		if sm[1] == "create" {
			answer = key.create(opts)
		} else {
			answer = key.get(opts)
		}
		b, _ := json.Marshal(answer)
		return string(b)
	})
	// ${pkcs12:ADDRESS}: a certificate with its key for ADDRESS from a
	// certification authority of the tests, as PKCS#12 (password "s")
	s = pkcs12Re.ReplaceAllStringFunc(s, func(m string) string {
		c, k := r.ca().issue(r.t, []string{pkcs12Re.FindStringSubmatch(m)[1]}, 365*24*time.Hour, false)
		return r.ca().p12(r.t, c, k, "s")
	})
	// ${smtp_port}: the port of the mail server of the examples
	s = strings.ReplaceAll(s, "${smtp_port}", r.smtp.addr[strings.LastIndex(r.smtp.addr, ":")+1:])
	// ${mail_link:ADDRESS:PARAM}: PARAM of the link in the newest e-mail to
	// ADDRESS (the mail queue is sent first)
	s = mailLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		sm := mailLinkRe.FindStringSubmatch(m)
		re := regexp.MustCompile(`[?&]` + regexp.QuoteMeta(sm[2]) + `=([A-Za-z0-9._~-]+)`)
		// some e-mails leave in the background (password reset: the answer
		// must not reveal whether the account exists): wait for it
		var text []byte
		for deadline := time.Now().Add(10 * time.Second); ; {
			r.e.flushMail(r.t)
			raw := r.smtp.last(strings.ToLower(sm[1]))
			var err error
			if text, err = io.ReadAll(quotedprintable.NewReader(strings.NewReader(raw))); err != nil {
				r.t.Fatalf("%s: %v", m, err)
			}
			if v := re.FindSubmatch(text); v != nil {
				return string(v[1])
			}
			if time.Now().After(deadline) {
				r.t.Fatalf("%s: no link in the e-mail to %s:\n%s", m, sm[1], text)
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	// ${totp:SECRET}: the current code of an authenticator app (after
	// ${given…} is filled in, e.g. ${totp:${given.….secret}})
	s = totpRe.ReplaceAllStringFunc(s, func(m string) string {
		code, err := mfa_totp.TOTPCode(totpRe.FindStringSubmatch(m)[1], time.Now().Unix()/30)
		if err != nil {
			r.t.Fatalf("%s: %v", m, err)
		}
		return code
	})
	for strings.Contains(s, "${ssh_key}") { // a fresh public key (keys are unique)
		pub, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			r.t.Fatal(err)
		}
		var blob []byte
		for _, part := range [][]byte{[]byte("ssh-ed25519"), pub} {
			blob = binary.BigEndian.AppendUint32(blob, uint32(len(part))) // #nosec G115 -- short constant parts
			blob = append(blob, part...)
		}
		s = strings.Replace(s, "${ssh_key}", "ssh-ed25519 "+base64.StdEncoding.EncodeToString(blob)+" example@ewiki", 1)
	}
	for strings.Contains(s, "${unique_lang}") { // ISO 639-2 codes for local use (qaa–qtz)
		n := r.seq.Add(1) % (20 * 26)
		s = strings.Replace(s, "${unique_lang}", string([]byte{'q', byte('a' + n/26), byte('a' + n%26)}), 1)
	}
	for strings.Contains(s, "${unique}") {
		s = strings.Replace(s, "${unique}", fmt.Sprintf("x%d", r.seq.Add(1)), 1)
	}
	// local plugins and their folder (after ${unique}: ids are unique)
	s = localPluginRe.ReplaceAllStringFunc(s, func(m string) string {
		id := localPluginRe.FindStringSubmatch(m)[1]
		dir := filepath.Join(r.pluginRoot, "src"+fmt.Sprint(r.seq.Add(1)), strings.ReplaceAll(id, ".", "-"))
		return localPlugin(r.t, dir, id)
	})
	return strings.ReplaceAll(s, "${plugin_root}", r.pluginRoot)
}

type who struct {
	cl   *client // nil: guest
	name string  // user name for --as ("" = CLI superuser)
	// server: the command line on the server itself (no --as), the way of
	// critical actions (safety.recent_sign_in) besides a fresh sign-in
	server bool
}

// who returns the person an example runs as. A member gets exactly the
// permission of the register entry.
// The command line needs no session (--as): login only for the API.
func (r *exampleRunner) who(a actions.Action, ex actions.Example, path string, query map[string]string, body string, login bool) who {
	switch ex.As {
	case "", "admin":
		return who{cl: r.admin, name: "admin"}
	case "guest":
		return who{cl: r.e.client()}
	case "self", "other":
		return who{} // replaced by the account of the example
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
	if login {
		cl.login(name, "Example-Passw0rd!"+name) // after the grants: the session carries them
	}
	return who{cl: cl, name: name}
}

// person is a signed-in account with no permissions of its own: the
// account of an example as self, or another person (${other.…}).
type person struct {
	who
	id             int64
	name, password string
	key            *authenticator // software passkey (created when first used)
}

const (
	rawSuffix = "\x00raw"  // the whole answer of a given reference
	selfKey   = "\x00self" // keys in vars beside the given references
	otherKey  = "\x00other"
)

// other returns the other person of an example (created when first used).
func (r *exampleRunner) other(vars map[string]any) *person {
	p, ok := vars[otherKey].(*person)
	if !ok {
		p = r.person()
		vars[otherKey] = p
	}
	return p
}

func (r *exampleRunner) person() *person {
	name, cl := r.user()
	pw := "Example-Passw0rd!" + name
	cl.login(name, pw)
	return &person{who: who{cl: cl, name: name}, id: r.userID(name), name: name, password: pw}
}

// outsider is one signed-in account without any permission for the whole
// run (password hashes are what the examples spend most time on).
func (r *exampleRunner) outsider() who {
	r.out.once.Do(func() {
		name, cl := r.user()
		cl.login(name, "Example-Passw0rd!"+name)
		r.out.who = who{cl: cl, name: name}
	})
	return r.out.who
}

func (r *exampleRunner) user() (string, *client) {
	name := fmt.Sprintf("ex%d", r.seq.Add(1))
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
func (r *exampleRunner) do(way string, a actions.Action, w who, path string, query map[string]string, body string, up *upload) exResult {
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
		ctype := "application/json"
		if body != "" {
			rd = strings.NewReader(body)
			if !json.Valid([]byte(body)) {
				ctype = "text/plain; charset=utf-8"
			}
		}
		if up != nil {
			b, ct := up.encode()
			rd, ctype = bytes.NewReader(b), ct
		}
		req, _ := http.NewRequest(a.Method, r.e.srv.URL+u, rd)
		req.Header.Set("Content-Type", ctype)
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
		if json.Valid([]byte(body)) {
			args = append(args, "--json", body)
		} else { // the command line takes other bodies from a file
			p := filepath.Join(r.t.TempDir(), "body")
			if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
				r.t.Fatal(err)
			}
			args = append(args, "--json", "@"+p)
		}
	}
	if up != nil {
		dir := r.t.TempDir()
		for _, f := range up.form {
			args = append(args, "--form", f[0]+"="+f[1])
		}
		for i, f := range up.files {
			sub := filepath.Join(dir, fmt.Sprint(i))
			if err := os.MkdirAll(sub, 0o700); err != nil {
				r.t.Fatal(err)
			}
			p := filepath.Join(sub, f.name)
			if err := os.WriteFile(p, f.data, 0o600); err != nil {
				r.t.Fatal(err)
			}
			args = append(args, "--file", f.field+"=@"+p)
		}
	}
	if w.name != "" && !w.server { // the same account as on the API (also "admin")
		args = append(args, "--as", w.name)
	}
	if !w.server && (w.cl == nil || w.name == "") { // a guest: the command line always has an account
		return r.do("api", a, w, path, query, body, up)
	}
	var stdout, stderr bytes.Buffer
	code := cli.RunAction(a.ID, args, strings.NewReader(""), &stdout, &stderr)
	out := exResult{ok: code == 0, raw: stdout.String() + stderr.String()}
	// the status of the wiki comes last ("message (HTTP 400)"); the message
	// may quote another one (e.g. of an external system)
	if ms := httpErrRe.FindAllStringSubmatch(stderr.String(), -1); ms != nil {
		_, _ = fmt.Sscan(ms[len(ms)-1][1], &out.status)
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
