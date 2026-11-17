package server_test

import (
	"strings"
	"testing"
)

// TestSettingsCannotReadTheEnvironment: a value saved through the API with a
// ${VAR} reference used to be expanded – the site name showed the database
// password (or any other variable of the server) to every guest.
func TestSettingsCannotReadTheEnvironment(t *testing.T) {
	t.Setenv("EWIKI_TEST_SERVER_SECRET", "server-secret-value")
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	for name, body := range map[string]map[string]any{
		"site name":      {"ui": map[string]any{"site_name": "${EWIKI_TEST_SERVER_SECRET}"}},
		"plugin setting": {"plugin_settings": map[string]any{"search.external": map[string]any{"engine": "meilisearch", "url": "https://search.example.com", "api_key": "${EWIKI_TEST_SERVER_SECRET}"}}},
	} {
		code, res := admin.rawJSON("PUT", "/api/v1/admin/settings", body)
		if code != 400 || !strings.Contains(errMsg(res), "config.yaml") {
			t.Errorf("%s: %d %v", name, code, res)
		}
	}
	var site map[string]any
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if strings.Contains(site["name"].(string), "server-secret") {
		t.Fatalf("the site shows the environment: %v", site["name"])
	}
	// saving the settings again with the references the operator wrote works
	var cur struct {
		Config map[string]any `json:"config"`
	}
	admin.must("GET", "/api/v1/admin/settings", nil, &cur, 200)
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": cur.Config["ui"]}, nil, 200)
}

func errMsg(body map[string]any) string {
	if e, ok := body["error"].(map[string]any); ok {
		m, _ := e["message"].(string)
		return m
	}
	return ""
}
