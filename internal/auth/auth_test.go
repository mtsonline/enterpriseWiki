package auth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("Correct-Horse-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("hash %s", h)
	}
	if !VerifyPassword(h, "Correct-Horse-1") || VerifyPassword(h, "correct-horse-1") {
		t.Fatal("verify")
	}
	h2, _ := HashPassword("Correct-Horse-1")
	if h == h2 {
		t.Fatal("salt not random")
	}
	if VerifyPassword("garbage", "x") {
		t.Fatal("garbage hash verified")
	}
}

func TestPasswordPolicy(t *testing.T) {
	for _, pw := range []string{"short1A!", "alllowercaseletters", "ALLUPPERCASE1234"} {
		if CheckPasswordPolicy(pw, 12) == nil {
			t.Errorf("weak password %q accepted", pw)
		}
	}
	if err := CheckPasswordPolicy("Good-Password-42", 12); err != nil {
		t.Error(err)
	}
}

func sign(s ssh.Signer, msg []byte, ns string) string {
	ws := func(b *bytes.Buffer, v []byte) {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(v)))
		b.Write(l[:])
		b.Write(v)
	}
	h := sha512.Sum512(msg)
	var signed bytes.Buffer
	signed.WriteString("SSHSIG")
	ws(&signed, []byte(ns))
	ws(&signed, nil)
	ws(&signed, []byte("sha512"))
	ws(&signed, h[:])
	sig, _ := s.Sign(rand.Reader, signed.Bytes())
	var blob bytes.Buffer
	blob.WriteString("SSHSIG")
	binary.Write(&blob, binary.BigEndian, uint32(1)) //nolint:errcheck
	ws(&blob, s.PublicKey().Marshal())
	ws(&blob, []byte(ns))
	ws(&blob, nil)
	ws(&blob, []byte("sha512"))
	ws(&blob, ssh.Marshal(sig))
	return "-----BEGIN SSH SIGNATURE-----\n" + base64.StdEncoding.EncodeToString(blob.Bytes()) + "\n-----END SSH SIGNATURE-----\n"
}

func TestSSHSignature(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s, _ := ssh.NewSignerFromKey(priv)
	msg := []byte("ewiki-ssh-challenge")
	armored := sign(s, msg, SSHNamespace)
	if err := VerifySSHSignature(s.PublicKey(), armored, SSHNamespace, msg); err != nil {
		t.Fatal(err)
	}
	if VerifySSHSignature(s.PublicKey(), armored, SSHNamespace, []byte("other")) == nil {
		t.Fatal("signature over other message accepted")
	}
	if VerifySSHSignature(s.PublicKey(), sign(s, msg, "git"), SSHNamespace, msg) == nil {
		t.Fatal("wrong namespace accepted")
	}
	_, priv2, _ := ed25519.GenerateKey(rand.Reader)
	s2, _ := ssh.NewSignerFromKey(priv2)
	if VerifySSHSignature(s2.PublicKey(), armored, SSHNamespace, msg) == nil {
		t.Fatal("signature of another key accepted")
	}
}

// TestSSHSignatureOpenSSH verifies compatibility with real `ssh-keygen -Y sign`.
func TestSSHSignatureOpenSSH(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not installed")
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "id")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	pubLine, _ := os.ReadFile(key + ".pub")
	pub, _, err := ParseAuthorizedKey(string(pubLine))
	if err != nil {
		t.Fatal(err)
	}
	msg := "ewiki-ssh-abc123"
	cmd := exec.Command("ssh-keygen", "-Y", "sign", "-q", "-f", key, "-n", SSHNamespace, "-")
	cmd.Stdin = strings.NewReader(msg)
	sig, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySSHSignature(pub, string(sig), SSHNamespace, []byte(msg)); err != nil {
		t.Fatal(err)
	}
}

func TestParseAuthorizedKey(t *testing.T) {
	if _, _, err := ParseAuthorizedKey("not a key"); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter()
	for i := 0; i < 3; i++ {
		if !l.allow("k", 3, 60e9) {
			t.Fatal("blocked too early")
		}
	}
	if l.allow("k", 3, 60e9) {
		t.Fatal("not blocked")
	}
}
