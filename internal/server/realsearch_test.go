package server_test

// External search against real engines: OpenSearch, Elasticsearch and
// Meilisearch. They complement TestExternalSearch (replicated engines in
// m4_test.go). Start the engines with tests/search/start.sh; without its
// environment the engine is skipped, with EWIKI_TEST_REQUIRE_REAL_SEARCH=1
// (CI job "search") a missing engine fails the test.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type realEngine struct {
	name     string
	urlEnv   string
	settings func(url string) map[string]any
	// wrong are settings with a wrong key or password
	wrong func(url string) map[string]any
}

var realEngines = []realEngine{
	{"opensearch", "EWIKI_TEST_OPENSEARCH_URL",
		func(u string) map[string]any { return map[string]any{"engine": "opensearch", "url": u} },
		nil}, // runs without the security plugin
	{"elasticsearch", "EWIKI_TEST_ELASTICSEARCH_URL",
		func(u string) map[string]any {
			return map[string]any{"engine": "opensearch", "url": u, "username": "elastic", "password": os.Getenv("EWIKI_TEST_ELASTICSEARCH_PASSWORD")}
		},
		func(u string) map[string]any {
			return map[string]any{"engine": "opensearch", "url": u, "username": "elastic", "password": "wrong"}
		}},
	{"meilisearch", "EWIKI_TEST_MEILISEARCH_URL",
		func(u string) map[string]any {
			return map[string]any{"engine": "meilisearch", "url": u, "api_key": os.Getenv("EWIKI_TEST_MEILISEARCH_KEY")}
		},
		func(u string) map[string]any {
			return map[string]any{"engine": "meilisearch", "url": u, "api_key": "wrong"}
		}},
}

func TestRealExternalSearch(t *testing.T) {
	for _, eng := range realEngines {
		t.Run(eng.name, func(t *testing.T) {
			u := os.Getenv(eng.urlEnv)
			if u == "" {
				if os.Getenv("EWIKI_TEST_REQUIRE_REAL_SEARCH") == "1" {
					t.Fatalf("%s not set (start the engines with tests/search/start.sh)", eng.urlEnv)
				}
				t.Skipf("%s not set: real search engines are not running (tests/search/start.sh)", eng.urlEnv)
			}
			e := setup(t)
			admin := e.client()
			admin.login("admin", adminPW)
			settings := eng.settings(u)
			// an own index per run: runs and engines share nothing
			settings["index"] = "ewiki_test_" + randomHex(t, 6)

			if eng.wrong != nil {
				// a wrong key is reported, the built-in search keeps working
				admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"search.external": eng.wrong(u)}}, nil, 200)
				admin.must("POST", "/api/v1/x/search.external/test", nil, nil, 400)
			}

			if eng.name == "elasticsearch" {
				// user name and password work; the run itself uses an API key of the engine
				admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"search.external": settings}}, nil, 200)
				admin.must("POST", "/api/v1/x/search.external/test", nil, nil, 200)
				settings = map[string]any{"engine": "opensearch", "url": u, "index": settings["index"], "api_key": elasticAPIKey(t, u)}
			}

			var root, hidden cat
			admin.must("POST", "/api/v1/categories", map[string]any{"guest_visible": true, "titles": map[string]string{"en": "Docs"}}, &root, 201)
			admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": root.ID, "titles": map[string]string{"en": "Hidden"}}, &hidden, 201)
			var e1, e2 elem
			admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Zebra guide", "tags": []string{"animals"},
				"body": htmlBody("<p>All about zebras. zebra &lt;img src=x onerror=alert(1)&gt; stripes</p>")}, &e1, 201)
			admin.must("POST", "/api/v1/elements", map[string]any{"category_id": hidden.ID, "type": "core.html", "title": "Secret zebra", "body": htmlBody("<p>zebra plans</p>")}, &e2, 201)
			admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"search.external": settings}}, nil, 200)
			run := func() {
				t.Helper()
				if err := e.app.Workers.RunOnce(context.Background(), "search.external.sync"); err != nil {
					t.Fatal(err)
				}
			}
			type status struct {
				Active          bool  `json:"active"`
				EngineDocuments int64 `json:"engine_documents"`
			}
			// waitDocs waits until the engine holds n documents (reindex runs in the background)
			waitDocs := func(n int64) {
				t.Helper()
				var st status
				for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
					run()
					admin.must("GET", "/api/v1/x/search.external/status", nil, &st, 200)
					if st.Active && st.EngineDocuments == n {
						return
					}
				}
				t.Fatalf("engine documents: %+v, want %d", st, n)
			}
			var res struct {
				Engine   string `json:"engine"`
				Elements []struct {
					ElementID int64  `json:"element_id"`
					Snippet   string `json:"snippet"`
				} `json:"elements"`
			}
			search := func(c *client, q string) {
				t.Helper()
				c.must("GET", "/api/v1/search?q="+q, nil, &res, 200)
			}
			waitDocs(2)
			admin.must("POST", "/api/v1/x/search.external/test", nil, nil, 200)

			// the admin finds both, the guest only the visible page: the core filters
			search(admin, "zebra")
			if res.Engine != eng.settings(u)["engine"] || len(res.Elements) != 2 {
				t.Fatalf("admin search: %+v", res)
			}
			guest := e.client()
			search(guest, "zebra")
			if len(res.Elements) != 1 || res.Elements[0].ElementID != e1.ID || !strings.Contains(res.Elements[0].Snippet, "<mark>") {
				t.Fatalf("guest search: %+v", res)
			}
			// text from the page comes back escaped, never as markup
			search(admin, "stripes")
			if len(res.Elements) != 1 || strings.Contains(res.Elements[0].Snippet, "<img") || !strings.Contains(res.Elements[0].Snippet, "&lt;img") {
				t.Fatalf("snippet not escaped: %+v", res)
			}
			// typo tolerance of the engine
			search(admin, "zebar")
			if len(res.Elements) == 0 {
				t.Fatalf("no fuzzy hit: %+v", res)
			}
			admin.must("GET", "/api/v1/search?q=zebra&tags=animals", nil, &res, 200)
			if len(res.Elements) != 1 || res.Elements[0].ElementID != e1.ID {
				t.Fatalf("tag filter: %+v", res)
			}

			// changes follow: new page, edited page, trash
			var e3 elem
			admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Okapi", "body": htmlBody("<p>okapi facts</p>")}, &e3, 201)
			admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", e1.ID), nil, nil, 204)
			waitDocs(2)
			search(admin, "okapi")
			if len(res.Elements) != 1 || res.Elements[0].ElementID != e3.ID {
				t.Fatalf("new page not found: %+v", res)
			}
			search(admin, "stripes")
			if res.Engine == "postgres" || len(res.Elements) != 0 {
				t.Fatalf("trashed page still found: %+v", res)
			}

			// rebuild on demand
			admin.must("POST", "/api/v1/x/search.external/reindex", nil, nil, 204)
			waitDocs(2)
			search(admin, "okapi")
			if len(res.Elements) != 1 {
				t.Fatalf("after reindex: %+v", res)
			}

			// people find what they may see, however much else matches
			crowdedSearch(t, e, admin, settings["engine"].(string), run)

			// engine unreachable: the built-in search answers
			down := eng.settings("http://127.0.0.1:1")
			down["index"] = settings["index"]
			admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"search.external": down}}, nil, 200)
			search(admin, "okapi")
			if res.Engine != "postgres" || len(res.Elements) != 1 {
				t.Fatalf("fallback: %+v", res)
			}
		})
	}
}

// elasticAPIKey creates an API key with the password of "elastic".
func elasticAPIKey(t *testing.T, u string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": "ewiki-test-" + randomHex(t, 4), "expiration": "1h"})
	req, err := http.NewRequest("POST", u+"/_security/api_key", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("elastic", os.Getenv("EWIKI_TEST_ELASTICSEARCH_PASSWORD"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Encoded string `json:"encoded"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&out) != nil || out.Encoded == "" {
		t.Fatalf("creating an API key: HTTP %d", resp.StatusCode)
	}
	return out.Encoded
}
