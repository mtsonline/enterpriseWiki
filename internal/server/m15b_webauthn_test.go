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

	"github.com/mtsonline/enterprisewiki/internal/auth"
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

	// register a passkey (user verification required)
	var opts map[string]any
	w.must("POST", "/api/v1/x/mfa.webauthn/register/begin", map[string]any{"passkey": true}, &opts, 200)
	sel := opts["publicKey"].(map[string]any)["authenticatorSelection"].(map[string]any)
	if sel["residentKey"] != "required" || sel["userVerification"] != "required" {
		t.Fatalf("passkey options %v", sel)
	}
	w.must("POST", "/api/v1/x/mfa.webauthn/register/finish", map[string]any{"name": "Laptop", "credential": key.create(opts)}, nil, 201)
	// the same registration cannot be finished twice; the same key not registered twice
	if got := w.do("POST", "/api/v1/x/mfa.webauthn/register/finish", map[string]any{"name": "Again", "credential": key.create(opts)}, nil); got != 400 {
		t.Fatalf("registration reused: %d", got)
	}
	var list []map[string]any
	w.must("GET", "/api/v1/x/mfa.webauthn/credentials", nil, &list, 200)
	if len(list) != 1 || list[0]["passkey"] != true {
		t.Fatalf("credentials %v", list)
	}

	// password sign-in now needs the key; a code given with the password does not replace it
	d := e.client()
	if st, out := d.rawJSON("POST", "/api/v1/auth/login", map[string]string{"username": "wendy", "password": "Wendy-Passw0rd!", "totp": "123456"}); st != 401 ||
		out["error"].(map[string]any)["code"] != "mfa_required" {
		t.Fatalf("inline code instead of the key: %d %v", st, out)
	}
	tok := d.pendingMFA("wendy", "Wendy-Passw0rd!")
	var ch map[string]any
	d.must("POST", "/api/v1/auth/mfa/challenge", map[string]any{"token": tok, "method": "webauthn"}, &ch, 200)
	if allow := ch["publicKey"].(map[string]any)["allowCredentials"].([]any); len(allow) != 1 {
		t.Fatalf("allowCredentials %v", allow)
	}
	// a signature by another key is refused
	other := newAuthenticator(t, origin)
	other.credID = key.credID
	if got := d.do("POST", "/api/v1/auth/mfa/verify", map[string]any{"token": tok, "method": "webauthn", "credential": other.get(ch)}, nil); got != 401 {
		t.Fatalf("foreign signature: %d", got)
	}
	d.must("POST", "/api/v1/auth/mfa/verify", map[string]any{"token": tok, "method": "webauthn", "credential": key.get(ch)}, nil, 200)
	if !signedIn(d) {
		t.Fatal("not signed in with the security key")
	}

	// passkey: sign-in without password
	p := e.client()
	var begin struct {
		Token   string         `json:"token"`
		Options map[string]any `json:"options"`
	}
	p.must("POST", "/api/v1/auth/passwordless/passkey/begin", nil, &begin, 200)
	p.must("POST", "/api/v1/auth/passwordless/passkey/finish", map[string]any{"token": begin.Token, "credential": key.get(begin.Options)}, nil, 200)
	if !signedIn(p) {
		t.Fatal("not signed in with the passkey")
	}
	// without user verification the passkey is refused
	q := e.client()
	q.must("POST", "/api/v1/auth/passwordless/passkey/begin", nil, &begin, 200)
	key.flags = flagUP
	if got := q.do("POST", "/api/v1/auth/passwordless/passkey/finish", map[string]any{"token": begin.Token, "credential": key.get(begin.Options)}, nil); got != 401 || signedIn(q) {
		t.Fatalf("passkey without user verification: %d", got)
	}
	key.flags = flagUP | flagUV
	// a signature from another origin is refused
	q.must("POST", "/api/v1/auth/passwordless/passkey/begin", nil, &begin, 200)
	key.origin = "https://evil.example"
	if got := q.do("POST", "/api/v1/auth/passwordless/passkey/finish", map[string]any{"token": begin.Token, "credential": key.get(begin.Options)}, nil); got != 401 {
		t.Fatalf("other origin: %d", got)
	}
	key.origin = origin
	// a counter going backwards (cloned key) is refused
	q.must("POST", "/api/v1/auth/passwordless/passkey/begin", nil, &begin, 200)
	key.counter = 0
	if got := q.do("POST", "/api/v1/auth/passwordless/passkey/finish", map[string]any{"token": begin.Token, "credential": key.get(begin.Options)}, nil); got != 401 || signedIn(q) {
		t.Fatalf("cloned key: %d", got)
	}

	// changing keys needs a recent sign-in
	if _, err := e.app.DB.Exec(t.Context(), `UPDATE sessions SET created_at = now() - interval '1 hour' WHERE user_id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	e.app.Bus.Publish(t.Context(), "auth", "")
	if st, out := w.rawJSON("POST", "/api/v1/x/mfa.webauthn/register/begin", map[string]any{"passkey": false}); st != 403 || out["error"].(map[string]any)["code"] != "reauth_required" {
		t.Fatalf("old session adds a key: %d %v", st, out)
	}
	// API keys cannot manage keys
	var apikey struct {
		Token string `json:"token"`
	}
	d.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "k", "permissions": 15, "expires_at": time.Now().Add(time.Hour)}, &apikey, 201)
	kc := e.client()
	kc.token = apikey.Token
	if got := kc.do("GET", "/api/v1/x/mfa.webauthn/credentials", nil, nil); got != 403 {
		t.Fatalf("API key lists keys: %d", got)
	}

	// administrators reset all second factors: the password alone works again
	admin.must("PUT", "/api/v1/admin/users/"+itoa(u.ID), map[string]any{"reset_totp": true}, nil, 200)
	e.client().login("wendy", "Wendy-Passw0rd!")
}

func mustPort(raw string) string {
	u, _ := url.Parse(raw)
	return u.Port()
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// TestMFAAfterSSO: the second factor of the wiki also after single
// sign-on, unless the provider does it (mfa: false).
func TestMFAAfterSSO(t *testing.T) {
	e := setup(t)
	idp := newFakeOIDC(t)
	e.setPluginSettings(t, "auth.oidc", map[string]any{"issuer": idp.srv.URL, "client_id": "wiki", "client_secret": "s3cret"})
	c := e.client()
	idp.login(t, e, c)
	// carol sets up the authenticator app
	var setupRes struct {
		Secret string `json:"secret"`
	}
	c.must("POST", "/api/v1/me/totp/setup", nil, &setupRes, 200)
	code, _ := auth.TOTPCode(setupRes.Secret, time.Now().Unix()/30)
	c.must("POST", "/api/v1/me/totp/enable", map[string]string{"code": code}, nil, 204)

	var st struct {
		Applies bool     `json:"applies"`
		Methods []string `json:"methods"`
	}
	c.must("GET", "/api/v1/me/mfa", nil, &st, 200)
	if !st.Applies || len(st.Methods) != 1 || st.Methods[0] != "totp" {
		t.Fatalf("mfa status %+v", st)
	}

	// the next single sign-on waits for the code
	d := e.client()
	if loc := idp.loginAt(t, e, d, "oidc"); loc != "/login?mfa=1" {
		t.Fatalf("after SSO: %s", loc)
	}
	if signedIn(d) {
		t.Fatal("signed in without the second factor")
	}
	var info struct {
		Methods []string `json:"methods"`
	}
	d.must("POST", "/api/v1/auth/mfa/info", map[string]string{}, &info, 200) // token from the cookie
	if len(info.Methods) != 1 || info.Methods[0] != "totp" {
		t.Fatalf("methods %v", info.Methods)
	}
	next, _ := auth.TOTPCode(setupRes.Secret, time.Now().Unix()/30+1)
	var done struct {
		Next string `json:"next"`
	}
	d.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"method": "totp", "code": next}, &done, 200)
	if !signedIn(d) || done.Next != "/" {
		t.Fatalf("after the second factor: %v %+v", signedIn(d), done)
	}
	// another browser without the cookie has no pending sign-in
	if got := e.client().do("POST", "/api/v1/auth/mfa/info", map[string]string{}, nil); got != 401 {
		t.Fatalf("pending sign-in without the cookie: %d", got)
	}

	// the provider does the second factor: the wiki does not ask
	e.setPluginSettings(t, "auth.oidc", map[string]any{"issuer": idp.srv.URL, "client_id": "wiki", "client_secret": "s3cret", "mfa": false})
	f := e.client()
	if loc := idp.loginAt(t, e, f, "oidc"); loc != "/" || !signedIn(f) {
		t.Fatalf("mfa false: %s", loc)
	}
	f.must("GET", "/api/v1/me/mfa", nil, &st, 200)
	if st.Applies {
		t.Fatal("factor offered although the provider does it")
	}
	if got := f.do("POST", "/api/v1/me/totp/setup", nil, nil); got != 400 {
		t.Fatalf("setup with mfa false: %d", got)
	}
}
