package server_test

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/pkg/wopi"
)

// fakeOffice is a WOPI client (like Collabora): it serves the discovery
// and signs requests with its proof key.
type fakeOffice struct {
	srv   *httptest.Server
	key   *rsa.PrivateKey
	saved atomic.Value // OnlyOffice: bytes served at /saved
}

func newFakeOffice(t *testing.T) *fakeOffice {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeOffice{key: k}
	f.saved.Store([]byte("saved by onlyoffice"))
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hosting/discovery":
			b64 := base64.StdEncoding.EncodeToString
			src := f.srv.URL + "/browser/abc/cool.html?"
			fmt.Fprintf(w, `<wopi-discovery><net-zone name="external-http">
<app name="writer"><action ext="docx" name="edit" urlsrc="%[1]s"/><action ext="docx" name="view" urlsrc="%[1]s"/><action ext="pdf" name="view" urlsrc="%[1]s"/></app>
</net-zone><proof-key modulus="%[2]s" exponent="%[3]s"/></wopi-discovery>`, src, b64(k.N.Bytes()), b64(big.NewInt(int64(k.E)).Bytes()))
		case "/healthcheck":
			_, _ = w.Write([]byte("true"))
		case "/saved":
			_, _ = w.Write(f.saved.Load().([]byte))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type wopiCall struct {
	status int
	body   []byte
	header http.Header
}

// call performs a WOPI request like the office server: base is the public
// URL of the wiki (server.base_url), the request goes to the test server.
func (f *fakeOffice) call(t *testing.T, e *env, base, method, path, token string, hdr map[string]string, body []byte, sign bool) wopiCall {
	t.Helper()
	rel := path + "?access_token=" + url.QueryEscape(token)
	req, _ := http.NewRequest(method, e.srv.URL+rel, bytes.NewReader(body))
	if sign {
		ts := wopi.Ticks(time.Now())
		dg := sha256.Sum256(wopi.ProofData(token, base+rel, ts))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, dg[:])
		req.Header.Set("X-WOPI-TimeStamp", strconv.FormatInt(ts, 10))
		req.Header.Set("X-WOPI-Proof", base64.StdEncoding.EncodeToString(sig))
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return wopiCall{res.StatusCode, b, res.Header}
}

type openOpt struct {
	Viewer     string   `json:"viewer"`
	Modes      []string `json:"modes"`
	Restricted bool     `json:"restricted"`
	Default    bool     `json:"default"`
	External   bool     `json:"external"`
}

type openRes struct {
	Viewer     string `json:"viewer"`
	Mode       string `json:"mode"`
	Restricted bool   `json:"restricted"`
	Action     struct {
		Kind   string            `json:"kind"`
		URL    string            `json:"url"`
		Form   map[string]string `json:"form"`
		Script string            `json:"script"`
		Config map[string]any    `json:"config"`
		Origin string            `json:"origin"`
	} `json:"action"`
}

func findOpt(opts []openOpt, viewer string) *openOpt {
	for i := range opts {
		if opts[i].Viewer == viewer {
			return &opts[i]
		}
	}
	return nil
}

func TestOfficeWOPI(t *testing.T) {
	e := setup(t)
	f := newFakeOffice(t)
	base := e.app.Config.Get().Server.BaseURL
	if err := e.app.Config.Update(func(c *config.Config) error {
		if c.Settings == nil {
			c.Settings = map[string]map[string]any{}
		}
		c.Settings["office.collabora"] = map[string]any{"url": f.srv.URL}
		c.Media.Viewer = "office.collabora"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	var cat struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Docs"}, "hidden": true}, &cat, 201)
	var bob, eve struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd!x", "display_name": "Bob"}, &bob, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "eve", "password": "Eve-Passw0rd!x", "display_name": "Eve"}, &eve, 201)
	// bob may edit, eve may only view
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "user", "subject_id": bob.ID, "permissions": 3, "inherit": true}, nil, 201)
	var eveRule struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "user", "subject_id": eve.ID, "permissions": 1, "inherit": true}, &eveRule, 201)
	bc, ec := e.client(), e.client()
	bc.login("bob", "Bob-Passw0rd!x")
	ec.login("eve", "Eve-Passw0rd!x")

	code, doc := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": cat.ID, "title": "Plan"}, "plan.docx", []byte("PK fake docx v1"))
	if code != 201 {
		t.Fatalf("upload docx: %d", code)
	}
	_, secret := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": cat.ID, "title": "Secret", "download": false}, "secret.docx", []byte("PK secret"))
	_, marked := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": cat.ID, "title": "Marked",
		"watermark": map[string]any{"mode": "viewer_view", "user": true}}, "marked.docx", []byte("PK marked"))
	_, other := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": cat.ID}, "other.docx", []byte("PK other"))

	// the page allows the office server as frame source
	_, _, hdr := bc.fetch(t, "/")
	if csp := hdr.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-src 'self' "+f.srv.URL) || !strings.Contains(csp, "form-action 'self' "+f.srv.URL) {
		t.Fatalf("CSP without office origin: %s", csp)
	}

	// options: bob edits (default program), eve views
	var opts []openOpt
	bc.must("GET", fmt.Sprintf("/api/v1/media/%d/open", doc.ID), nil, &opts, 200)
	if o := findOpt(opts, "office.collabora"); o == nil || o.Modes[0] != "edit" || o.Restricted || !o.Default {
		t.Fatalf("bob options %+v", opts)
	}
	ec.must("GET", fmt.Sprintf("/api/v1/media/%d/open", doc.ID), nil, &opts, 200)
	if o := findOpt(opts, "office.collabora"); o == nil || len(o.Modes) != 1 || o.Modes[0] != "view" {
		t.Fatalf("eve options %+v", opts)
	}
	if c := ec.do("POST", fmt.Sprintf("/api/v1/media/%d/open", doc.ID), map[string]string{"viewer": "office.collabora", "mode": "edit"}, nil); c != 403 {
		t.Fatalf("eve opened for editing: %d", c)
	}

	var bo openRes
	bc.must("POST", fmt.Sprintf("/api/v1/media/%d/open", doc.ID), map[string]string{"viewer": "office.collabora"}, &bo, 200)
	tok := bo.Action.Form["access_token"]
	if bo.Mode != "edit" || bo.Action.Kind != "wopi" || tok == "" || !strings.Contains(bo.Action.URL, "WOPISrc="+url.QueryEscape(base+"/api/v1/wopi/files/"+strconv.FormatInt(doc.ID, 10))) {
		t.Fatalf("open: %+v", bo)
	}
	if strings.Contains(bo.Action.URL, tok) {
		t.Fatal("access token in the action URL (must be POSTed)")
	}
	p := fmt.Sprintf("/api/v1/wopi/files/%d", doc.ID)

	// CheckFileInfo: signed → ok; unsigned → refused; wrong token → 401
	r := f.call(t, e, base, "GET", p, tok, nil, nil, true)
	var info map[string]any
	if r.status != 200 || json.Unmarshal(r.body, &info) != nil || info["UserCanWrite"] != true || info["BaseFileName"] != "plan.docx" || info["UserFriendlyName"] != "Bob" {
		t.Fatalf("CheckFileInfo: %d %s", r.status, r.body)
	}
	if r := f.call(t, e, base, "GET", p, tok, nil, nil, false); r.status != 500 {
		t.Fatalf("unsigned request: %d", r.status)
	}
	if r := f.call(t, e, base, "GET", p, tok+"x", nil, nil, true); r.status != 401 {
		t.Fatalf("wrong token: %d", r.status)
	}
	// the token is bound to one file
	if r := f.call(t, e, base, "GET", fmt.Sprintf("/api/v1/wopi/files/%d", other.ID), tok, nil, nil, true); r.status != 401 {
		t.Fatalf("token used for another file: %d", r.status)
	}
	// a browser session alone never gets in
	if c, _, _ := bc.fetch(t, p+"/contents"); c != 401 {
		t.Fatalf("WOPI with session cookie, no token: %d", c)
	}
	if r := f.call(t, e, base, "GET", p+"/contents", tok, nil, nil, true); r.status != 200 || string(r.body) != "PK fake docx v1" {
		t.Fatalf("GetFile: %d %q", r.status, r.body)
	}

	// locks and saving
	if r := f.call(t, e, base, "POST", p, tok, map[string]string{"X-WOPI-Override": "LOCK", "X-WOPI-Lock": "L1"}, nil, true); r.status != 200 {
		t.Fatalf("LOCK: %d", r.status)
	}
	if r := f.call(t, e, base, "POST", p, tok, map[string]string{"X-WOPI-Override": "LOCK", "X-WOPI-Lock": "L2"}, nil, true); r.status != 409 || r.header.Get("X-WOPI-Lock") != "L1" {
		t.Fatalf("conflicting LOCK: %d %q", r.status, r.header.Get("X-WOPI-Lock"))
	}
	if r := f.call(t, e, base, "POST", p+"/contents", tok, map[string]string{"X-WOPI-Override": "PUT", "X-WOPI-Lock": "L2"}, []byte("v2"), true); r.status != 409 {
		t.Fatalf("PutFile with wrong lock: %d", r.status)
	}
	if r := f.call(t, e, base, "POST", p+"/contents", tok, map[string]string{"X-WOPI-Override": "PUT", "X-WOPI-Lock": "L1"}, []byte("PK fake docx v2"), true); r.status != 200 || r.header.Get("X-WOPI-ItemVersion") != "v2" {
		t.Fatalf("PutFile: %d %s", r.status, r.body)
	}
	if r := f.call(t, e, base, "POST", p, tok, map[string]string{"X-WOPI-Override": "UNLOCK", "X-WOPI-Lock": "L1"}, nil, true); r.status != 200 {
		t.Fatalf("UNLOCK: %d", r.status)
	}
	var vers []struct {
		Version int    `json:"version"`
		Note    string `json:"note"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/media/%d/versions", doc.ID), nil, &vers, 200)
	if len(vers) != 2 || vers[0].Note != "Collabora Online" {
		t.Fatalf("versions %+v", vers)
	}
	if r := f.call(t, e, base, "POST", p, tok, map[string]string{"X-WOPI-Override": "PUT_RELATIVE"}, nil, true); r.status != 501 {
		t.Fatalf("PUT_RELATIVE: %d", r.status)
	}

	// eve: view token cannot write or lock
	var eo openRes
	ec.must("POST", fmt.Sprintf("/api/v1/media/%d/open", doc.ID), map[string]string{"viewer": "office.collabora"}, &eo, 200)
	et := eo.Action.Form["access_token"]
	if r := f.call(t, e, base, "POST", p+"/contents", et, map[string]string{"X-WOPI-Override": "PUT"}, []byte("evil"), true); r.status != 401 {
		t.Fatalf("view token wrote: %d", r.status)
	}
	if r := f.call(t, e, base, "POST", p, et, map[string]string{"X-WOPI-Override": "LOCK", "X-WOPI-Lock": "E"}, nil, true); r.status != 401 {
		t.Fatalf("view token locked: %d", r.status)
	}

	// no download: eve views protected (default readonly_without_download), bob too (download off for all but managers)
	ec.must("POST", fmt.Sprintf("/api/v1/media/%d/open", secret.ID), map[string]string{"viewer": "office.collabora"}, &eo, 200)
	if !eo.Restricted || eo.Mode != "view" {
		t.Fatalf("view-only file: %+v", eo)
	}
	r = f.call(t, e, base, "GET", fmt.Sprintf("/api/v1/wopi/files/%d", secret.ID), eo.Action.Form["access_token"], nil, nil, true)
	_ = json.Unmarshal(r.body, &info)
	if info["DisableExport"] != true || info["DisablePrint"] != true || info["DisableCopy"] != true || info["UserCanWrite"] != false {
		t.Fatalf("restricted CheckFileInfo: %s", r.body)
	}
	// with the option switched off, view-only users only get the browser
	_ = e.app.Config.Update(func(c *config.Config) error {
		c.Settings["office.collabora"]["readonly_without_download"] = false
		return nil
	})
	ec.must("GET", fmt.Sprintf("/api/v1/media/%d/open", secret.ID), nil, &opts, 200)
	if findOpt(opts, "office.collabora") != nil {
		t.Fatalf("view-only user may open in office without the option: %+v", opts)
	}
	if r := f.call(t, e, base, "GET", fmt.Sprintf("/api/v1/wopi/files/%d", secret.ID), eo.Action.Form["access_token"], nil, nil, true); r.status != 401 {
		t.Fatalf("token still valid after the option was switched off: %d", r.status)
	}
	_ = e.app.Config.Update(func(c *config.Config) error {
		delete(c.Settings["office.collabora"], "readonly_without_download")
		return nil
	})

	// watermark for viewers: only protected viewing with the viewer's name
	ec.must("POST", fmt.Sprintf("/api/v1/media/%d/open", marked.ID), map[string]string{"viewer": "office.collabora"}, &eo, 200)
	r = f.call(t, e, base, "GET", fmt.Sprintf("/api/v1/wopi/files/%d", marked.ID), eo.Action.Form["access_token"], nil, nil, true)
	_ = json.Unmarshal(r.body, &info)
	if !eo.Restricted || !strings.Contains(fmt.Sprint(info["WatermarkText"]), "Eve") {
		t.Fatalf("watermarked file: %+v %s", eo, r.body)
	}
	if c := bc.do("POST", fmt.Sprintf("/api/v1/media/%d/open", marked.ID), map[string]string{"viewer": "office.collabora", "mode": "edit"}, nil); c != 403 {
		t.Fatalf("editing a watermarked original without manage: %d", c)
	}

	// rights withdrawn: the token stops working at once
	ec.must("POST", fmt.Sprintf("/api/v1/media/%d/open", doc.ID), map[string]string{"viewer": "office.collabora"}, &eo, 200)
	et = eo.Action.Form["access_token"]
	admin.must("DELETE", fmt.Sprintf("/api/v1/acl/%d", eveRule.ID), nil, nil, 204)
	if r := f.call(t, e, base, "GET", p, et, nil, nil, true); r.status != 401 {
		t.Fatalf("token after the rights were withdrawn: %d", r.status)
	}

	// API keys and the bare CLI user cannot issue office tokens
	var key struct {
		Token string `json:"token"`
	}
	bc.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "k", "permissions": 1, "expires_at": time.Now().Add(time.Hour)}, &key, 201)
	kc := e.client()
	kc.token = key.Token
	if c := kc.do("POST", fmt.Sprintf("/api/v1/media/%d/open", doc.ID), map[string]string{"viewer": "office.collabora"}, nil); c != 403 {
		t.Fatalf("API key opened in office: %d", c)
	}
	if code, _ := runCLI(t, e.cfg, "media", "open", strconv.FormatInt(doc.ID, 10), "--viewer", "office.collabora"); code == 0 {
		t.Fatal("CLI without --as issued an office token")
	}
	if code, out := runCLI(t, e.cfg, "media", "open", strconv.FormatInt(doc.ID, 10), "--viewer", "office.collabora", "--as", "bob"); code != 0 || !strings.Contains(out, "access_token") {
		t.Fatalf("CLI --as bob: %d %s", code, out)
	}

	// plugin switched off: tokens stop working, the program disappears
	bc.must("POST", fmt.Sprintf("/api/v1/media/%d/open", doc.ID), map[string]string{"viewer": "office.collabora"}, &bo, 200)
	_ = e.app.Config.Update(func(c *config.Config) error {
		c.Plugins.Disabled = append(c.Plugins.Disabled, "office.collabora")
		return nil
	})
	if r := f.call(t, e, base, "GET", p, bo.Action.Form["access_token"], nil, nil, true); r.status != 401 {
		t.Fatalf("token after the plugin was switched off: %d", r.status)
	}
}

func hs256(t *testing.T, claims any, secret string) string {
	t.Helper()
	enc := base64.RawURLEncoding
	b, _ := json.Marshal(claims)
	msg := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + enc.EncodeToString(b)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	return msg + "." + enc.EncodeToString(mac.Sum(nil))
}

func TestOfficeOnlyOfficeAPI(t *testing.T) {
	e := setup(t)
	f := newFakeOffice(t)
	const secret = "jwt-secret-for-tests-0123456789"
	if err := e.app.Config.Update(func(c *config.Config) error {
		if c.Settings == nil {
			c.Settings = map[string]map[string]any{}
		}
		c.Settings["office.onlyoffice"] = map[string]any{"url": f.srv.URL, "jwt_secret": secret}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	var cat struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Docs"}}, &cat, 201)
	_, doc := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": cat.ID}, "sheet.xlsx", []byte("PK sheet v1"))

	_, _, hdr := admin.fetch(t, "/")
	if csp := hdr.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self' "+f.srv.URL) {
		t.Fatalf("CSP without the Document Server as script source: %s", csp)
	}
	var o openRes
	admin.must("POST", fmt.Sprintf("/api/v1/media/%d/open", doc.ID), map[string]string{"viewer": "office.onlyoffice"}, &o, 200)
	if o.Action.Kind != "onlyoffice" || o.Action.Script != f.srv.URL+"/web-apps/apps/api/documents/api.js" {
		t.Fatalf("open: %+v", o)
	}
	document := o.Action.Config["document"].(map[string]any)
	fileURL := document["url"].(string)
	cbURL := o.Action.Config["editorConfig"].(map[string]any)["callbackUrl"].(string)
	if tok, _ := o.Action.Config["token"].(string); len(strings.Split(tok, ".")) != 3 {
		t.Fatalf("config not signed: %v", o.Action.Config["token"])
	}
	local := func(u string) string { // base_url → test server
		p, _ := url.Parse(u)
		return e.srv.URL + p.RequestURI()
	}
	get := func(auth string) (int, string) {
		req, _ := http.NewRequest("GET", local(fileURL), nil)
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if c, _ := get(""); c != 401 {
		t.Fatalf("file without JWT: %d", c)
	}
	if c, _ := get(hs256(t, map[string]any{"x": 1}, "wrong")); c != 401 {
		t.Fatalf("file with a foreign JWT: %d", c)
	}
	if c, b := get(hs256(t, map[string]any{"payload": map[string]any{"url": fileURL}}, secret)); c != 200 || b != "PK sheet v1" {
		t.Fatalf("file: %d %q", c, b)
	}
	callback := func(body map[string]any, signed bool) (int, string) {
		if signed {
			body["token"] = hs256(t, body, secret)
		}
		b, _ := json.Marshal(body)
		res, err := http.Post(local(cbURL), "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		rb, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(rb)
	}
	key := document["key"].(string)
	if c, _ := callback(map[string]any{"key": key, "status": 2, "url": f.srv.URL + "/saved"}, false); c != 401 {
		t.Fatalf("unsigned callback: %d", c)
	}
	// downloads only from the Document Server (no SSRF)
	if c, b := callback(map[string]any{"key": key, "status": 2, "url": "http://169.254.169.254/latest/meta-data"}, true); c != 200 || !strings.Contains(b, `"error":1`) {
		t.Fatalf("callback with a foreign URL: %d %s", c, b)
	}
	if c, b := callback(map[string]any{"key": key, "status": 1}, true); c != 200 || !strings.Contains(b, `"error":0`) {
		t.Fatalf("status callback: %d %s", c, b)
	}
	if c, b := callback(map[string]any{"key": key, "status": 2, "url": f.srv.URL + "/saved"}, true); c != 200 || !strings.Contains(b, `"error":0`) {
		t.Fatalf("save callback: %d %s", c, b)
	}
	var m mediumResp
	admin.must("GET", fmt.Sprintf("/api/v1/media/%d", doc.ID), nil, &m, 200)
	if m.Version != 2 {
		t.Fatalf("version after save: %d", m.Version)
	}
	// without a secret the API mode is off
	_ = e.app.Config.Update(func(c *config.Config) error {
		delete(c.Settings["office.onlyoffice"], "jwt_secret")
		return nil
	})
	if c := admin.do("POST", fmt.Sprintf("/api/v1/media/%d/open", doc.ID), map[string]string{"viewer": "office.onlyoffice"}, nil); c != 403 {
		t.Fatalf("open without JWT secret: %d", c)
	}
}
