package server_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// authenticator is a software WebAuthn authenticator (attestation "none",
// ES256) for tests: it answers the options the wiki sends like a browser
// with a security key would.
type authenticator struct {
	t       *testing.T
	origin  string
	rpID    string
	key     *ecdsa.PrivateKey
	credID  []byte
	handle  []byte
	counter uint32
	flags   byte // UP|UV by default
}

const (
	flagUP = 0x01
	flagUV = 0x04
	flagAT = 0x40
)

func newAuthenticator(t *testing.T, base string) *authenticator {
	u, _ := url.Parse(base)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	return &authenticator{t: t, origin: u.Scheme + "://" + u.Host, rpID: u.Hostname(), key: key, credID: id, flags: flagUP | flagUV}
}

var b64 = base64.RawURLEncoding

func (a *authenticator) authData(attested bool) []byte {
	rp := sha256.Sum256([]byte(a.rpID))
	var buf bytes.Buffer
	buf.Write(rp[:])
	flags := a.flags
	if attested {
		flags |= flagAT
	}
	buf.WriteByte(flags)
	_ = binary.Write(&buf, binary.BigEndian, a.counter)
	if attested {
		buf.Write(make([]byte, 16)) // AAGUID
		_ = binary.Write(&buf, binary.BigEndian, uint16(len(a.credID)))
		buf.Write(a.credID)
		pub := a.key.PublicKey
		x, y := make([]byte, 32), make([]byte, 32)
		pub.X.FillBytes(x)
		pub.Y.FillBytes(y)
		cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
		buf.Write(cose)
	}
	return buf.Bytes()
}

func (a *authenticator) clientData(typ string, options map[string]any) []byte {
	pk := options["publicKey"].(map[string]any)
	cd, _ := json.Marshal(map[string]any{"type": typ, "challenge": pk["challenge"], "origin": a.origin, "crossOrigin": false})
	return cd
}

// create answers registration options.
func (a *authenticator) create(options map[string]any) map[string]any {
	pk := options["publicKey"].(map[string]any)
	if user, ok := pk["user"].(map[string]any); ok {
		a.handle, _ = b64.DecodeString(user["id"].(string))
	}
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": a.authData(true)})
	return map[string]any{"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(a.clientData("webauthn.create", options)), "attestationObject": b64.EncodeToString(att)}}
}

// get answers sign-in options (the counter goes up with every signature).
func (a *authenticator) get(options map[string]any) map[string]any {
	a.counter++
	ad := a.authData(false)
	cd := a.clientData("webauthn.get", options)
	sum := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), sum[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		a.t.Fatal(err)
	}
	return map[string]any{"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(cd), "authenticatorData": b64.EncodeToString(ad),
			"signature": b64.EncodeToString(sig), "userHandle": b64.EncodeToString(a.handle)}}
}

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

// enrollWebAuthn registers key for c through the generic factor setup.
func enrollWebAuthn(t *testing.T, c *client, key *authenticator, name string, passkey bool) map[string]any {
	t.Helper()
	var opts map[string]any
	c.must("POST", "/api/v1/me/mfa/webauthn/begin", map[string]any{"passkey": passkey}, &opts, 200)
	c.must("POST", "/api/v1/me/mfa/webauthn/finish", map[string]any{"name": name, "credential": key.create(opts)}, nil, 204)
	return opts
}

func setFactor(t *testing.T, admin *client, plugin string, n int) {
	t.Helper()
	admin.must("PUT", "/api/v1/admin/mfa", map[string]any{"factors": map[string]int{plugin: n}}, nil, 200)
}

func TestWebAuthn(t *testing.T) {
	e := setup(t)
	// WebAuthn needs a domain: the test server is reached as localhost
	origin := "http://localhost:" + mustPort(e.srv.URL)
	e.setPluginSettings(t, "mfa.webauthn", map[string]any{"rp_id": "localhost", "origins": []any{origin}})
	admin := e.client()
	admin.login("admin", adminPW)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "wendy", "password": "Wendy-Passw0rd!", "display_name": "Wendy"}, &u, 201)
	w := e.client()
	w.login("wendy", "Wendy-Passw0rd!")
	key := newAuthenticator(t, origin)

	// as factor 1 (default) every key is a passkey: user verification required
	opts := enrollWebAuthn(t, w, key, "Laptop", false)
	sel := opts["publicKey"].(map[string]any)["authenticatorSelection"].(map[string]any)
	if sel["residentKey"] != "required" || sel["userVerification"] != "required" {
		t.Fatalf("passkey options %v", sel)
	}
	// a setup is used once
	if got := w.do("POST", "/api/v1/me/mfa/webauthn/finish", map[string]any{"name": "Again", "credential": key.create(opts)}, nil); got != 400 {
		t.Fatalf("setup reused: %d", got)
	}
	var list []map[string]any
	w.must("GET", "/api/v1/x/mfa.webauthn/credentials", nil, &list, 200)
	if len(list) != 1 || list[0]["passkey"] != true {
		t.Fatalf("credentials %v", list)
	}

	// passkey: sign-in without password (factor 1 alternative)
	p := e.client()
	var begin struct {
		Token   string         `json:"token"`
		Options map[string]any `json:"options"`
	}
	p.must("POST", "/api/v1/auth/passwordless/webauthn/begin", nil, &begin, 200)
	p.must("POST", "/api/v1/auth/passwordless/webauthn/finish", map[string]any{"token": begin.Token, "credential": key.get(begin.Options)}, nil, 200)
	if !signedIn(p) {
		t.Fatal("not signed in with the passkey")
	}
	// the password alone works too (factor 1 alternatives)
	e.client().login("wendy", "Wendy-Passw0rd!")
	// without user verification the passkey is refused
	q := e.client()
	q.must("POST", "/api/v1/auth/passwordless/webauthn/begin", nil, &begin, 200)
	key.flags = flagUP
	if got := q.do("POST", "/api/v1/auth/passwordless/webauthn/finish", map[string]any{"token": begin.Token, "credential": key.get(begin.Options)}, nil); got != 401 || signedIn(q) {
		t.Fatalf("passkey without user verification: %d", got)
	}
	key.flags = flagUP | flagUV
	// a signature from another origin is refused
	q.must("POST", "/api/v1/auth/passwordless/webauthn/begin", nil, &begin, 200)
	key.origin = "https://evil.example"
	if got := q.do("POST", "/api/v1/auth/passwordless/webauthn/finish", map[string]any{"token": begin.Token, "credential": key.get(begin.Options)}, nil); got != 401 {
		t.Fatalf("other origin: %d", got)
	}
	key.origin = origin

	// as factor 2 the key confirms a password sign-in; no passkey sign-in then
	setFactor(t, admin, "mfa.webauthn", 2)
	if got := e.client().do("POST", "/api/v1/auth/passwordless/webauthn/begin", nil, nil); got != 404 {
		t.Fatalf("passkey sign-in as factor 2: %d", got)
	}
	d := e.client()
	tok := d.pendingMFA("wendy", "Wendy-Passw0rd!")
	var ch map[string]any
	d.must("POST", "/api/v1/auth/mfa/challenge", map[string]any{"token": tok, "method": "webauthn"}, &ch, 200)
	if allow := ch["publicKey"].(map[string]any)["allowCredentials"].([]any); len(allow) != 1 {
		t.Fatalf("allowCredentials %v", allow)
	}
	other := newAuthenticator(t, origin)
	other.credID = key.credID
	if got := d.do("POST", "/api/v1/auth/mfa/verify", map[string]any{"token": tok, "method": "webauthn", "credential": other.get(ch)}, nil); got != 401 {
		t.Fatalf("foreign signature: %d", got)
	}
	var done struct {
		Done bool `json:"done"`
	}
	d.must("POST", "/api/v1/auth/mfa/verify", map[string]any{"token": tok, "method": "webauthn", "credential": key.get(ch)}, &done, 200)
	if !done.Done || !signedIn(d) {
		t.Fatal("not signed in with the security key")
	}
	// a counter going backwards (cloned key) is refused
	tok = d.pendingMFA("wendy", "Wendy-Passw0rd!")
	d.must("POST", "/api/v1/auth/mfa/challenge", map[string]any{"token": tok, "method": "webauthn"}, &ch, 200)
	key.counter = 0
	if got := d.do("POST", "/api/v1/auth/mfa/verify", map[string]any{"token": tok, "method": "webauthn", "credential": key.get(ch)}, nil); got != 401 {
		t.Fatalf("cloned key: %d", got)
	}

	// changing factors needs a recent sign-in
	if _, err := e.app.DB.Exec(t.Context(), `UPDATE sessions SET created_at = now() - interval '1 hour' WHERE user_id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	e.app.Bus.Publish(t.Context(), "auth", "")
	if st, out := w.rawJSON("POST", "/api/v1/me/mfa/webauthn/begin", map[string]any{"passkey": false}); st != 403 || out["error"].(map[string]any)["code"] != "reauth_required" {
		t.Fatalf("old session adds a key: %d %v", st, out)
	}
	// API keys cannot manage keys
	var apikey struct {
		Token string `json:"token"`
	}
	p.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "k", "permissions": 15, "expires_at": time.Now().Add(time.Hour)}, &apikey, 201)
	kc := e.client()
	kc.token = apikey.Token
	if got := kc.do("GET", "/api/v1/x/mfa.webauthn/credentials", nil, nil); got != 403 {
		t.Fatalf("API key lists keys: %d", got)
	}
	if got := kc.do("POST", "/api/v1/me/mfa/webauthn/begin", map[string]any{}, nil); got != 403 {
		t.Fatalf("API key adds a key: %d", got)
	}

	// administrators reset all factors: the password alone works again
	admin.must("PUT", "/api/v1/admin/users/"+itoa(u.ID), map[string]any{"reset_mfa": true}, nil, 200)
	e.client().login("wendy", "Wendy-Passw0rd!")
}

func mustPort(raw string) string {
	u, _ := url.Parse(raw)
	return u.Port()
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
