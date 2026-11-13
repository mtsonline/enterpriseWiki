package server_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/smallstep/pkcs7"
	"software.sslmate.com/src/go-pkcs12"

	"github.com/mtsonline/enterprisewiki/internal/mail"
)

// testCA is a certification authority for S/MIME tests.
type testCA struct {
	cert *x509.Certificate
	key  crypto.Signer
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test S/MIME CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, k.Public(), k)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return &testCA{cert: c, key: k}
}

// issue creates a sender certificate for the addresses, valid from now
// for the given time, with an RSA or ECDSA key.
func (ca *testCA) issue(t *testing.T, emails []string, valid time.Duration, rsaKey bool) (*x509.Certificate, crypto.Signer) {
	t.Helper()
	var k crypto.Signer
	var err error
	if rsaKey {
		k, err = rsa.GenerateKey(rand.Reader, 2048)
	} else {
		k, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	if err != nil {
		t.Fatal(err)
	}
	sn, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tpl := &x509.Certificate{SerialNumber: sn, Subject: pkix.Name{CommonName: emails[0]}, EmailAddresses: emails,
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(valid),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca.cert, k.Public(), ca.key)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return c, k
}

// p12 encodes a certificate with key and CA as PKCS#12 (base64).
func (ca *testCA) p12(t *testing.T, c *x509.Certificate, k crypto.Signer, password string) string {
	t.Helper()
	b, err := pkcs12.Modern.Encode(k, c, []*x509.Certificate{ca.cert}, password)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func (ca *testCA) pemFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// opensslVerify checks a signed message with OpenSSL (the reference
// implementation receivers use). Without OpenSSL the check is skipped
// locally but required in the CI.
func opensslVerify(t *testing.T, ca *testCA, msg []byte) string {
	t.Helper()
	bin, err := exec.LookPath("openssl")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("openssl is needed for the S/MIME check")
		}
		t.Log("openssl not found: S/MIME check with OpenSSL skipped")
		return ""
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "msg.eml")
	if err := os.WriteFile(in, msg, 0o600); err != nil {
		t.Fatal(err)
	}
	signer := filepath.Join(dir, "signer.pem")
	out, err := exec.Command(bin, "smime", "-verify", "-in", in, "-CAfile", ca.pemFile(t), "-purpose", "smimesign", // #nosec G204 -- test
		"-signer", signer, "-out", filepath.Join(dir, "content")).CombinedOutput()
	if err != nil {
		t.Fatalf("openssl smime -verify: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(signer) // #nosec G304 -- test
	return string(b)
}

// signerEmails parses the multipart/signed message and returns the e-mail
// addresses of the signing certificate after checking the signature.
func signerEmails(t *testing.T, msg []byte) []string {
	t.Helper()
	s := string(msg)
	m := regexp.MustCompile(`Content-Type: multipart/signed; protocol="application/pkcs7-signature"; micalg=sha-256; boundary="([^"]+)"`).FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	b := "--" + m[1]
	start := strings.Index(s, b+"\r\n")
	sigStart := strings.Index(s, "\r\n"+b+"\r\nContent-Type: application/pkcs7-signature")
	if start < 0 || sigStart < 0 {
		t.Fatalf("malformed multipart/signed")
	}
	content := s[start+len(b)+2 : sigStart]
	rest := s[sigStart+len(b)+4:]
	body := rest[strings.Index(rest, "\r\n\r\n")+4 : strings.Index(rest, "\r\n"+b+"--")]
	der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(body), ""))
	if err != nil {
		t.Fatal(err)
	}
	p7, err := pkcs7.Parse(der)
	if err != nil {
		t.Fatal(err)
	}
	p7.Content = []byte(content)
	if err := p7.Verify(); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
	return p7.GetOnlySigner().EmailAddresses
}

type smimeCert struct {
	ID       int64    `json:"id"`
	UserID   *int64   `json:"user_id"`
	Emails   []string `json:"emails"`
	Trusted  bool     `json:"trusted"`
	State    string   `json:"state"`
	DaysLeft int      `json:"days_left"`
	SignMail bool     `json:"sign_mail"`
	Seal     bool     `json:"seal"`
}

// lastTo returns the last captured message to addr.
func (c *capture) lastTo(t *testing.T, addr string) []byte {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.msgs) - 1; i >= 0; i-- {
		for _, r := range c.msgs[i].Recipients {
			if r == addr {
				return c.msgs[i].Data
			}
		}
	}
	t.Fatalf("no message to %s", addr)
	return nil
}

// TestSMIMEMail: certificates of the organisation and personal ones sign
// outgoing e-mails (checked with OpenSSL); only fitting certificates are
// accepted; expired certificates send unsigned or hold, as set.
func TestSMIMEMail(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"mail": map[string]any{"from": "wiki@example.com", "allowed_from": []string{"*@example.com"}}}, nil, 200)
	c := &capture{}
	e.app.Mail.SetTransport(c)
	ca := newTestCA(t)
	const base = "/api/v1/x/signature.smime"

	org, orgKey := ca.issue(t, []string{"wiki@example.com"}, 365*24*time.Hour, true)
	file := ca.p12(t, org, orgKey, "geheim")
	// wrong password, foreign sender, bad base64: rejected
	if code := admin.do("POST", base+"/certs", map[string]any{"scope": "org", "file": file, "password": "falsch"}, nil); code != 400 {
		t.Fatalf("wrong password: %d", code)
	}
	other, otherKey := ca.issue(t, []string{"someone@elsewhere.test"}, 365*24*time.Hour, false)
	if code := admin.do("POST", base+"/certs", map[string]any{"scope": "org", "file": ca.p12(t, other, otherKey, "x"), "password": "x"}, nil); code != 400 {
		t.Fatalf("certificate of a foreign sender: %d", code)
	}
	if code := admin.do("POST", base+"/certs", map[string]any{"scope": "org", "file": "%%%", "password": "x"}, nil); code != 400 {
		t.Fatalf("bad base64: %d", code)
	}
	var cert smimeCert
	admin.must("POST", base+"/certs", map[string]any{"scope": "org", "file": file, "password": "geheim", "label": "Org 2026"}, &cert, 201)
	if cert.UserID != nil || cert.State != "valid" || cert.Trusted || len(cert.Emails) != 1 || !cert.SignMail {
		t.Fatalf("stored certificate: %+v", cert)
	}
	if code := admin.do("POST", base+"/certs", map[string]any{"scope": "org", "file": file, "password": "geheim"}, nil); code != 409 {
		t.Fatalf("same certificate twice: %d", code)
	}
	// the key is sealed like every secret and never returned
	_, raw := admin.rawBody("GET", base+"/certs?scope=org", "")
	if strings.Contains(raw, "PRIVATE") || strings.Contains(raw, `"key"`) {
		t.Fatalf("key in the list: %.300s", raw)
	}

	// a message of the wiki is signed with the certificate of the organisation
	if _, err := e.app.Mail.Enqueue(ctx, e.app.DB, mail.Message{To: []string{"reader@example.org"}, Subject: "Signed", Text: "Hello\n\nä ö ü"}); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	msg := c.lastTo(t, "reader@example.org")
	if got := signerEmails(t, msg); len(got) != 1 || got[0] != "wiki@example.com" {
		t.Fatalf("signed by %v:\n%.600s", got, msg)
	}
	if signer := opensslVerify(t, ca, msg); signer != "" && !strings.Contains(signer, "BEGIN CERTIFICATE") {
		t.Fatalf("openssl signer: %s", signer)
	}
	// immediate messages (test e-mail) are signed too
	admin.must("POST", "/api/v1/admin/mail/test", map[string]any{"to": "test@example.org"}, nil, 200)
	if got := signerEmails(t, c.lastTo(t, "test@example.org")); len(got) != 1 {
		t.Fatal("test e-mail not signed")
	}

	// switched off: unsigned
	admin.must("PUT", base+"/settings", map[string]any{"mail_signing": false, "on_expired": "unsigned", "personal": true, "seal_evidence": false}, nil, 200)
	if _, err := e.app.Mail.Enqueue(ctx, e.app.DB, mail.Message{To: []string{"plain@example.org"}, Subject: "Plain", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	if signerEmails(t, c.lastTo(t, "plain@example.org")) != nil {
		t.Fatal("signed although switched off")
	}
	admin.must("PUT", base+"/settings", map[string]any{"mail_signing": true, "on_expired": "unsigned", "personal": true, "seal_evidence": false}, nil, 200)

	// personal certificate: only for a confirmed address of the account
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bea", "password": "Bea-Passw0rd!xy", "display_name": "Bea", "email": "bea@example.com"}, nil, 201)
	bea := e.client()
	bea.login("bea", "Bea-Passw0rd!xy")
	notMine, notMineKey := ca.issue(t, []string{"boss@example.com"}, 200*24*time.Hour, false)
	if code := bea.do("POST", base+"/certs", map[string]any{"scope": "mine", "file": ca.p12(t, notMine, notMineKey, "p"), "password": "p"}, nil); code != 400 {
		t.Fatalf("certificate of someone else's address: %d", code)
	}
	if code := bea.do("POST", base+"/certs", map[string]any{"scope": "org", "file": file, "password": "geheim"}, nil); code != 403 {
		t.Fatalf("bea uploads for the organisation: %d", code)
	}
	if code := bea.do("GET", base+"/certs?scope=org", nil, nil); code != 403 {
		t.Fatalf("bea lists the organisation: %d", code)
	}
	beaCert, beaKey := ca.issue(t, []string{"bea@example.com"}, 200*24*time.Hour, false)
	var mine smimeCert
	bea.must("POST", base+"/certs", map[string]any{"scope": "mine", "file": ca.p12(t, beaCert, beaKey, "p"), "password": "p"}, &mine, 201)
	if mine.UserID == nil {
		t.Fatalf("personal certificate: %+v", mine)
	}
	var me struct {
		Personal bool        `json:"personal"`
		Emails   []string    `json:"emails"`
		Certs    []smimeCert `json:"certs"`
	}
	bea.must("GET", base+"/me", nil, &me, 200)
	if !me.Personal || len(me.Certs) != 1 || len(me.Emails) == 0 {
		t.Fatalf("me: %+v", me)
	}
	// someone else cannot see or remove it; administrators can see it
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "carl", "password": "Carl-Passw0rd!x", "display_name": "Carl"}, nil, 201)
	carl := e.client()
	carl.login("carl", "Carl-Passw0rd!x")
	if code := carl.do("DELETE", fmt.Sprintf("%s/certs/%d", base, mine.ID), nil, nil); code != 404 {
		t.Fatalf("carl removes bea's certificate: %d", code)
	}
	var all []smimeCert
	admin.must("GET", base+"/certs?scope=all", nil, &all, 200)
	if len(all) != 2 {
		t.Fatalf("all certificates: %+v", all)
	}
	// e-mails sent in bea's name are signed with her certificate, and logged
	if _, err := e.app.Mail.Enqueue(ctx, e.app.DB, mail.Message{To: []string{"partner@example.org"}, From: "bea@example.com", Subject: "From Bea", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	if got := signerEmails(t, c.lastTo(t, "partner@example.org")); len(got) != 1 || got[0] != "bea@example.com" {
		t.Fatalf("message in bea's name signed by %v", got)
	}
	var used int
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='signature.smime.mail_signed'`).Scan(&used)
	if used != 1 {
		t.Fatalf("uses of the personal key logged: %d", used)
	}
	// personal certificates switched off: the organisation signs (its address is not bea's)
	admin.must("PUT", base+"/settings", map[string]any{"mail_signing": true, "on_expired": "unsigned", "personal": false, "seal_evidence": false}, nil, 200)
	if code := bea.do("POST", base+"/certs", map[string]any{"scope": "mine", "file": ca.p12(t, beaCert, beaKey, "p"), "password": "p"}, nil); code != 403 {
		t.Fatalf("upload while switched off: %d", code)
	}
	if _, err := e.app.Mail.Enqueue(ctx, e.app.DB, mail.Message{To: []string{"partner2@example.org"}, From: "bea@example.com", Subject: "From Bea", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	if got := signerEmails(t, c.lastTo(t, "partner2@example.org")); got != nil {
		t.Fatalf("bea's message signed by %v although personal certificates are off", got)
	}
	bea.must("DELETE", fmt.Sprintf("%s/certs/%d", base, mine.ID), nil, nil, 204)

	// expired certificate of the organisation: unsigned, or held when set
	if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_smime_certs SET not_after = now() - interval '1 hour' WHERE id=$1`, cert.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.Mail.Enqueue(ctx, e.app.DB, mail.Message{To: []string{"late@example.org"}, Subject: "Late", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	if signerEmails(t, c.lastTo(t, "late@example.org")) != nil {
		t.Fatal("signed with an expired certificate")
	}
	admin.must("PUT", base+"/settings", map[string]any{"mail_signing": true, "on_expired": "hold", "personal": true, "seal_evidence": false}, nil, 200)
	id, err := e.app.Mail.Enqueue(ctx, e.app.DB, mail.Message{To: []string{"held@example.org"}, Subject: "Held", Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	var status, lastErr string
	_ = e.app.DB.QueryRow(ctx, `SELECT status, last_error FROM mail_outbox WHERE id=$1`, id).Scan(&status, &lastErr)
	if status != "queued" || !strings.Contains(lastErr, "S/MIME") {
		t.Fatalf("held message: %s %q", status, lastErr)
	}
	// a new certificate releases it at the next attempt
	renewed, renewedKey := ca.issue(t, []string{"wiki@example.com"}, 2*365*24*time.Hour, false)
	admin.must("POST", base+"/certs", map[string]any{"scope": "org", "file": ca.p12(t, renewed, renewedKey, "n"), "password": "n"}, nil, 201)
	if _, err := e.app.DB.Exec(ctx, `UPDATE mail_recipients SET next_attempt_at = now() WHERE message_id=$1;`, id); err != nil {
		t.Fatal(err)
	}
	_, _ = e.app.DB.Exec(ctx, `UPDATE mail_outbox SET next_attempt_at = now() WHERE id=$1`, id)
	e.flushMail(t)
	if got := signerEmails(t, c.lastTo(t, "held@example.org")); len(got) != 1 {
		t.Fatal("held message not sent signed with the new certificate")
	}
	if !bytes.Contains(c.lastTo(t, "held@example.org"), []byte("Subject: Held")) {
		t.Fatal("wrong message")
	}
}

// TestSMIMEExpiry: warnings 60/30/14/7/1 days before the end and once
// after it, each only once; a newer certificate for the same addresses
// silences the warnings of the old one.
func TestSMIMEExpiry(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"mail": map[string]any{"from": "wiki@example.com"}}, nil, 200)
	ca := newTestCA(t)
	const base = "/api/v1/x/signature.smime"
	org, orgKey := ca.issue(t, []string{"wiki@example.com"}, 365*24*time.Hour, false)
	var cert smimeCert
	admin.must("POST", base+"/certs", map[string]any{"scope": "org", "file": ca.p12(t, org, orgKey, "p"), "password": "p"}, &cert, 201)
	run := func() {
		t.Helper()
		if err := e.app.Workers.RunOnce(ctx, "signature.smime.expiry"); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int {
		t.Helper()
		var n int
		_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE kind='signature.smime.expiry'`).Scan(&n)
		return n
	}
	run()
	if count() != 0 {
		t.Fatal("warning a year before the end")
	}
	set := func(days int) {
		t.Helper()
		if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_smime_certs SET not_after = now() + make_interval(days => $2) - interval '1 hour' WHERE id=$1`, cert.ID, days); err != nil {
			t.Fatal(err)
		}
	}
	set(30)
	run()
	run()
	if count() != 1 {
		t.Fatalf("warnings at 30 days: %d", count())
	}
	var list []smimeCert
	admin.must("GET", base+"/certs", nil, &list, 200)
	if list[0].DaysLeft != 30 || list[0].State != "valid" {
		t.Fatalf("list: %+v", list)
	}
	set(7)
	run()
	if count() != 2 {
		t.Fatalf("warnings at 7 days: %d", count())
	}
	var title string
	_ = e.app.DB.QueryRow(ctx, `SELECT title FROM notifications WHERE kind='signature.smime.expiry' ORDER BY id DESC LIMIT 1`).Scan(&title)
	if !strings.Contains(title, "7") {
		t.Fatalf("title: %q", title)
	}
	if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_smime_certs SET not_after = now() - interval '1 hour' WHERE id=$1`, cert.ID); err != nil {
		t.Fatal(err)
	}
	run()
	run()
	if count() != 3 {
		t.Fatalf("warning after the end: %d", count())
	}
	// a renewed certificate: no warnings for the old one
	old, oldKey := ca.issue(t, []string{"wiki@example.com"}, 365*24*time.Hour, false)
	var second smimeCert
	admin.must("POST", base+"/certs", map[string]any{"scope": "org", "file": ca.p12(t, old, oldKey, "p"), "password": "p"}, &second, 201)
	newer, newerKey := ca.issue(t, []string{"wiki@example.com"}, 3*365*24*time.Hour, false)
	admin.must("POST", base+"/certs", map[string]any{"scope": "org", "file": ca.p12(t, newer, newerKey, "p"), "password": "p"}, nil, 201)
	if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_smime_certs SET not_after = now() + interval '5 days' WHERE id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	run()
	if count() != 3 {
		t.Fatalf("warning although a newer certificate exists: %d", count())
	}
}

// TestSMIMECLI: "ewiki smime" goes through the API (same checks; --as
// carries only the rights of that person).
func TestSMIMECLI(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"mail": map[string]any{"from": "wiki@example.com"}}, nil, 200)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "dora", "password": "Dora-Passw0rd!x", "display_name": "Dora", "email": "dora@example.com"}, nil, 201)
	ca := newTestCA(t)
	dir := t.TempDir()
	write := func(name, b64 string) string {
		t.Helper()
		b, _ := base64.StdEncoding.DecodeString(b64)
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	org, orgKey := ca.issue(t, []string{"wiki@example.com"}, 365*24*time.Hour, false)
	orgFile := write("org.p12", ca.p12(t, org, orgKey, "pw-org"))
	passFile := filepath.Join(dir, "pass")
	if err := os.WriteFile(passFile, []byte("pw-org\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := runCLI(t, e.cfg, "smime", "add", orgFile, "--password-file", passFile, "--label", "CLI cert")
	if code != 0 || !strings.Contains(out, `"CLI cert"`) {
		t.Fatalf("smime add: %d %s", code, out)
	}
	var added smimeCert
	_ = json.Unmarshal([]byte(out), &added)
	if code, out = runCLI(t, e.cfg, "smime", "set", fmt.Sprint(added.ID), "--no-sign"); code != 0 || !strings.Contains(out, `"sign_mail": false`) {
		t.Fatalf("smime set: %d %s", code, out)
	}
	if code, out = runCLI(t, e.cfg, "smime", "settings", "on_expired=hold"); code != 0 || !strings.Contains(out, `"on_expired": "hold"`) {
		t.Fatalf("smime settings: %d %s", code, out)
	}
	// dora: no access to the organisation, but her own certificate
	if code, _ = runCLI(t, e.cfg, "smime", "list", "--as", "dora"); code == 0 {
		t.Fatal("dora lists the certificates of the organisation")
	}
	if code, _ = runCLI(t, e.cfg, "smime", "remove", fmt.Sprint(added.ID), "--as", "dora"); code == 0 {
		t.Fatal("dora removes a certificate of the organisation")
	}
	dc, dk := ca.issue(t, []string{"dora@example.com"}, 100*24*time.Hour, false)
	doraFile := write("dora.p12", ca.p12(t, dc, dk, ""))
	if code, out = runCLI(t, e.cfg, "smime", "add", doraFile, "--mine", "--as", "dora"); code != 0 {
		t.Fatalf("dora adds her certificate: %d %s", code, out)
	}
	if code, out = runCLI(t, e.cfg, "smime", "list", "--mine", "--as", "dora"); code != 0 || !strings.Contains(out, "dora@example.com") {
		t.Fatalf("dora's list: %d %s", code, out)
	}
	if code, _ = runCLI(t, e.cfg, "smime", "remove", fmt.Sprint(added.ID)); code != 0 {
		t.Fatal("smime remove")
	}
	if code, _ = runCLI(t, e.cfg, "smime", "frobnicate"); code == 0 {
		t.Fatal("unknown command accepted")
	}
}
