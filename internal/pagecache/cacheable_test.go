package pagecache

import (
	"net/http/httptest"
	"testing"
)

// Requests with secrets in the URL or for office programs are never cached.
func TestCacheable(t *testing.T) {
	for path, want := range map[string]bool{
		"/api/v1/categories":                                true,
		"/api/v1/elements/3":                                true,
		"/api/v1/wopi/files/7?access_token=x":               false,
		"/api/v1/wopi/files/7":                              false,
		"/api/v1/x/office.onlyoffice/file/7?access_token=x": false,
		"/api/v1/feed.atom?token=x":                         false,
		"/api/v1/me":                                        false,
		"/api/v1/admin/settings":                            false,
	} {
		if got := cacheable(httptest.NewRequest("GET", path, nil)); got != want {
			t.Errorf("cacheable(%s) = %v, want %v", path, got, want)
		}
	}
	if cacheable(httptest.NewRequest("POST", "/api/v1/categories", nil)) {
		t.Error("POST cacheable")
	}
}
