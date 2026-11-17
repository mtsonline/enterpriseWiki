package netguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestBlockedAndHint: an internal address is refused unless allowed, and
// the hint names the setting of the calling function (not some other one).
func TestBlockedAndHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer srv.Close()
	_, err := Client([]string{srv.URL}, false, 5*time.Second).Get(srv.URL)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("internal address not blocked: %v", err)
	}
	hinted := Hint(err, "feeds.allow_private in config.yaml")
	if !errors.Is(hinted, ErrBlocked) || !strings.Contains(hinted.Error(), "feeds.allow_private") || strings.Contains(hinted.Error(), "http_allow_private") {
		t.Fatalf("hint: %v", hinted)
	}
	res, err := Client([]string{srv.URL}, true, 5*time.Second).Get(srv.URL)
	if err != nil {
		t.Fatalf("allowed internal address: %v", err)
	}
	_ = res.Body.Close()
	other := errors.New("other")
	if Hint(other, "x") != other || Hint(nil, "x") != nil {
		t.Fatal("Hint changes other errors")
	}
}
