package server_test

import (
	"encoding/csv"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestAuditCSVNoFormulas: values others choose (here the user agent of a
// failed sign-in) never become formulas in the CSV export of the audit log.
func TestAuditCSVNoFormulas(t *testing.T) {
	e := setup(t)
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"wrong-password-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "ewiki")
	req.Header.Set("User-Agent", `=HYPERLINK("https://evil.example.com","x")`)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	admin := e.client()
	admin.login("admin", adminPW)
	r2, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/admin/audit/export?format=csv", nil)
	r2.Header.Set("X-Requested-With", "ewiki")
	res, err = admin.http.Do(r2)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(data), "HYPERLINK") {
		t.Fatalf("export: %d (the failed sign-in with its user agent must be in it)\n%s", res.StatusCode, data)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\xef\xbb\xbf"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		for _, v := range row {
			if v != "" && strings.ContainsRune("=+-@", rune(v[0])) {
				t.Fatalf("a cell starts like a formula: %q", v)
			}
		}
	}
}
