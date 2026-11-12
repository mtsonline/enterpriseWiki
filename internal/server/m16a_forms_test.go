package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/plugins/content_forms"
)

type formStart struct {
	Token string            `json:"token"`
	State string            `json:"state"`
	Pre   map[string]string `json:"prefill"`
}

// submitForm sends a submission as multipart (files: field -> name -> content).
func (c *client) submitForm(t *testing.T, id int64, data map[string]any, files map[string]map[string][]byte) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	dj, _ := json.Marshal(data)
	_ = mw.WriteField("data", string(dj))
	for field, fs := range files {
		for name, content := range fs {
			fw, _ := mw.CreateFormFile("file."+field, name)
			_, _ = fw.Write(content)
		}
	}
	_ = mw.Close()
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/x/content.forms/elements/%d/submit?lang=en", c.e.srv.URL, id), &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "ewiki")
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// TestForms covers forms: rendering, the token, checks on the server,
// uploads, the hidden field, limits, rights on the submissions, exports
// and the property database as target.
func TestForms(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Contact"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)

	// a property database as target
	admin.must("POST", "/api/v1/x/core.properties/defs", map[string]any{"key": "topic", "type": "text", "labels": map[string]string{"en": "Topic"}}, nil, 201)
	var dbc cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Requests"}}, &dbc, 201)
	var db elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": dbc.ID, "type": "content.propdb", "title": "Requests", "body": map[string]any{"source": fmt.Sprintf("category:%d", dbc.ID)}}, &db, 201)

	def := map[string]any{"intro": "Write to us.", "submit_label": "Send", "success_message": "Thanks!",
		"fields": []map[string]any{
			{"id": "name", "type": "text", "label": "Your name", "required": true, "autocomplete": "name", "width": 6},
			{"id": "mail", "type": "email", "label": "E-mail", "width": 6},
			{"id": "topic", "type": "select", "label": "Topic", "options": []map[string]string{{"value": "it", "label": "IT"}, {"value": "hr", "label": "HR"}}},
			{"id": "other", "type": "text", "label": "Which?", "required": true, "show_if": map[string]string{"field": "topic", "op": "eq", "value": "hr"}},
			{"id": "doc", "type": "file", "label": "Document", "accept": []string{"pdf"}, "max_files": 2, "max_size_mb": 1},
			{"id": "ok", "type": "consent", "label": "I agree", "required": true},
		},
		"targets": map[string]any{"store": true, "email": []map[string]any{{"to": "office@example.org", "subject": "Request from {{field.name}}"}},
			"propdb": map[string]any{"element_id": db.ID, "title": "{{field.name}}: {{field.topic}}", "mapping": map[string]string{"topic": "topic"}}}}
	var form elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.forms", "title": "Contact", "body": def}, &form, 201)

	// rendering: labels, required marks, groups, the hidden field
	html := viewHTML(t, guest, form.ID)
	for _, want := range []string{`data-ew-widget="form"`, `<label class="ewfm-label" for="f` + fmt.Sprint(form.ID) + `-name">Your name`, `aria-required="true"`,
		`autocomplete="name"`, `data-show-if=`, `name="website"`, `accept=".pdf"`, `>Send</button>`} {
		if !strings.Contains(html, want) {
			t.Fatalf("render lacks %q: %s", want, html)
		}
	}

	var st formStart
	guest.must("GET", fmt.Sprintf("/api/v1/x/content.forms/elements/%d/start", form.ID), nil, &st, 200)
	if st.Token == "" || st.State != "" {
		t.Fatalf("start: %+v", st)
	}
	valid := map[string]any{"name": "Ann", "mail": "ann@example.org", "topic": "it", "ok": true}
	// too fast: a bot
	if code, body := guest.submitForm(t, form.ID, map[string]any{"token": st.Token, "values": valid}, nil); code != 429 {
		t.Fatalf("immediate submit: %d %s", code, body)
	}
	old := content_forms.MinAge
	content_forms.MinAge = 0
	t.Cleanup(func() { content_forms.MinAge = old })
	// a forged token
	if code, _ := guest.submitForm(t, form.ID, map[string]any{"token": "1." + strings.Repeat("0", 64), "values": valid}, nil); code != 400 {
		t.Fatalf("forged token: %d", code)
	}
	// checks on the server (the condition makes "other" required)
	code, body := guest.submitForm(t, form.ID, map[string]any{"token": st.Token, "values": map[string]any{"name": "", "mail": "x@", "topic": "hr", "ok": false}}, nil)
	if code != 422 || !strings.Contains(body, `"field":"name"`) || !strings.Contains(body, `"field":"other"`) || !strings.Contains(body, `"code":"consent"`) || !strings.Contains(body, `"code":"email"`) {
		t.Fatalf("invalid values: %d %s", code, body)
	}
	// wrong file content and active PDF content are refused
	code, body = guest.submitForm(t, form.ID, map[string]any{"token": st.Token, "values": valid}, map[string]map[string][]byte{"doc": {"x.pdf": []byte("MZ\x90\x00 program")}})
	if code != 422 || !strings.Contains(body, "file_content") {
		t.Fatalf("exe as pdf: %d %s", code, body)
	}
	code, body = guest.submitForm(t, form.ID, map[string]any{"token": st.Token, "values": valid}, map[string]map[string][]byte{"doc": {"x.pdf": []byte("%PDF-1.4 /OpenAction << /JS (x) >>")}})
	if code != 422 || !strings.Contains(body, "pdf_active") {
		t.Fatalf("active pdf: %d %s", code, body)
	}
	code, _ = guest.submitForm(t, form.ID, map[string]any{"token": st.Token, "values": valid}, map[string]map[string][]byte{"doc": {"big.pdf": append([]byte("%PDF-1.4 "), bytes.Repeat([]byte("a"), 2<<20)...)}})
	if code != 413 {
		t.Fatalf("2 MB file in a 1 MB field: %d", code)
	}
	// the hidden field: answered as success, nothing stored
	code, body = guest.submitForm(t, form.ID, map[string]any{"token": st.Token, "values": valid, "website": "http://spam"}, nil)
	if code != 200 || !strings.Contains(body, `"id":0`) {
		t.Fatalf("honeypot: %d %s", code, body)
	}
	// valid submission with a file; a value that looks like a formula
	valid["name"] = "=HYPERLINK(\"x\")"
	code, body = guest.submitForm(t, form.ID, map[string]any{"token": st.Token, "values": valid}, map[string]map[string][]byte{"doc": {"letter.pdf": []byte("%PDF-1.4 plain letter")}})
	if code != 201 || !strings.Contains(body, "Thanks!") {
		t.Fatalf("valid: %d %s", code, body)
	}
	var queued int
	if err := e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM mail_outbox WHERE subject LIKE 'Request from %'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("mail queued: %d %v", queued, err)
	}

	// the submissions: not for guests or others, for the creator
	if code := guest.do("GET", fmt.Sprintf("/api/v1/x/content.forms/elements/%d/submissions", form.ID), nil, nil); code != 401 {
		t.Fatalf("guest sees submissions: %d", code)
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "carl", "password": "Carl-Passw0rd!", "display_name": "Carl", "roles": []int64{}}, nil, 201)
	carl := e.client()
	carl.login("carl", "Carl-Passw0rd!")
	if code := carl.do("GET", fmt.Sprintf("/api/v1/x/content.forms/elements/%d/submissions", form.ID), nil, nil); code != 403 {
		t.Fatalf("other user sees submissions: %d", code)
	}
	var list struct {
		Total int `json:"total"`
		Items []struct {
			ID    int64          `json:"id"`
			Data  map[string]any `json:"data"`
			Files []struct {
				Name string `json:"name"`
				Hash string `json:"hash"`
			} `json:"files"`
			Pending []string `json:"pending"`
		} `json:"items"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/content.forms/elements/%d/submissions", form.ID), nil, &list, 200)
	if list.Total != 1 || list.Items[0].Data["topic"] != "it" || len(list.Items[0].Files) != 1 || list.Items[0].Files[0].Hash != "" {
		t.Fatalf("submissions: %+v", list)
	}
	sid := list.Items[0].ID
	if code := carl.do("GET", fmt.Sprintf("/api/v1/x/content.forms/submissions/%d/files/0", sid), nil, nil); code != 404 {
		t.Fatalf("other user downloads: %d", code)
	}
	code, data, hdr := admin.fetch(t, fmt.Sprintf("/api/v1/x/content.forms/submissions/%d/files/0", sid))
	if code != 200 || string(data) != "%PDF-1.4 plain letter" || !strings.HasPrefix(hdr.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download: %d %q %v", code, data, hdr)
	}
	code, data, _ = admin.fetch(t, fmt.Sprintf("/api/v1/x/content.forms/elements/%d/submissions.csv", form.ID))
	if code != 200 || !strings.Contains(string(data), `'=HYPERLINK`) || !strings.Contains(string(data), "IT") {
		t.Fatalf("csv: %d %s", code, data)
	}

	// the property database: the worker adds the entry with the creator's rights
	if err := e.app.Workers.RunOnce(context.Background(), "content.forms.deliver"); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := e.app.DB.QueryRow(context.Background(), `SELECT r.title FROM elements e JOIN element_revisions r ON r.element_id=e.id WHERE e.category_id=$1 AND e.type='core.html'`, dbc.ID).Scan(&title); err != nil || !strings.Contains(title, "HYPERLINK") {
		t.Fatalf("entry: %q %v", title, err)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/content.forms/elements/%d/submissions", form.ID), nil, &list, 200)
	if len(list.Items[0].Pending) != 0 {
		t.Fatalf("pending after delivery: %+v", list.Items[0].Pending)
	}

	// once per person, closed forms
	def["once_per_user"] = true
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", form.ID), map[string]any{"title": "Contact", "body": def, "base_rev": 1}, nil, 200)
	carl.must("GET", fmt.Sprintf("/api/v1/x/content.forms/elements/%d/start", form.ID), nil, &st, 200)
	valid["name"] = "Carl"
	if code, body := carl.submitForm(t, form.ID, map[string]any{"token": st.Token, "values": valid}, nil); code != 201 {
		t.Fatalf("carl: %d %s", code, body)
	}
	if code, _ := carl.submitForm(t, form.ID, map[string]any{"token": st.Token, "values": valid}, nil); code != 409 {
		t.Fatalf("carl twice: %d", code)
	}
	guest.must("GET", fmt.Sprintf("/api/v1/x/content.forms/elements/%d/start", form.ID), nil, &st, 200)
	if st.State != "sign_in" {
		t.Fatalf("guest of a once-per-person form: %+v", st)
	}
	past := time.Now().Add(-time.Hour)
	def["once_per_user"], def["closes_at"] = false, past
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", form.ID), map[string]any{"title": "Contact", "body": def, "base_rev": 2}, nil, 200)
	guest.must("GET", fmt.Sprintf("/api/v1/x/content.forms/elements/%d/start", form.ID), nil, &st, 200)
	if st.State != "closed" {
		t.Fatalf("closed: %+v", st)
	}
	if code, _ := guest.submitForm(t, form.ID, map[string]any{"token": st.Token, "values": valid}, nil); code != 409 {
		t.Fatalf("submit to a closed form: %d", code)
	}
	// deleting a submission
	admin.must("DELETE", fmt.Sprintf("/api/v1/x/content.forms/submissions/%d", sid), nil, nil, 204)
	admin.must("GET", fmt.Sprintf("/api/v1/x/content.forms/elements/%d/submissions", form.ID), nil, &list, 200)
	if list.Total != 1 {
		t.Fatalf("after delete: %d", list.Total)
	}
}
