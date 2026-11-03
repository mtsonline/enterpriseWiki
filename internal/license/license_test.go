package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestKeys(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	pubs := []string{base64.StdEncoding.EncodeToString(other), base64.StdEncoding.EncodeToString(pub)}
	key, err := Sign(License{ID: "L-1", Licensee: "Universität Musterstadt", Edition: "edu", Users: 500, Issued: "2026-11-03", Expires: "2027-11-02"}, priv)
	if err != nil {
		t.Fatal(err)
	}
	l, err := ParseWith(key[:40]+"\n "+key[40:], pubs) // line breaks from e-mails
	if err != nil || l.Licensee != "Universität Musterstadt" || l.Users != 500 {
		t.Fatalf("parse: %+v %v", l, err)
	}
	// tampered payload
	parts := strings.Split(key, ".")
	forged, _ := Encode(License{ID: "L-1", Licensee: "Universität Musterstadt", Edition: "edu", Users: 50000, Issued: "2026-11-03"})
	if _, err := ParseWith(forged+"."+parts[2], pubs); err != ErrSignature {
		t.Fatalf("forged key: %v", err)
	}
	if _, err := ParseWith(key, pubs[:1]); err != ErrSignature {
		t.Fatalf("unknown signer: %v", err)
	}
	if _, err := ParseWith("hello", pubs); err != ErrFormat {
		t.Fatalf("garbage: %v", err)
	}
}

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.Local)
	if st := Evaluate("", 3, now); st.Installed || st.Level != "info" {
		t.Fatalf("no key: %+v", st)
	}
	if st := Evaluate("EWL1.x.y", 3, now); st.Valid || st.Level != "error" {
		t.Fatalf("bad key: %+v", st)
	}
}

func TestEvaluateTerms(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pubs := []string{base64.StdEncoding.EncodeToString(pub)}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.Local)
	key := func(users int, expires string) string {
		k, _ := Sign(License{ID: "L", Licensee: "Org", Edition: "business", Users: users, Issued: "2026-01-01", Expires: expires}, priv)
		return k
	}
	cases := []struct {
		key    string
		users  int
		level  string
		valid  bool
		over   bool
		expire bool
	}{
		{key(100, "2027-12-31"), 50, "ok", true, false, false},
		{key(100, ""), 150, "warning", true, true, false},
		{key(0, "2026-11-20"), 9999, "warning", true, false, false}, // expires soon
		{key(100, "2026-11-03"), 1, "ok", true, false, false},       // last day still valid → but < 30 days
		{key(100, "2026-11-02"), 1, "error", false, false, true},
	}
	for i, c := range cases {
		st := EvaluateWith(c.key, pubs, c.users, now)
		if i == 3 {
			c.level = "warning"
		}
		if st.Level != c.level || st.Valid != c.valid || st.OverUsers != c.over || st.Expired != c.expire {
			t.Errorf("case %d: %+v", i, st)
		}
	}
}
