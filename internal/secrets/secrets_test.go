package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModes(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "secrets.key")
	for _, mode := range []string{ModeAES, ModePQ} {
		b, err := Load(mode, key, "", true)
		if err != nil {
			t.Fatal(err)
		}
		v, err := b.Seal([]byte("topsecret"), "webhooks:1")
		if err != nil || !Sealed(v) || bytes.Contains(v, []byte("topsecret")) {
			t.Fatalf("%s: seal %q %v", mode, v, err)
		}
		v2, _ := b.Seal([]byte("topsecret"), "webhooks:1")
		if bytes.Equal(v, v2) {
			t.Fatalf("%s: the same value twice gives the same ciphertext", mode)
		}
		if out, err := b.Open(v, "webhooks:1"); err != nil || string(out) != "topsecret" {
			t.Fatalf("%s: open %q %v", mode, out, err)
		}
		if _, err := b.Open(v, "webhooks:2"); err == nil {
			t.Fatalf("%s: a value moved to another row was opened", mode)
		}
		bad := append([]byte{}, v...)
		bad[len(bad)-3] ^= 1
		if _, err := b.Open(bad, "webhooks:1"); err == nil {
			t.Fatalf("%s: a changed value was opened", mode)
		}
		// plain values (stored before) stay readable
		if out, _ := b.Open([]byte("old"), "x:1"); string(out) != "old" {
			t.Fatalf("%s: plain value", mode)
		}
		// mode plain with the key file reads encrypted values and stores plain
		p, err := Load(ModePlain, key, "", false)
		if err != nil {
			t.Fatal(err)
		}
		if out, err := p.Open(v, "webhooks:1"); err != nil || string(out) != "topsecret" {
			t.Fatalf("%s via plain: %q %v", mode, out, err)
		}
		if out, _ := p.Seal([]byte("x"), "a:1"); string(out) != "x" {
			t.Fatal("plain seals nothing")
		}
	}
	// without the key file nothing encrypted can be read
	if _, err := Plain().Open([]byte(prefixAES+"AAAA"), "x"); err != ErrNoKey {
		t.Fatalf("no key: %v", err)
	}
	if _, err := Load(ModeAES, filepath.Join(dir, "missing.key"), "", false); err == nil {
		t.Fatal("missing key file accepted")
	}
	if _, err := Load("rot13", key, "", false); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestKeyFile(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "k")
	if err := NewKeyFile(key, "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if err := NewKeyFile(key, ""); err == nil {
		t.Fatal("an existing key file was replaced")
	}
	raw, _ := os.ReadFile(key)
	if !strings.Contains(string(raw), "argon2id") || strings.Contains(string(raw), `"aes"`) {
		t.Fatalf("wrapped key file shows its keys: %s", raw)
	}
	if _, err := Load(ModePQ, key, "", false); err == nil {
		t.Fatal("wrapped key file opened without password")
	}
	if _, err := Load(ModePQ, key, "wrong", false); err == nil || !strings.Contains(err.Error(), "wrong password") {
		t.Fatalf("wrong password: %v", err)
	}
	b, err := Load(ModePQ, key, "correct horse battery staple", false)
	if err != nil || !b.HasKey() {
		t.Fatal(err)
	}
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(ModePQ, key, "correct horse battery staple", false); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("readable key file: %v", err)
	}
}
