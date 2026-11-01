package auth

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"
)

// SSHNamespace is the namespace clients must use with `ssh-keygen -Y sign -n`.
const SSHNamespace = "ewiki-login"

// ParseAuthorizedKey parses an OpenSSH public key line and returns the key and
// its SHA256 fingerprint.
func ParseAuthorizedKey(line string) (ssh.PublicKey, string, error) {
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(line)))
	if err != nil {
		return nil, "", errors.New("invalid ssh public key")
	}
	switch pk.Type() {
	case ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoRSA,
		ssh.KeyAlgoSKED25519, ssh.KeyAlgoSKECDSA256:
	default:
		return nil, "", errors.New("unsupported ssh key type " + pk.Type())
	}
	return pk, ssh.FingerprintSHA256(pk), nil
}

type sshsigBlob struct {
	PublicKey     []byte
	Namespace     string
	Reserved      string
	HashAlgorithm string
	Signature     []byte
}

func readString(b []byte) ([]byte, []byte, error) {
	if len(b) < 4 {
		return nil, nil, errors.New("short buffer")
	}
	n := binary.BigEndian.Uint32(b)
	if uint32(len(b)-4) < n {
		return nil, nil, errors.New("short buffer")
	}
	return b[4 : 4+n], b[4+n:], nil
}

func writeString(buf *bytes.Buffer, s []byte) {
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(s)))
	buf.Write(l[:])
	buf.Write(s)
}

// ParseSSHSignature decodes an armored SSHSIG signature (output of ssh-keygen -Y sign).
func ParseSSHSignature(armored string) (*sshsigBlob, error) {
	s := strings.TrimSpace(armored)
	s = strings.TrimPrefix(s, "-----BEGIN SSH SIGNATURE-----")
	s = strings.TrimSuffix(s, "-----END SSH SIGNATURE-----")
	s = strings.Join(strings.Fields(s), "")
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New("signature is not valid base64")
	}
	if len(raw) < 10 || string(raw[:6]) != "SSHSIG" {
		return nil, errors.New("not an SSHSIG signature")
	}
	if binary.BigEndian.Uint32(raw[6:10]) != 1 {
		return nil, errors.New("unsupported SSHSIG version")
	}
	rest := raw[10:]
	var fields [5][]byte
	for i := range fields {
		fields[i], rest, err = readString(rest)
		if err != nil {
			return nil, errors.New("malformed SSHSIG")
		}
	}
	return &sshsigBlob{fields[0], string(fields[1]), string(fields[2]), string(fields[3]), fields[4]}, nil
}

// VerifySSHSignature verifies that armored is a valid signature by pub over
// message in the given namespace.
func VerifySSHSignature(pub ssh.PublicKey, armored, namespace string, message []byte) error {
	sig, err := ParseSSHSignature(armored)
	if err != nil {
		return err
	}
	if sig.Namespace != namespace {
		return errors.New("wrong signature namespace")
	}
	if !bytes.Equal(sig.PublicKey, pub.Marshal()) {
		return errors.New("signature made by a different key")
	}
	var digest []byte
	switch sig.HashAlgorithm {
	case "sha256":
		h := sha256.Sum256(message)
		digest = h[:]
	case "sha512":
		h := sha512.Sum512(message)
		digest = h[:]
	default:
		return errors.New("unsupported hash algorithm")
	}
	var signed bytes.Buffer
	signed.WriteString("SSHSIG")
	writeString(&signed, []byte(sig.Namespace))
	writeString(&signed, []byte(sig.Reserved))
	writeString(&signed, []byte(sig.HashAlgorithm))
	writeString(&signed, digest)
	s := new(ssh.Signature)
	if err := ssh.Unmarshal(sig.Signature, s); err != nil {
		return errors.New("malformed signature")
	}
	if err := pub.Verify(signed.Bytes(), s); err != nil {
		return errors.New("signature verification failed")
	}
	return nil
}
