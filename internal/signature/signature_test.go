package signature

import (
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func TestCanonical(t *testing.T) {
	a, da, err := Canonical(map[string]any{"b": 1, "a": map[string]any{"z": "x", "y": 12345678901234567}})
	if err != nil {
		t.Fatal(err)
	}
	b, db, _ := Canonical(json.RawMessage(`{ "a": {"y": 12345678901234567, "z": "x"}, "b": 1 }`))
	if string(a) != `{"a":{"y":12345678901234567,"z":"x"},"b":1}` || string(a) != string(b) || hex.EncodeToString(da) != hex.EncodeToString(db) {
		t.Fatalf("%s / %s", a, b)
	}
}

// a sealed export verifies; any change to record, signer or seal does not
func TestSealVerify(t *testing.T) {
	RegisterVerifier("test.ok", func(digest []byte, proof json.RawMessage) []Check {
		return []Check{{Name: "test", OK: string(proof) == `{"ok":true}`}}
	})
	seed := make([]byte, ed25519.SeedSize+mldsa.PrivateKeySize)
	_, _ = rand.Read(seed)
	k, err := pair(seed)
	if err != nil {
		t.Fatal(err)
	}
	k.id, k.pub.ID = 7, 7
	rec, digest, _ := Canonical(map[string]any{"decision": "approve", "note": "fine"})
	e := Evidence{Version: 1, Purpose: "p", Ref: "r:1", Signer: Signer{ID: 3, Name: "Ana"}, Method: "test.ok", Level: LevelAdvanced,
		Digest: hex.EncodeToString(digest), Record: rec, Proof: json.RawMessage(`{"ok":true}`), SignedAt: time.Now().UTC().Truncate(time.Millisecond)}
	e.Seal.Key = k.id
	msg, _ := e.sealed()
	e.Seal.Ed25519 = ed25519.Sign(k.ed, msg)
	e.Seal.MLDSA, err = k.ml.Sign(rand.Reader, msg, &mldsa.Options{Context: sealContext})
	if err != nil {
		t.Fatal(err)
	}
	x := Export{Evidence: e, Key: k.pub}
	if cs := Verify(&x, []PublicKey{k.pub}); !Valid(cs) {
		t.Fatalf("valid export: %+v", cs)
	}
	// through JSON (as exported) it still verifies
	b, _ := json.Marshal(x)
	var y Export
	_ = json.Unmarshal(b, &y)
	if cs := Verify(&y, []PublicKey{k.pub}); !Valid(cs) {
		t.Fatalf("after JSON: %+v", cs)
	}
	tamper := func(name string, f func(*Export)) {
		z := y
		z.Record = append(json.RawMessage{}, y.Record...)
		f(&z)
		if Valid(Verify(&z, []PublicKey{k.pub})) {
			t.Fatalf("%s: still valid", name)
		}
	}
	tamper("record", func(z *Export) { z.Record = json.RawMessage(`{"decision":"approve","note":"other"}`) })
	tamper("signer", func(z *Export) { z.Signer.Name = "Bob" })
	tamper("ed25519", func(z *Export) { s := append([]byte{}, z.Seal.Ed25519...); s[0] ^= 1; z.Seal.Ed25519 = s })
	tamper("ml-dsa", func(z *Export) { s := append([]byte{}, z.Seal.MLDSA...); s[3] ^= 1; z.Seal.MLDSA = s })
	tamper("proof", func(z *Export) { z.Proof = json.RawMessage(`{"ok":false}`) })
	// a key that is not this wiki's
	other, _ := pair(append(make([]byte, 0), make([]byte, len(seed))...))
	if Valid(Verify(&y, []PublicKey{{ID: 7, Ed25519: other.pub.Ed25519, MLDSA: other.pub.MLDSA}})) {
		t.Fatal("foreign key accepted")
	}
}
