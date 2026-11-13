// Package signature_passkey signs records with a passkey or security key
// (WebAuthn): the challenge of the key is the SHA-256 digest of the record
// followed by a random value, so the signature of the key covers exactly
// that record. User verification (PIN, biometrics) is required. Level:
// advanced electronic signature.
//
// The proof holds credential id, COSE public key, authenticator data,
// client data and signature; Verify checks it without the wiki.
package signature_passkey

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/go-webauthn/webauthn/protocol/webauthncose"

	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/signature"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// ID is the id of the plugin and the method.
const ID = "signature.passkey"

// Plugin is the passkey signature plugin.
type Plugin struct{}

func init() {
	sdk.Register(&Plugin{})
	signature.RegisterVerifier(ID, Verify)
}

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Setup registers the method.
func (p *Plugin) Setup(host *sdk.Host) error {
	host.RegisterSignatureMethod(&method{host: host})
	return nil
}

type method struct{ host *sdk.Host }

func (m *method) ID() string    { return ID }
func (m *method) Level() string { return signature.LevelAdvanced }
func (m *method) Label(lang string) string {
	return m.host.I18n.T(context.Background(), lang, "signature.passkey.label", nil)
}

func (m *method) signer() auth.KeySigner { return m.host.Auth.KeySigner() }

func (m *method) Available(ctx context.Context, uid int64) bool {
	k := m.signer()
	return k != nil && k.HasKey(ctx, uid)
}

type state struct {
	Challenge []byte `json:"challenge"`
	Session   string `json:"session"`
}

// Begin: the challenge is digest || 16 random bytes.
func (m *method) Begin(ctx context.Context, req signature.Request) (signature.Challenge, string, error) {
	k := m.signer()
	if k == nil {
		return signature.Challenge{}, "", errors.New("security keys are switched off")
	}
	if len(req.Digest) != sha256.Size {
		return signature.Challenge{}, "", errors.New("digest must be SHA-256")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return signature.Challenge{}, "", err
	}
	challenge := append(append([]byte{}, req.Digest...), nonce...)
	opts, sess, err := k.SignBegin(ctx, req.UserID, challenge)
	if err != nil {
		return signature.Challenge{}, "", err
	}
	st, err := json.Marshal(state{Challenge: challenge, Session: sess})
	return signature.Challenge{Kind: "webauthn", Public: opts}, string(st), err
}

// Proof is what the passkey signature keeps.
type Proof struct {
	auth.KeyProof
	Challenge []byte `json:"challenge"`
}

func (m *method) Finish(ctx context.Context, req signature.Request, st string, answer json.RawMessage) (json.RawMessage, error) {
	k := m.signer()
	if k == nil {
		return nil, errors.New("security keys are switched off")
	}
	var s state
	if err := json.Unmarshal([]byte(st), &s); err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(s.Challenge, req.Digest) {
		return nil, errors.New("challenge does not belong to this record")
	}
	kp, err := k.SignFinish(ctx, req.UserID, s.Session, answer)
	if err != nil {
		return nil, err
	}
	proof := Proof{KeyProof: *kp, Challenge: s.Challenge}
	if cs := Verify(req.Digest, mustJSON(proof)); !signature.Valid(cs) {
		return nil, fmt.Errorf("proof does not verify: %+v", cs)
	}
	return json.Marshal(proof)
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// Verify checks a passkey proof offline: the challenge holds the digest,
// the client data is a WebAuthn assertion over that challenge from an
// allowed origin, the authenticator data belongs to the relying party and
// shows user verification, and the signature verifies with the public key.
func Verify(digest []byte, raw json.RawMessage) []signature.Check {
	var p Proof
	if err := json.Unmarshal(raw, &p); err != nil {
		return []signature.Check{{Name: "signature.passkey.check.proof", OK: false, Detail: err.Error()}}
	}
	out := []signature.Check{{Name: "signature.passkey.check.challenge", OK: len(digest) == sha256.Size && bytes.HasPrefix(p.Challenge, digest) && len(p.Challenge) >= len(digest)+16}}
	var cd struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
	}
	cdOK := json.Unmarshal(p.ClientDataJSON, &cd) == nil && cd.Type == "webauthn.get" &&
		cd.Challenge == base64.RawURLEncoding.EncodeToString(p.Challenge) && slices.Contains(p.Origins, cd.Origin)
	out = append(out, signature.Check{Name: "signature.passkey.check.client_data", OK: cdOK, Detail: cd.Origin})
	rp := sha256.Sum256([]byte(p.RPID))
	adOK := len(p.AuthenticatorData) >= 37 && bytes.Equal(p.AuthenticatorData[:32], rp[:]) &&
		p.AuthenticatorData[32]&0x01 != 0 && p.AuthenticatorData[32]&0x04 != 0 // user present and verified
	out = append(out, signature.Check{Name: "signature.passkey.check.authenticator", OK: adOK, Detail: p.RPID})
	cdHash := sha256.Sum256(p.ClientDataJSON)
	signed := append(append([]byte{}, p.AuthenticatorData...), cdHash[:]...)
	key, err := webauthncose.ParsePublicKey(p.PublicKey)
	sigOK := false
	if err == nil {
		sigOK, err = webauthncose.VerifySignature(key, signed, p.Signature)
	}
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	return append(out, signature.Check{Name: "signature.passkey.check.signature", OK: err == nil && sigOK, Detail: detail})
}
