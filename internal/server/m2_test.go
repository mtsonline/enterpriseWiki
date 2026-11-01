package server_test

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/auth"
)

// MinimalPDF builds a small valid PDF with one line of text in a Flate stream.
func MinimalPDF(text string) []byte {
	var cs bytes.Buffer
	zw := zlib.NewWriter(&cs)
	fmt.Fprintf(zw, "BT /F1 18 Tf 72 720 Td (%s) Tj ET", text)
	zw.Close()
	var b bytes.Buffer
	offs := []int{}
	obj := func(s string) { offs = append(offs, b.Len()); b.WriteString(s) }
	b.WriteString("%PDF-1.4\n")
	obj("1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n")
	obj("2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n")
	obj("3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >> endobj\n")
	offs = append(offs, b.Len())
	fmt.Fprintf(&b, "4 0 obj << /Length %d /Filter /FlateDecode >> stream\n", cs.Len())
	b.Write(cs.Bytes())
	b.WriteString("\nendstream endobj\n")
	obj("5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj\n")
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offs)+1, xref)
	return b.Bytes()
}

// tiny 1x1 PNG
var pngData = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 0x49, 0x48, 0x44, 0x52, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 0x1f, 0x15, 0xc4, 0x89, 0, 0, 0, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0, 0x05, 0, 0x01, 0xff, 0x89, 0x99, 0x3d, 0x1d, 0, 0, 0, 0, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82}

type fileResp struct {
	Hash string `json:"hash"`
	Mime string `json:"mime"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func (c *client) upload(t *testing.T, name string, data []byte) (int, fileResp) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", c.e.srv.URL+"/api/v1/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "ewiki")
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var f fileResp
	b, _ := io.ReadAll(res.Body)
	_ = jsonUnmarshal(b, &f)
	return res.StatusCode, f
}

func TestAttachments(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()
	var root, hidden cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Docs"}}, &root, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": root.ID, "titles": map[string]string{"en": "Internal"}}, &hidden, 201)
	if code, _ := guest.upload(t, "x.pdf", MinimalPDF("x")); code != 401 {
		t.Fatalf("guest upload: %d", code)
	}
	code, pdf := admin.upload(t, "../../etc/Report Q3.pdf", MinimalPDF("Quarterly zebra report"))
	if code != 201 || pdf.Mime != "application/pdf" || pdf.Name != "Report Q3.pdf" || len(pdf.Hash) != 64 {
		t.Fatalf("upload %d %+v", code, pdf)
	}
	var el elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": hidden.ID, "type": "core.attachment", "title": "Q3 report",
		"body": map[string]any{"file": map[string]string{"hash": pdf.Hash, "name": pdf.Name}, "description": "Numbers <b>&</b> facts"}}, &el, 201)
	if !strings.Contains(el.HTML, "<iframe") || strings.Contains(el.HTML, "<b>") {
		t.Fatalf("attachment render %s", el.HTML)
	}
	if got := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": hidden.ID, "type": "core.attachment", "title": "x",
		"body": map[string]any{"file": map[string]string{"hash": strings.Repeat("a", 64)}}}, nil); got != 400 {
		t.Fatalf("unknown file accepted: %d", got)
	}
	var sr struct {
		Elements []struct {
			ElementID int64 `json:"element_id"`
		} `json:"elements"`
	}
	admin.must("GET", "/api/v1/search?q=zebra", nil, &sr, 200)
	if len(sr.Elements) != 1 || sr.Elements[0].ElementID != el.ID {
		t.Fatalf("pdf text not searchable: %+v", sr)
	}
	dl := fmt.Sprintf("%s/api/v1/x/core.attachment/elements/%d/file", e.srv.URL, el.ID)
	res, _ := admin.http.Get(dl)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !bytes.HasPrefix(body, []byte("%PDF")) || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") ||
		res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download %d %v", res.StatusCode, res.Header)
	}
	res, _ = admin.http.Get(dl + "?inline=1&hash=" + pdf.Hash)
	if !strings.HasPrefix(res.Header.Get("Content-Disposition"), "inline") {
		t.Fatal("pdf not inline")
	}
	res, _ = guest.http.Get(dl)
	if res.StatusCode != 401 {
		t.Fatalf("guest downloads hidden file: %d", res.StatusCode)
	}
	res, _ = admin.http.Get(e.srv.URL + "/api/v1/files/" + pdf.Hash)
	if res.StatusCode != 404 {
		t.Fatal("non-image served by hash")
	}
	// new version with another file; the old file stays reachable through the history
	_, pdf2 := admin.upload(t, "v2.pdf", MinimalPDF("Second version"))
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", el.ID), map[string]any{"lang": "en", "title": "Q3 report",
		"body": map[string]any{"file": map[string]string{"hash": pdf2.Hash}, "description": ""}}, nil, 200)
	res, _ = admin.http.Get(dl + "?hash=" + pdf.Hash)
	if res.StatusCode != 200 {
		t.Fatalf("old version not downloadable: %d", res.StatusCode)
	}
	res, _ = admin.http.Get(dl + "?hash=" + strings.Repeat("b", 64))
	if res.StatusCode != 404 {
		t.Fatal("foreign hash served")
	}
	// images: public by hash (for pages), active content never inline
	_, img := admin.upload(t, "pic.png", pngData)
	res, _ = guest.http.Get(e.srv.URL + "/api/v1/files/" + img.Hash)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("image %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	_, svg := admin.upload(t, "evil.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	if strings.Contains(svg.Mime, "svg") {
		t.Fatalf("svg type trusted from extension: %s", svg.Mime)
	}
	res, _ = guest.http.Get(e.srv.URL + "/api/v1/files/" + svg.Hash)
	if res.StatusCode != 404 {
		t.Fatal("svg served as image")
	}
	// image in rich text survives sanitizing
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "With image",
		"body": map[string]string{"format": "html", "source": `<p><img src="/api/v1/files/` + img.Hash + `" alt="Logo"></p>`}}, &page, 201)
	if !strings.Contains(page.HTML, `src="/api/v1/files/`+img.Hash+`"`) {
		t.Fatalf("image removed: %s", page.HTML)
	}
	var gc map[string]int
	admin.must("POST", "/api/v1/admin/retention/run", nil, &gc, 200)
}

func TestTOTP(t *testing.T) {
	e := setup(t)
	c := e.client()
	c.login("admin", adminPW)
	var st struct {
		Enabled bool `json:"enabled"`
	}
	c.must("GET", "/api/v1/me/totp", nil, &st, 200)
	var setupRes auth.TOTPSetup
	c.must("POST", "/api/v1/me/totp/setup", nil, &setupRes, 200)
	if !strings.HasPrefix(setupRes.URI, "otpauth://totp/") {
		t.Fatal(setupRes.URI)
	}
	if got := c.do("POST", "/api/v1/me/totp/enable", map[string]string{"code": "000000"}, nil); got != 401 {
		t.Fatalf("wrong code accepted: %d", got)
	}
	code, _ := auth.TOTPCode(setupRes.Secret, time.Now().Unix()/30)
	c.must("POST", "/api/v1/me/totp/enable", map[string]string{"code": code}, nil, 204)
	// login now needs the second factor
	d := e.client()
	if got := d.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": adminPW}, nil); got != 401 {
		t.Fatalf("login without code: %d", got)
	}
	if got := d.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": adminPW, "totp": code}, nil); got != 401 {
		t.Fatalf("replayed code accepted: %d", got)
	}
	next, _ := auth.TOTPCode(setupRes.Secret, time.Now().Unix()/30+1)
	d.must("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": adminPW, "totp": next}, nil, 200)
	// admin reset
	var id int64
	e.app.DB.QueryRow(t.Context(), `SELECT id FROM users WHERE username='admin'`).Scan(&id)
	d.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", id), map[string]any{"reset_totp": true}, nil, 200)
	e.client().login("admin", adminPW)
	c.must("POST", "/api/v1/me/totp/disable", map[string]string{"code": ""}, nil, 204)
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
