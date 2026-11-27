package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (OWASP recommendation: m=19 MiB, t=2, p=1).
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
)

// HashPassword returns a PHC formatted Argon2id hash.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks pw against a PHC Argon2id hash in constant time.
func VerifyPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	b64 := base64.RawStdEncoding
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	// bounds: a manipulated hash must not exhaust memory or CPU
	if err1 != nil || err2 != nil || len(want) < 16 || len(want) > 128 || m == 0 || m > 1<<20 || t == 0 || t > 16 || p == 0 || p > 64 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want))) // #nosec G115 -- len(want) <= 128 (checked above)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is verified for unknown users so that timing does not reveal
// whether an account exists.
var dummyHash, _ = HashPassword("timing-equalizer-not-a-real-password")

// CheckPasswordPolicy validates a new password.
func CheckPasswordPolicy(pw string, minLen int) error {
	if len([]rune(pw)) < minLen {
		return fmt.Errorf("password must have at least %d characters", minLen)
	}
	if len(pw) > 1024 {
		return errors.New("password too long")
	}
	var classes int
	var lower, upper, digit, other bool
	for _, r := range pw {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		default:
			other = true
		}
	}
	for _, b := range []bool{lower, upper, digit, other} {
		if b {
			classes++
		}
	}
	if classes < 3 {
		return errors.New("password must contain at least three of: lowercase, uppercase, digits, symbols")
	}
	return nil
}

// TokenBytes is the randomness of a token: 384 bits, far more than can
// ever be guessed (96 hexadecimal digits).
const TokenBytes = 48

// NewToken returns a random token with the given prefix, for sessions,
// links in e-mails, feeds and other secrets that the server compares
// itself.
//
// Only digits and a-f (hexadecimal): these never look like an attack to a
// web application firewall, so no exception for them is needed. Found
// 27.11.2026: about one in 20,000 base64url tokens started with a digit and
// "--", which the OWASP Core Rule Set reads as an SQL comment (rule 942100);
// a session cookie like that had every request of its person refused.
// Digits and a-f form no SQL comment and no SQL keyword (TestTokensPassWAF).
func NewToken(prefix string) string { return newHexToken(prefix, TokenBytes) }

// NewShortToken is NewToken with 256 bits (64 hexadecimal digits), for
// values that a standard limits: the state of a sign-in travels as SAML
// RelayState, which may have at most 80 bytes (SAML bindings, 3.4.3). Such a
// value is used once and expires after minutes.
func NewShortToken(prefix string) string { return newHexToken(prefix, 32) }

func newHexToken(prefix string, n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}

// HashToken returns the SHA-256 of a token; only hashes are stored.
func HashToken(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}
