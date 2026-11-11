package server_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

type previewResp struct {
	Outcome      string   `json:"outcome"`
	User         string   `json:"user"`
	Groups       []string `json:"groups"`
	NewGroups    []string `json:"new_groups"`
	Roles        []string `json:"roles"`
	MissingRoles []string `json:"missing_roles"`
	JIT          bool     `json:"jit"`
}

func TestProviderInstances(t *testing.T) {
	e := setup(t)
	staff, partner := newFakeOIDC(t), newFakeOIDC(t)
	e.setPluginSettings(t, "auth.oidc", map[string]any{"name": "Staff", "issuer": staff.srv.URL, "client_id": "wiki", "client_secret": "s3cret",
		"instances": []any{
			map[string]any{"id": "partner", "name": "Partner", "issuer": partner.srv.URL, "client_id": "wiki", "client_secret": "p4rtner",
				"group_mapping": map[string]any{"sales": "Partners"}, "keep_unmapped_groups": false},
			map[string]any{"id": "Bad ID", "issuer": partner.srv.URL, "client_id": "wiki"}, // ignored
		}})
	var providers []map[string]string
	e.client().must("GET", "/api/v1/auth/providers", nil, &providers, 200)
	names := map[string]string{}
	for _, p := range providers {
		if p["kind"] != "passwordless" {
			names[p["id"]] = p["name"]
		}
	}
	if names["oidc"] != "Staff" || names["oidc-partner"] != "Partner" || len(names) != 3 {
		t.Fatalf("providers %v", providers)
	}
	// each instance signs in on its own: own provider id, own mapping
	a := e.client()
	if loc := partner.loginAt(t, e, a, "oidc-partner"); loc != "/" {
		t.Fatalf("partner sign-in: %s", loc)
	}
	var me meResp
	a.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User == nil || me.User.Provider != "oidc-partner" {
		t.Fatalf("partner user %+v", me.User)
	}
	if g := groupNames(t, e, me.User.Groups); len(g) != 1 || g[0] != "Partners" {
		t.Fatalf("partner groups %v", g)
	}
	b := e.client()
	staff.login(t, e, b)
	b.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User.Provider != "oidc" {
		t.Fatalf("staff user %+v", me.User)
	}

	admin := e.client()
	admin.login("admin", adminPW)
	// preview: existing, new (JIT), refused without JIT, mapping
	var pv previewResp
	admin.must("POST", "/api/v1/admin/auth/providers/oidc-partner/preview", map[string]any{"subject": "u-123", "username": "carol"}, &pv, 200)
	if pv.Outcome != "existing" || pv.User != "carol@oidc-partner" {
		t.Fatalf("preview existing %+v", pv)
	}
	admin.must("POST", "/api/v1/admin/auth/providers/oidc-partner/preview", map[string]any{"username": "dan", "groups": []string{"sales", "other"}}, &pv, 200)
	if pv.Outcome != "create" || len(pv.Groups) != 1 || pv.Groups[0] != "Partners" || len(pv.NewGroups) != 0 {
		t.Fatalf("preview new %+v", pv)
	}
	if got := admin.do("POST", "/api/v1/admin/auth/providers/oidc-nope/preview", map[string]any{"username": "x"}, nil); got != 404 {
		t.Fatalf("unknown provider: %d", got)
	}
	if got := a.do("POST", "/api/v1/admin/auth/providers/oidc/preview", map[string]any{"username": "x"}, nil); got != 403 {
		t.Fatalf("preview by a user: %d", got)
	}
	if got := admin.do("POST", "/api/v1/x/auth.ldap/test", map[string]string{"instance": "ldap-nope", "username": "x", "password": "y"}, nil); got != 404 {
		t.Fatalf("ldap test of an unknown instance: %d", got)
	}
	admin.must("GET", "/api/v1/x/auth.oidc/check?instance=oidc-partner", nil, nil, 200)
	if got := admin.do("GET", "/api/v1/x/auth.oidc/check?instance=oidc-nope", nil, nil); got != 404 {
		t.Fatalf("check unknown instance: %d", got)
	}

	// settings API: instance secrets are masked and kept when the mask comes back
	var st struct {
		Config struct {
			Settings map[string]map[string]any `json:"plugin_settings"`
		} `json:"config"`
	}
	admin.must("GET", "/api/v1/admin/settings", nil, &st, 200)
	oidc := st.Config.Settings["auth.oidc"]
	inst := oidc["instances"].([]any)[0].(map[string]any)
	if inst["client_secret"] != "********" || oidc["client_secret"] != "********" {
		t.Fatalf("secrets not masked: %v", oidc)
	}
	// JIT off for the partner (and the invalid entry dropped)
	inst["jit"] = false
	oidc["instances"] = []any{inst}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"auth.oidc": oidc}}, nil, 200)
	var secret string
	_ = e.app.Config.Update(func(c *config.Config) error {
		secret, _ = c.Settings["auth.oidc"]["instances"].([]any)[0].(map[string]any)["client_secret"].(string)
		return nil
	})
	if secret != "p4rtner" {
		t.Fatalf("instance secret after save: %q", secret)
	}
	admin.must("POST", "/api/v1/admin/auth/providers/oidc-partner/preview", map[string]any{"username": "dan"}, &pv, 200)
	if pv.Outcome != "refused" || pv.JIT {
		t.Fatalf("preview without JIT %+v", pv)
	}
	// known people still get in, new ones do not
	if loc := partner.loginAt(t, e, e.client(), "oidc-partner"); loc != "/" {
		t.Fatalf("known person without JIT: %s", loc)
	}
	for _, bad := range []any{
		[]any{map[string]any{"id": "UPPER"}},
		[]any{map[string]any{"id": "x"}, map[string]any{"id": "x"}},
		[]any{"not a map"},
		"not a list",
	} {
		oidc["instances"] = bad
		if got := admin.do("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"auth.oidc": oidc}}, nil); got != 400 {
			t.Errorf("instances %v: %d", bad, got)
		}
	}

	// SAML: an instance has its own entity, ACS and SLO addresses
	newFakeSAML(t, e, map[string]any{"instances": []any{map[string]any{"id": "uni", "mode": "saml", "idp_metadata": "<x/>"}}})
	res, err := http.Get(e.srv.URL + "/api/v1/x/auth.shib/metadata?instance=shib-uni")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("instance metadata: %v %d", err, res.StatusCode)
	}
	md, _ := io.ReadAll(res.Body)
	res.Body.Close()
	for _, want := range []string{"/api/v1/auth/sso/shib-uni/callback", "/api/v1/auth/sso/shib-uni/logout", "metadata?instance=shib-uni"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("instance metadata lacks %s", want)
		}
	}
}
