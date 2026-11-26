package server_test

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"testing"
)

// rawJSON posts body and returns status and the decoded answer.
func (c *client) rawJSON(method, path string, body any) (int, map[string]any) {
	c.e.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, c.e.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "ewiki")
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return res.StatusCode, out
}

func mustPort(raw string) string {
	u, _ := url.Parse(raw)
	return u.Port()
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// raw sends a request without failing the test (for goroutines).
func (c *client) send(method, path string, body any) (int, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, c.e.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "ewiki")
	res, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.StatusCode, nil
}

// pngHeader returns a PNG whose header claims w × h pixels (only the
// header is valid: enough for the dimension check).
func pngHeader(w, h uint32) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1)))
	b := buf.Bytes()
	binary.BigEndian.PutUint32(b[16:], w) // IHDR data starts at 16
	binary.BigEndian.PutUint32(b[20:], h)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	return b
}

func (c *client) postRaw(path string, body []byte) (int, []byte) {
	req, _ := http.NewRequest("POST", c.e.srv.URL+path, bytes.NewReader(body))
	req.Header.Set("X-Requested-With", "ewiki")
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func find(list []uiString, key string) uiString {
	for _, s := range list {
		if s.Key == key {
			return s
		}
	}
	return uiString{}
}

func pngEncode(w io.Writer, width, height int) error {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 10; x < width-10; x++ {
		img.Set(x, height/2, color.Black)
	}
	return png.Encode(w, img)
}

func testPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func (c *client) fetch(t *testing.T, path string) (int, []byte, http.Header) {
	t.Helper()
	req, _ := http.NewRequest("GET", c.e.srv.URL+path, nil)
	if c.host != "" {
		req.Host = c.host
	}
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b, res.Header
}

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
	if c.host != "" {
		req.Host = c.host
	}
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

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

type idResp struct {
	ID int64 `json:"id"`
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (c *client) raw(path string, hdr map[string]string) (*http.Response, string) {
	req, _ := http.NewRequest("GET", c.e.srv.URL+path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}
