package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHiddenCategorySearch: pages in hidden categories (and below) are not
// found by the general search, only when searching in that category.
func TestHiddenCategorySearch(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var folder, sub struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Hidden stuff"}, "hidden": true}, &folder, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": folder.ID, "titles": map[string]string{"en": "Deeper"}}, &sub, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "lang": "en", "title": "Wombat notes",
		"body": htmlBody(`<p>wombat</p>`)}, nil, 201)
	var res struct {
		Elements   []any `json:"elements"`
		Categories []any `json:"categories"`
	}
	admin.must("GET", "/api/v1/search?q=wombat", nil, &res, 200)
	if len(res.Elements) != 0 {
		t.Fatalf("page of a hidden category found: %+v", res.Elements)
	}
	admin.must("GET", "/api/v1/search?q=Hidden", nil, &res, 200)
	if len(res.Categories) != 0 {
		t.Fatalf("hidden category found: %+v", res.Categories)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/search?q=wombat&category=%d", folder.ID), nil, &res, 200)
	if len(res.Elements) != 1 {
		t.Fatalf("search inside the hidden category: %+v", res.Elements)
	}
}

func TestExternalSearch(t *testing.T) {
	for _, engine := range []string{"meilisearch", "opensearch"} {
		t.Run(engine, func(t *testing.T) {
			e := setup(t)
			admin := e.client()
			admin.login("admin", adminPW)
			f := &fakeEngine{docs: map[string]map[string]any{}, auth: "k3y"}
			var h http.Handler = f.meili()
			settings := map[string]any{"engine": engine, "api_key": "k3y"}
			if engine == "opensearch" {
				h = f.opensearch()
				settings = map[string]any{"engine": engine, "username": "elastic", "password": "k3y"}
			}
			srv := httptest.NewServer(h)
			defer srv.Close()
			settings["url"] = srv.URL
			var root, hidden cat
			admin.must("POST", "/api/v1/categories", map[string]any{"guest_visible": true, "titles": map[string]string{"en": "Docs"}}, &root, 201)
			admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": root.ID, "titles": map[string]string{"en": "Hidden"}}, &hidden, 201)
			var e1, e2 elem
			admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Zebra guide", "tags": []string{"animals"}, "body": htmlBody("<p>All about zebras</p>")}, &e1, 201)
			admin.must("POST", "/api/v1/elements", map[string]any{"category_id": hidden.ID, "type": "core.html", "title": "Secret zebra", "body": htmlBody("<p>zebra plans</p>")}, &e2, 201)
			admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"search.external": settings}}, nil, 200)
			run := func() {
				if err := e.app.Workers.RunOnce(context.Background(), "search.external.sync"); err != nil {
					t.Fatal(err)
				}
			}
			var res struct {
				Engine   string `json:"engine"`
				Elements []struct {
					ElementID int64  `json:"element_id"`
					Snippet   string `json:"snippet"`
				} `json:"elements"`
			}
			run()
			if len(f.docs) != 2 {
				t.Fatalf("index after rebuild: %d docs", len(f.docs))
			}
			var st struct {
				Active          bool  `json:"active"`
				EngineDocuments int64 `json:"engine_documents"`
			}
			admin.must("GET", "/api/v1/x/search.external/status", nil, &st, 200)
			if !st.Active || st.EngineDocuments != 2 {
				t.Fatalf("status %+v", st)
			}
			admin.must("POST", "/api/v1/x/search.external/test", nil, nil, 200)
			// guest only sees the first level (default guest right): the hidden page is filtered by the core
			guest := e.client()
			guest.must("GET", "/api/v1/search?q=zebra", nil, &res, 200)
			if res.Engine != engine || len(res.Elements) != 1 || res.Elements[0].ElementID != e1.ID || !strings.Contains(res.Elements[0].Snippet, "<mark>zebra</mark>") ||
				strings.Contains(res.Elements[0].Snippet, "<b>") {
				t.Fatalf("guest search: %+v", res)
			}
			admin.must("GET", "/api/v1/search?q=zebra&tags=animals", nil, &res, 200)
			if len(res.Elements) != 1 {
				t.Fatalf("tag filter: %+v", res)
			}
			// a wrong index cannot show more: the wiki checks every hit itself
			f.mu.Lock()
			for _, d := range f.docs {
				d["viewers"] = []any{"guest"}
			}
			f.mu.Unlock()
			guest.must("GET", "/api/v1/search?q=zebra", nil, &res, 200)
			if len(res.Elements) != 1 || res.Elements[0].ElementID != e1.ID {
				t.Fatalf("index with wrong viewers: %+v", res)
			}
			// the query carries the viewers of the caller
			f.mu.Lock()
			for _, d := range f.docs {
				d["viewers"] = []any{"user:999999"}
			}
			f.mu.Unlock()
			guest.must("GET", "/api/v1/search?q=zebra", nil, &res, 200)
			if res.Engine != engine || len(res.Elements) != 0 {
				t.Fatalf("viewers not filtered: %+v", res)
			}
			// back to the real viewers
			admin.must("POST", "/api/v1/x/search.external/reindex", nil, nil, 204)
			run()
			// changes follow: new page, trash
			var e3 elem
			admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Okapi", "body": htmlBody("<p>okapi facts</p>")}, &e3, 201)
			admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", e1.ID), nil, nil, 204)
			run()
			admin.must("GET", "/api/v1/search?q=okapi", nil, &res, 200)
			if res.Engine != engine || len(res.Elements) != 1 || res.Elements[0].ElementID != e3.ID {
				t.Fatalf("new page not indexed: %+v", res)
			}
			for _, d := range f.docs {
				if fmt.Sprint(d["element_id"]) == fmt.Sprint(e1.ID) {
					t.Fatal("trashed page still in the engine")
				}
			}
			// engine down: the built-in search answers
			f.mu.Lock()
			f.down = true
			f.mu.Unlock()
			admin.must("GET", "/api/v1/search?q=okapi", nil, &res, 200)
			if res.Engine != "postgres" || len(res.Elements) != 1 {
				t.Fatalf("fallback: %+v", res)
			}
			f.mu.Lock()
			f.down = false
			f.mu.Unlock()
			// rebuild on demand
			admin.must("POST", "/api/v1/x/search.external/reindex", nil, nil, 204)
			time.Sleep(300 * time.Millisecond)
			run()
			if len(f.docs) != 2 {
				t.Fatalf("after reindex: %d docs", len(f.docs))
			}
			// disabling the plugin switches back to PostgreSQL
			admin.must("POST", "/api/v1/admin/plugins/search.external/disable", nil, nil, 204)
			admin.must("GET", "/api/v1/search?q=okapi", nil, &res, 200)
			if res.Engine != "postgres" {
				t.Fatalf("disabled plugin still used: %s", res.Engine)
			}
		})
	}
}

// fakeEngine implements the parts of the Meilisearch and OpenSearch APIs the
// plugin uses (substring search).
type fakeEngine struct {
	mu   sync.Mutex
	docs map[string]map[string]any
	down bool
	auth string
}

func (f *fakeEngine) match(q string, d map[string]any, tags, viewers []string) bool {
	if viewers != nil {
		found := false
		for _, v := range d["viewers"].([]any) {
			found = found || slices.Contains(viewers, v.(string))
		}
		if !found {
			return false
		}
	}
	text := strings.ToLower(fmt.Sprint(d["title"], " ", d["text"], " ", d["tags"]))
	for _, w := range strings.Fields(strings.ToLower(q)) {
		if !strings.Contains(text, w) {
			return false
		}
	}
	for _, t := range tags {
		found := false
		for _, x := range d["tags"].([]any) {
			found = found || x == t
		}
		if !found {
			return false
		}
	}
	return true
}

func (f *fakeEngine) sorted() []map[string]any {
	ids := make([]string, 0, len(f.docs))
	for id := range f.docs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []map[string]any{}
	for _, id := range ids {
		out = append(out, f.docs[id])
	}
	return out
}

func (f *fakeEngine) deleteElements(ids []any) {
	for k, d := range f.docs {
		for _, id := range ids {
			if fmt.Sprint(d["element_id"]) == fmt.Sprint(id) {
				delete(f.docs, k)
			}
		}
	}
}

func (f *fakeEngine) meili() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.down {
			http.Error(w, "down", 503)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+f.auth {
			http.Error(w, `{"message":"invalid key"}`, 403)
			return
		}
		task := func() { w.WriteHeader(202); w.Write([]byte(`{"taskUid":1}`)) }
		p := r.URL.Path
		var body map[string]any
		var arr []map[string]any
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		_ = json.Unmarshal(data, &arr)
		switch {
		case p == "/health" || strings.HasPrefix(p, "/tasks/"):
			w.Write([]byte(`{"status":"succeeded"}`))
		case r.Method == "DELETE" && strings.HasPrefix(p, "/indexes/"):
			f.docs = map[string]map[string]any{}
			task()
		case r.Method == "POST" && p == "/indexes", r.Method == "PATCH":
			task()
		case r.Method == "POST" && strings.HasSuffix(p, "/documents/delete"):
			ids := strings.Trim(strings.TrimPrefix(body["filter"].(string), "element_id IN "), "[]")
			list := []any{}
			for _, s := range strings.Split(ids, ",") {
				list = append(list, strings.TrimSpace(s))
			}
			f.deleteElements(list)
			task()
		case r.Method == "POST" && strings.HasSuffix(p, "/documents"):
			for _, d := range arr {
				f.docs[d["id"].(string)] = d
			}
			task()
		case strings.HasSuffix(p, "/stats"):
			fmt.Fprintf(w, `{"numberOfDocuments":%d}`, len(f.docs))
		case strings.HasSuffix(p, "/search"):
			tags := []string{}
			var viewers []string
			if fs, ok := body["filter"].(string); ok {
				for _, part := range strings.Split(fs, " AND ") {
					if list, ok := strings.CutPrefix(part, "viewers IN ["); ok {
						viewers = []string{}
						for _, v := range strings.Split(strings.TrimSuffix(list, "]"), ", ") {
							viewers = append(viewers, strings.Trim(v, `"`))
						}
						continue
					}
					tags = append(tags, strings.Trim(strings.TrimPrefix(part, "tags = "), `"`))
				}
			}
			hits := []map[string]any{}
			for _, d := range f.sorted() {
				if f.match(body["q"].(string), d, tags, viewers) {
					hits = append(hits, map[string]any{"element_id": d["element_id"], "lang": d["lang"], "title": d["title"], "_rankingScore": 0.9,
						"_formatted": map[string]any{"text": "\x01" + body["q"].(string) + "\x02 <b>bold</b>"}})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"hits": hits})
		default:
			w.Write([]byte(`{}`))
		}
	})
}

func (f *fakeEngine) opensearch() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.down {
			http.Error(w, "down", 503)
			return
		}
		if u, p, _ := r.BasicAuth(); u != "elastic" || p != f.auth {
			http.Error(w, "unauthorized", 401)
			return
		}
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		p := r.URL.Path
		switch {
		case p == "/":
			w.Write([]byte(`{"version":{"number":"2.15.0"}}`))
		case r.Method == "DELETE":
			f.docs = map[string]map[string]any{}
			w.Write([]byte(`{}`))
		case r.Method == "PUT":
			w.Write([]byte(`{"acknowledged":true}`))
		case p == "/_bulk":
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			for i := 0; i+1 < len(lines); i += 2 {
				var meta map[string]map[string]any
				var doc map[string]any
				json.Unmarshal([]byte(lines[i]), &meta)
				json.Unmarshal([]byte(lines[i+1]), &doc)
				f.docs[meta["index"]["_id"].(string)] = doc
			}
			w.Write([]byte(`{"errors":false,"items":[]}`))
		case strings.HasSuffix(p, "/_delete_by_query"):
			q := body["query"].(map[string]any)["terms"].(map[string]any)["element_id"].([]any)
			f.deleteElements(q)
			w.Write([]byte(`{}`))
		case strings.HasSuffix(p, "/_count"):
			fmt.Fprintf(w, `{"count":%d}`, len(f.docs))
		case strings.HasSuffix(p, "/_search"):
			b := body["query"].(map[string]any)["bool"].(map[string]any)
			q := b["must"].([]any)[0].(map[string]any)["multi_match"].(map[string]any)["query"].(string)
			tags := []string{}
			var viewers []string
			for _, x := range b["filter"].([]any) {
				if t, ok := x.(map[string]any)["terms"]; ok {
					viewers = []string{}
					for _, v := range t.(map[string]any)["viewers"].([]any) {
						viewers = append(viewers, v.(string))
					}
					continue
				}
				tags = append(tags, x.(map[string]any)["term"].(map[string]any)["tags"].(string))
			}
			hits := []map[string]any{}
			for _, d := range f.sorted() {
				if f.match(q, d, tags, viewers) {
					hits = append(hits, map[string]any{"_score": 3.2, "_source": map[string]any{"element_id": d["element_id"], "lang": d["lang"], "title": d["title"]},
						"highlight": map[string]any{"text": []string{"…\x01" + q + "\x02…"}}})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"hits": map[string]any{"hits": hits}})
		default:
			w.Write([]byte(`{}`))
		}
	})
}
