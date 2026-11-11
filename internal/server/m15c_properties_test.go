package server_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type propDef struct {
	ID int64 `json:"id"`
}

type propItems struct {
	Items []struct {
		Key     string          `json:"key"`
		Type    string          `json:"type"`
		Label   string          `json:"label"`
		Value   json.RawMessage `json:"value"`
		CanEdit bool            `json:"can_edit"`
	} `json:"items"`
	CanAddFree bool `json:"can_add_free"`
}

func (p propItems) value(key string) string {
	for _, i := range p.Items {
		if i.Key == key {
			return string(i.Value)
		}
	}
	return ""
}

func (p propItems) has(key string) bool {
	for _, i := range p.Items {
		if i.Key == key {
			return true
		}
	}
	return false
}

type propResult struct {
	Rows []struct {
		ID     int64                      `json:"id"`
		Title  string                     `json:"title"`
		Values map[string]json.RawMessage `json:"values"`
	} `json:"rows"`
	Groups []struct {
		Key   json.RawMessage `json:"key"`
		Value float64         `json:"value"`
		Count int             `json:"count"`
	} `json:"groups"`
	Total int `json:"total"`
}

func (r propResult) titles() string {
	var out []string
	for _, x := range r.Rows {
		out = append(out, x.Title)
	}
	return strings.Join(out, ",")
}

// TestProperties covers definitions (global, per category, inherited),
// types and validation, free properties, computed values, saving with the
// element and drafts, restricted visibility, queries and the report.
func TestProperties(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var c, sub cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Projects"}}, &c, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": c.ID, "titles": map[string]string{"en": "Active"}}, &sub, 201)
	def := func(body map[string]any, want int) propDef {
		var d propDef
		admin.must("POST", "/api/v1/x/core.properties/defs", body, &d, want)
		return d
	}
	choices := []map[string]any{{"value": "Open", "color": "blue"}, {"value": "Review", "color": "yellow"}, {"value": "Done", "color": "green"}}
	def(map[string]any{"key": "status", "type": "status", "labels": map[string]string{"en": "Status", "de": "Status"}, "options": map[string]any{"choices": choices}, "default": "Open"}, 201)
	def(map[string]any{"category_id": c.ID, "key": "budget", "type": "currency", "labels": map[string]string{"en": "Budget"}, "options": map[string]any{"currency": "EUR", "min": 0}}, 201)
	def(map[string]any{"category_id": c.ID, "key": "due", "type": "date", "labels": map[string]string{"en": "Due"}}, 201)
	def(map[string]any{"category_id": c.ID, "key": "owner", "type": "user", "labels": map[string]string{"en": "Owner"}}, 201)
	def(map[string]any{"category_id": c.ID, "key": "ref", "type": "autonumber", "options": map[string]any{"prefix": "REQ-", "digits": 4}}, 201)
	def(map[string]any{"category_id": c.ID, "key": "double", "type": "formula", "options": map[string]any{"formula": "budget * 2 + (due - due)"}}, 201)
	// invalid definitions
	def(map[string]any{"key": "Bad Key", "type": "text"}, 400)
	def(map[string]any{"key": "title", "type": "text"}, 400)
	def(map[string]any{"key": "x", "type": "nope"}, 400)
	def(map[string]any{"key": "x", "type": "text", "options": map[string]any{"pattern": "("}}, 400)
	def(map[string]any{"key": "x", "type": "formula", "options": map[string]any{"formula": "budget +"}}, 400)
	def(map[string]any{"key": "x", "type": "formula", "options": map[string]any{"formula": "system(1)"}}, 400)
	def(map[string]any{"key": "status", "type": "text"}, 409)

	// a role whose members see the property "secret"
	var role struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Controllers"}, &role, 200)
	def(map[string]any{"category_id": c.ID, "key": "secret", "type": "text", "view_subjects": []map[string]any{{"type": "role", "id": role.ID}}}, 201)

	// saving with the element (field core.properties); autonumber and formula
	var p1, p2, p3 elem
	fields := func(values map[string]any) map[string]any {
		return map[string]any{"core.properties": map[string]any{"values": values}}
	}
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Alpha", "body": htmlBody("<p>a</p>"),
		"fields": fields(map[string]any{"budget": 300, "due": "2030-01-01", "status": "review", "secret": "s3cret <b>"})}, &p1, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "Beta", "body": htmlBody("<p>b</p>"),
		"fields": fields(map[string]any{"budget": "20,5", "due": time.Now().AddDate(0, 0, 10).Format("2006-01-02")})}, &p2, 201)
	// created without the field: default and number anyway
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Gamma", "body": htmlBody("<p>g</p>")}, &p3, 201)
	get := func(cl *client, id int64) propItems {
		var p propItems
		cl.must("GET", fmt.Sprintf("/api/v1/x/core.properties/elements/%d", id), nil, &p, 200)
		return p
	}
	a := get(admin, p1.ID)
	if a.value("status") != `"Review"` || a.value("budget") != "300" || a.value("ref") != `"REQ-0001"` || a.value("double") != "600" {
		t.Fatalf("alpha: %+v", a)
	}
	if b := get(admin, p2.ID); b.value("budget") != "20.5" || b.value("status") != `"Open"` || b.value("ref") != `"REQ-0002"` || b.value("double") != "41" {
		t.Fatalf("beta: %+v", b)
	}
	if g := get(admin, p3.ID); g.value("status") != `"Open"` || g.value("ref") != `"REQ-0003"` {
		t.Fatalf("gamma: %+v", g)
	}
	// validation
	bad := func(values map[string]any) {
		t.Helper()
		if code := admin.do("PUT", fmt.Sprintf("/api/v1/x/core.properties/elements/%d", p1.ID), map[string]any{"values": values}, nil); code != 400 {
			t.Fatalf("accepted %v: %d", values, code)
		}
	}
	bad(map[string]any{"due": "tomorrow"})
	bad(map[string]any{"status": "Unknown"})
	bad(map[string]any{"budget": -5})
	bad(map[string]any{"ref": "REQ-9"})
	bad(map[string]any{"tags": []string{"x"}})
	bad(map[string]any{"Owner Name": "x"})
	bad(map[string]any{"link": "javascript:alert(1)"}) // a free one needs a type ...
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/x/core.properties/elements/%d", p1.ID), map[string]any{"values": map[string]any{"link": "javascript:alert(1)"},
		"free": map[string]any{"link": map[string]any{"type": "url"}}}, nil); code != 400 {
		t.Fatalf("javascript link: %d", code)
	}
	// a free property; the formula follows changes
	admin.must("PUT", fmt.Sprintf("/api/v1/x/core.properties/elements/%d", p1.ID), map[string]any{"values": map[string]any{"room": "B 2.14", "budget": 400},
		"free": map[string]any{"room": map[string]any{"type": "text", "label": "Room"}}}, nil, 200)
	if a = get(admin, p1.ID); a.value("room") != `"B 2.14"` || a.value("double") != "800" || a.value("ref") != `"REQ-0001"` {
		t.Fatalf("free and formula: %+v", a)
	}
	// free properties can be forbidden per category
	admin.must("PUT", fmt.Sprintf("/api/v1/x/core.properties/settings/%d", sub.ID), map[string]any{"free": "deny"}, nil, 200)
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/x/core.properties/elements/%d", p2.ID), map[string]any{"values": map[string]any{"room": "x"},
		"free": map[string]any{"room": map[string]any{"type": "text"}}}, nil); code != 403 {
		t.Fatalf("free property where forbidden: %d", code)
	}

	// drafts keep the fields; publishing stores them
	var d struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/drafts", map[string]any{"element_id": p3.ID, "lang": "en", "title": "Gamma", "body": htmlBody("<p>g2</p>"), "base_rev": 1,
		"fields": fields(map[string]any{"budget": 70})}, &d, 201)
	if g := get(admin, p3.ID); g.has("budget") && g.value("budget") != "null" && g.value("budget") != "" {
		t.Fatalf("draft changed the element: %+v", g)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/drafts/%d/publish", d.ID), map[string]any{}, nil, 200)
	if g := get(admin, p3.ID); g.value("budget") != "70" || g.value("double") != "140" {
		t.Fatalf("published draft: %+v", g)
	}
	// unknown fields are refused (no silent loss)
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/elements/%d", p3.ID), map[string]any{"lang": "en", "title": "Gamma", "body": htmlBody("<p>g</p>"),
		"fields": map[string]any{"nope": 1}}, nil); code != 400 {
		t.Fatalf("unknown field: %d", code)
	}

	// queries
	query := func(cl *client, q map[string]any, want int) propResult {
		var r propResult
		cl.must("POST", "/api/v1/x/core.properties/query", q, &r, want)
		return r
	}
	src := []map[string]any{{"kind": "category", "id": c.ID, "sub": true}}
	if r := query(admin, map[string]any{"sources": src, "sort": []map[string]any{{"key": "budget"}}}, 200); r.titles() != "Beta,Gamma,Alpha" {
		t.Fatalf("sort by budget: %s", r.titles())
	}
	if r := query(admin, map[string]any{"sources": src, "filters": []map[string]any{{"key": "due", "op": "le", "value": "today+30d"}, {"key": "due", "op": "ge", "value": "today"}}}, 200); r.titles() != "Beta" {
		t.Fatalf("relative dates: %s", r.titles())
	}
	if r := query(admin, map[string]any{"sources": src, "filters": []map[string]any{{"key": "status", "op": "in", "value": []string{"open"}}}, "sort": []map[string]any{{"key": "title", "desc": true}}}, 200); r.titles() != "Gamma,Beta" {
		t.Fatalf("status in: %s", r.titles())
	}
	if r := query(admin, map[string]any{"sources": src, "filters": []map[string]any{{"key": "status", "op": "ne", "value": "Open"}}}, 200); r.titles() != "Alpha" {
		t.Fatalf("status ne: %s", r.titles())
	}
	if r := query(admin, map[string]any{"sources": src, "filters": []map[string]any{{"key": "room", "op": "empty"}}}, 200); r.Total != 2 {
		t.Fatalf("empty: %+v", r)
	}
	g := query(admin, map[string]any{"sources": src, "group": "status", "agg": "sum", "agg_key": "budget"}, 200)
	sums := map[string]float64{}
	for _, x := range g.Groups {
		sums[string(x.Key)] = x.Value
	}
	if sums[`"Open"`] != 90.5 || sums[`"Review"`] != 400 {
		t.Fatalf("groups: %+v", g)
	}
	query(admin, map[string]any{"filters": []map[string]any{{"key": "budget", "op": "contains", "value": "x"}}}, 400)
	query(admin, map[string]any{"filters": []map[string]any{{"key": "x'; DROP TABLE elements; --", "op": "eq", "value": "1"}}}, 400)

	// readers: no hidden property, not in filters, sorting or sums
	pw := "Passw0rd!Passw0rd"
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "reader", "password": pw, "display_name": "Reader", "roles": []int64{}}, nil, 201)
	r := e.client()
	r.login("reader", pw)
	ra := get(r, p1.ID)
	if ra.has("secret") || ra.value("budget") != "400" || ra.CanAddFree {
		t.Fatalf("reader sees: %+v", ra)
	}
	for _, it := range ra.Items {
		if it.CanEdit {
			t.Fatalf("reader may edit %s", it.Key)
		}
	}
	query(r, map[string]any{"sources": src, "filters": []map[string]any{{"key": "secret", "op": "notempty"}}}, 400)
	query(r, map[string]any{"sources": src, "sort": []map[string]any{{"key": "secret"}}}, 400)
	query(r, map[string]any{"sources": src, "group": "secret"}, 400)
	if rr := query(r, map[string]any{"sources": src}, 200); strings.Contains(fmt.Sprint(rr.Rows), "s3cret") {
		t.Fatal("hidden value in rows")
	}
	if code := r.do("PUT", fmt.Sprintf("/api/v1/x/core.properties/elements/%d", p1.ID), map[string]any{"values": map[string]any{"budget": 1}}, nil); code != 403 {
		t.Fatalf("reader changed a property: %d", code)
	}
	if code := r.do("POST", "/api/v1/x/core.properties/defs", map[string]any{"category_id": c.ID, "key": "y", "type": "text"}, nil); code != 403 {
		t.Fatalf("reader defined a property: %d", code)
	}
	var hist []struct{ Key string }
	r.must("GET", fmt.Sprintf("/api/v1/x/core.properties/history/%d", p1.ID), nil, &hist, 200)
	for _, x := range hist {
		if x.Key == "secret" {
			t.Fatal("hidden property in the history")
		}
	}
	// members of the role see it
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ctrl", "password": pw, "display_name": "Ctrl", "roles": []int64{role.ID}}, nil, 201)
	cc := e.client()
	cc.login("ctrl", pw)
	if x := get(cc, p1.ID); !strings.Contains(x.value("secret"), "s3cret") {
		t.Fatalf("role member: %+v", x)
	}
	// guests see nothing of a category without guest access
	if code := e.client().do("POST", "/api/v1/x/core.properties/query", map[string]any{"sources": src}, nil); code == 200 {
		var gr propResult
		e.client().must("POST", "/api/v1/x/core.properties/query", map[string]any{"sources": src}, &gr, 200)
		if gr.Total != 0 {
			t.Fatalf("guest rows: %+v", gr)
		}
	}

	// report macro: escaped, with the rights of the reader
	var rep elem
	body := `<div data-macro="properties" data-params='{"columns":"status,budget,secret","sort":"-budget","filter":"budget >= 1"}'></div>`
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Report", "body": htmlBody(body)}, &rep, 201)
	var v elem
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", rep.ID), nil, &v, 200)
	if !strings.Contains(v.HTML, "s3cret &lt;b&gt;") || strings.Contains(v.HTML, "s3cret <b>") || strings.Index(v.HTML, "Alpha") > strings.Index(v.HTML, "Gamma") {
		t.Fatalf("report: %s", v.HTML)
	}
	r.must("GET", fmt.Sprintf("/api/v1/elements/%d", rep.ID), nil, &v, 200)
	if strings.Contains(v.HTML, "s3cret") || !strings.Contains(v.HTML, "Alpha") {
		t.Fatalf("report for readers: %s", v.HTML)
	}

	// CSV: no formulas for spreadsheets
	admin.must("PUT", fmt.Sprintf("/api/v1/x/core.properties/elements/%d", p1.ID), map[string]any{"values": map[string]any{"room": "=HYPERLINK(\"x\")"}}, nil, 200)
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/x/core.properties/query.csv", strings.NewReader(fmt.Sprintf(`{"sources":[{"kind":"category","id":%d,"sub":true}],"columns":["room"]}`, c.ID)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "ewiki")
	resp, err := admin.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	csv, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(csv), `'=HYPERLINK`) {
		t.Fatalf("csv %d: %s", resp.StatusCode, csv)
	}

	// removing a definition keeps the values as free properties
	var defs struct {
		Defs []struct {
			ID  int64  `json:"id"`
			Key string `json:"key"`
		} `json:"defs"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/core.properties/defs?category=%d", c.ID), nil, &defs, 200)
	for _, x := range defs.Defs {
		if x.Key == "due" {
			admin.must("DELETE", fmt.Sprintf("/api/v1/x/core.properties/defs/%d", x.ID), nil, nil, 204)
		}
	}
	if a = get(admin, p1.ID); a.value("due") != `"2030-01-01"` {
		t.Fatalf("after removing the definition: %+v", a)
	}
}
