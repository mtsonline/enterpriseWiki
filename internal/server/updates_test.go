package server_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

func TestUpdateCheck(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/mtsonline/enterpriseWiki/releases/latest":
			w.Write([]byte(`{"tag_name":"v99.0.0","html_url":"https://github.com/mtsonline/enterpriseWiki/releases/tag/v99.0.0","body":"Big release"}`))
		case "/repos/acme/ewiki-plugin-foo/releases/latest":
			http.NotFound(w, r)
		case "/repos/acme/ewiki-plugin-foo/tags":
			w.Write([]byte(`[{"name":"v1.2.0"},{"name":"v1.10.0"},{"name":"nightly"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Plugins.Updates.APIURL = gh.URL
		c.Plugins.Catalog = []config.CatalogEntry{{ID: "example.notice", Name: "Notice", Repo: "github.com/acme/ewiki-plugin-foo"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Items []struct {
			ID        string `json:"id"`
			Latest    string `json:"latest"`
			Available bool   `json:"available"`
			Error     string `json:"error"`
		} `json:"items"`
	}
	admin.must("GET", "/api/v1/admin/updates?force=1", nil, &rep, 200)
	got := map[string]string{}
	for _, it := range rep.Items {
		got[it.ID] = fmt.Sprintf("%s/%v/%s", it.Latest, it.Available, it.Error)
	}
	if got["core"] != "99.0.0/true/" || got["example.notice"] != "1.10.0/true/" {
		t.Fatalf("update report: %+v", got)
	}
	if code := admin.do("POST", "/api/v1/admin/updates/core", nil, nil); code != 400 {
		t.Fatalf("core update through the API: %d", code)
	}
}
