package wopi

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func keyAttrs(k *rsa.PrivateKey, prefix string) string {
	return fmt.Sprintf(`%smodulus="%s" %sexponent="%s"`, prefix, b64(k.N.Bytes()), prefix, b64(big.NewInt(int64(k.E)).Bytes()))
}

func discoveryXML(cur, old *rsa.PrivateKey) string {
	return `<?xml version="1.0" encoding="utf-8"?><wopi-discovery>
<net-zone name="external-http">
 <app name="writer"><action default="true" ext="docx" name="edit" urlsrc="https://office.example.com/browser/x/cool.html?"/>
  <action ext="docx" name="view" urlsrc="https://office.example.com/browser/x/cool.html?&lt;ui=UI_LLCC&amp;&gt;&lt;rs=DC_LLCC&amp;&gt;&lt;dchat=DISABLE_CHAT&amp;&gt;"/></app>
 <app name="application/vnd.oasis.opendocument.text"><action name="edit" ext="" urlsrc="https://office.example.com/browser/x/cool.html?"/></app>
</net-zone>
<proof-key value="x" ` + keyAttrs(cur, "") + ` ` + keyAttrs(old, "old") + `/>
</wopi-discovery>`
}

func TestDiscoveryAndProof(t *testing.T) {
	cur, _ := rsa.GenerateKey(rand.Reader, 2048)
	old, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	d, err := Parse([]byte(discoveryXML(cur, old)), "")
	if err != nil {
		t.Fatal(err)
	}
	if a := d.Find("DOCX", "", "edit"); a == nil || a.Name != "edit" {
		t.Fatalf("docx edit: %+v", a)
	}
	if a := d.Find("odt", "application/vnd.oasis.opendocument.text", "edit"); a == nil {
		t.Fatal("odt by MIME type not found")
	}
	if a := d.Find("exe", "application/x-msdownload", "edit", "view"); a != nil {
		t.Fatalf("exe found: %+v", a)
	}
	v := d.Find("docx", "", "view")
	u, err := ActionURL(v.URLSrc, "https://wiki.example.com/api/v1/wopi/files/7", "de-AT")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "ui=de-AT") || strings.Contains(u, "DISABLE_CHAT") || !strings.Contains(u, "WOPISrc=https%3A%2F%2Fwiki.example.com%2Fapi%2Fv1%2Fwopi%2Ffiles%2F7") {
		t.Fatalf("action URL %s", u)
	}
	if _, err := ActionURL("javascript:alert(1)", "x", ""); err == nil {
		t.Fatal("javascript: URL accepted")
	}

	now := time.Now()
	tok, full := "tok-123", "https://wiki.example.com/api/v1/wopi/files/7?access_token=tok-123"
	sign := func(k *rsa.PrivateKey, ts int64) string {
		dg := sha256.Sum256(ProofData(tok, full, ts))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, dg[:])
		return b64(sig)
	}
	req := func(proof, proofOld string, ts int64) error {
		r := httptest.NewRequest("GET", "/api/v1/wopi/files/7?access_token=tok-123", nil)
		r.Header.Set("X-WOPI-TimeStamp", strconv.FormatInt(ts, 10))
		r.Header.Set("X-WOPI-Proof", proof)
		r.Header.Set("X-WOPI-ProofOld", proofOld)
		return d.Proof.Verify(r, tok, full, now)
	}
	ts := Ticks(now)
	if err := req(sign(cur, ts), "", ts); err != nil {
		t.Fatalf("current key: %v", err)
	}
	if err := req(sign(old, ts), "", ts); err != nil {
		t.Fatalf("old key (rotation): %v", err)
	}
	if err := req("", sign(cur, ts), ts); err != nil {
		t.Fatalf("ProofOld with current key: %v", err)
	}
	if err := req(sign(other, ts), sign(other, ts), ts); err == nil {
		t.Fatal("foreign key accepted")
	}
	stale := Ticks(now.Add(-21 * time.Minute))
	if err := req(sign(cur, stale), "", stale); err == nil {
		t.Fatal("stale timestamp accepted")
	}
	if err := req(sign(cur, ts), "", ts+1); err == nil {
		t.Fatal("signature for another timestamp accepted")
	}
	if err := req(sign(cur, 1<<62), "", 1<<62); err == nil {
		t.Fatal("absurd timestamp accepted")
	}
	// another URL (e.g. a different file) must not verify
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-WOPI-TimeStamp", strconv.FormatInt(ts, 10))
	r.Header.Set("X-WOPI-Proof", sign(cur, ts))
	if err := d.Proof.Verify(r, tok, "https://wiki.example.com/api/v1/wopi/files/8?access_token=tok-123", now); err == nil {
		t.Fatal("proof for another URL accepted")
	}
	// short keys are refused
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	if _, err := Parse([]byte(discoveryXML(small, old)), ""); err == nil {
		t.Fatal("1024 bit proof key accepted")
	}
}
