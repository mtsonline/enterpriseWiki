package signature_smime

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

type ca struct {
	cert *x509.Certificate
	key  crypto.Signer
}

func newCA(t *testing.T) *ca {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Unit CA"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(24 * time.Hour * 3650), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, k.Public(), k)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return &ca{c, k}
}

func (a *ca) issue(t *testing.T, key crypto.Signer, mod func(*x509.Certificate)) *x509.Certificate {
	t.Helper()
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "Ann"}, EmailAddresses: []string{"Ann@Example.com"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour * 365), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection}}
	if mod != nil {
		mod(tpl)
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, a.cert, key.Public(), a.key)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return c
}

func ecKey(t *testing.T) crypto.Signer {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestParseUpload(t *testing.T) {
	a := newCA(t)
	now := time.Now()
	k := ecKey(t)
	c := a.issue(t, k, nil)
	p12, err := pkcs12.Modern.Encode(k, c, []*x509.Certificate{a.cert}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseUpload(p12, "pw", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.emails) != 1 || got.emails[0] != "ann@example.com" || len(got.parents) != 1 || got.trusted {
		t.Fatalf("parsed: %+v", got)
	}
	if _, err := parseUpload(p12, "wrong", now); err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("wrong password: %v", err)
	}
	// PEM with an unencrypted key, in any order
	kd, _ := x509.MarshalPKCS8PrivateKey(k)
	pemFile := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.cert.Raw}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kd})...)
	pemFile = append(pemFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	if got, err := parseUpload(pemFile, "", now); err != nil || !got.cert.Equal(c) || len(got.parents) != 1 {
		t.Fatalf("PEM: %v", err)
	}
	enc := pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte{1}})
	if _, err := parseUpload(append(enc, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...), "", now); err == nil {
		t.Fatal("encrypted PEM key accepted")
	}
	reject := func(name string, key crypto.Signer, mod func(*x509.Certificate)) {
		t.Helper()
		x := a.issue(t, key, mod)
		b, err := pkcs12.Modern.Encode(key, x, nil, "pw")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseUpload(b, "pw", now); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	reject("no address", ecKey(t), func(c *x509.Certificate) { c.EmailAddresses = nil })
	reject("expired", ecKey(t), func(c *x509.Certificate) { c.NotAfter = now.Add(-time.Minute) })
	reject("TLS certificate", ecKey(t), func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth} })
	reject("only encryption", ecKey(t), func(c *x509.Certificate) { c.KeyUsage = x509.KeyUsageKeyEncipherment })
	reject("CA", ecKey(t), func(c *x509.Certificate) { c.IsCA, c.BasicConstraintsValid = true, true })
	small, err := rsa.GenerateKey(rand.Reader, 1024) // #nosec G403 -- the test checks that small keys are refused
	if err != nil {
		t.Fatal(err)
	}
	reject("RSA 1024", small, nil)
	// a key that does not belong to the certificate
	other := ecKey(t)
	b, err := pkcs12.Modern.Encode(other, c, nil, "pw")
	if err == nil {
		if _, err := parseUpload(b, "pw", now); err == nil {
			t.Fatal("foreign key accepted")
		}
	}
	if _, err := parseUpload(make([]byte, MaxUpload+1), "", now); err == nil {
		t.Fatal("large file accepted")
	}
}

func TestSignVerify(t *testing.T) {
	a := newCA(t)
	k := ecKey(t)
	c := a.issue(t, k, nil)
	content := []byte("Content-Type: text/plain\r\n\r\nhello\r\n")
	der, err := signDetached(content, c, k, []*x509.Certificate{a.cert})
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(a.cert)
	if got, err := verifyDetached(content, der, roots); err != nil || !got.Equal(c) {
		t.Fatalf("verify: %v", err)
	}
	if _, err := verifyDetached([]byte("changed"), der, nil); err == nil {
		t.Fatal("changed content verifies")
	}
}

func TestThreshold(t *testing.T) {
	for days, want := range map[int]int{365: 0, 61: 0, 60: 60, 31: 60, 30: 30, 15: 30, 14: 14, 8: 14, 7: 7, 2: 7, 1: 1, 0: 1, -3: 1} {
		if got := threshold(days); got != want {
			t.Errorf("threshold(%d) = %d, want %d", days, got, want)
		}
	}
}
