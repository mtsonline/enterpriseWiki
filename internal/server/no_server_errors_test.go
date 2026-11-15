package server_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/actions"
)

// Inputs that are wrong in every way a client can get them wrong.
var (
	badPathValues = []string{"0", "-1", "1", "9223372036854775807", "99999999999999999999", "x", "%00", strings.Repeat("a", 300)}
	badQuery      = "?limit=-1&offset=x&q=%25%00%27&lang=zz&id=x&type=x&days=99999999&page=-3&at=x&from=x&to=x&format=x&path=..%2F..%2Fetc&category=9223372036854775807&element=9223372036854775807&calendar=-1&ids=1,x,9223372036854775807"
	badBodies     = []string{"", "{", "null", "[]", `"x"`, `{}`,
		`{"id":"x","name":1,"title":{},"titles":"x","body":"x","email":[],"username":{},"password":1,"category_id":"x","parent_id":-1,` +
			`"limit":"x","permissions":"x","roles":"x","groups":[1.5],"lang":5,"type":7,"key":[],"value":{},"url":"javascript:x","at":"x"}`}
	pathParamRe = regexp.MustCompile(`\{[^}]+\}`)
)

// notProbed are routes the probe does not send wrong input to, with the
// reason. Everything else is probed.
var notProbed = map[string]string{
	"POST /api/v1/admin/reset":                "deletes all content of the site; covered by its own test",
	"GET /api/v1/collab/elements/{id}/{lang}": "WebSocket upgrade, probed in the collaboration tests",
}

// TestNoServerErrors (phase 4, check 3 "no crash"): every route of the
// action register answers wrong input with a 4xx, never with a 5xx – as
// administrator, as signed-in person without permissions and as guest.
func TestNoServerErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	e := setup(t)
	all, err := actions.All()
	if err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "plain", "password": "Plain-Passw0rd!x", "display_name": "Plain"}, nil, 201)
	member := e.client()
	member.login("plain", "Plain-Passw0rd!x")
	guest := e.client()
	// some content, so that "1" finds something
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Probe"}}, &c, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Probe", "body": htmlBody("<p>x</p>")}, nil, 201)

	type finding struct{ route, who, req string }
	var found []finding
	signedIn := func(cl *client) bool {
		var me struct {
			User *struct{} `json:"user"`
		}
		return cl.do("GET", "/api/v1/auth/me", nil, &me) == 200 && me.User != nil
	}
	send := func(cl *client, method, path, body string) (int, string) {
		var r io.Reader
		if body != "" {
			r = bytes.NewReader([]byte(body))
		}
		req, _ := http.NewRequest(method, e.srv.URL+path, r)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "ewiki")
		res, err := cl.http.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return res.StatusCode, string(b)
	}
	probes := 0
	hist := map[string]int{}
	for _, a := range all {
		route := a.Route()
		if _, skip := notProbed[route]; skip {
			continue
		}
		method, path, _ := strings.Cut(route, " ")
		var reqs [][2]string // path, body
		for _, v := range badPathValues {
			if !pathParamRe.MatchString(path) && v != "1" {
				continue
			}
			p := pathParamRe.ReplaceAllString(path, v)
			reqs = append(reqs, [2]string{p, "{}"}, [2]string{p + badQuery, "{}"})
		}
		if method != "GET" && method != "DELETE" {
			for _, b := range badBodies {
				reqs = append(reqs, [2]string{pathParamRe.ReplaceAllString(path, "1"), b})
			}
		}
		for _, who := range []struct {
			name string
			cl   *client
			user string
			pw   string
		}{{"admin", admin, "admin", adminPW}, {"member", member, "plain", "Plain-Passw0rd!x"}, {"guest", guest, "", ""}} {
			for _, rq := range reqs {
				body := rq[1]
				if method == "GET" || method == "DELETE" {
					body = ""
				}
				code, out := send(who.cl, method, rq[0], body)
				probes++
				hist[who.name+" "+fmt.Sprint(code)]++
				if code >= 500 || code == 0 {
					found = append(found, finding{route, who.name, fmt.Sprintf("%s %s %q → %d %s", method, rq[0], body, code, strings.TrimSpace(out))})
				}
			}
			// a route may sign the caller out (logout, own sessions): sign in again
			if who.user != "" && !signedIn(who.cl) {
				who.cl.login(who.user, who.pw)
			}
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].route < found[j].route })
	seen := map[string]bool{}
	for _, f := range found {
		if seen[f.route+f.who] {
			continue
		}
		seen[f.route+f.who] = true
		t.Errorf("%s as %s: %s", f.route, f.who, f.req)
	}
	t.Logf("%d requests to %d routes, %d server errors; answers: %v", probes, len(all), len(found), hist)
}
