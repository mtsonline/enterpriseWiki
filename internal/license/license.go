// Package license checks license keys offline. A key is
//
//	EWL1.<base64url(JSON payload)>.<base64url(Ed25519 signature)>
//
// signed by the licensor; the public keys are compiled in. The key does not
// lock any function: Enterprise Wiki is licensed under the Business Source
// License 1.1 and free for private use by natural persons. Organisations
// need a license; the key documents it (licensee, edition, users, term) and
// the administration shows when it has expired or too many users are active.
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Prefix starts every license key (format version 1).
const Prefix = "EWL1."

// PublicKeys verify license keys (base64, Ed25519). Several keys allow a
// key change: keys issued with the old key stay valid.
var PublicKeys = []string{
	"4EBYn9KVPNsEBmr4Clc/wTxw6QT7Vbj5IkoR6h4Vkl0=",
}

// Editions of a license.
var Editions = []string{"business", "public", "edu", "nonprofit", "trial"}

// License is the content of a key.
type License struct {
	ID       string `json:"id"`                // e.g. L-2026-0001
	Licensee string `json:"licensee"`          // organisation
	Email    string `json:"email,omitempty"`   // contact
	Edition  string `json:"edition"`           // business | public | edu | nonprofit | trial
	Users    int    `json:"users"`             // licensed active users (0 = unlimited)
	Issued   string `json:"issued"`            // YYYY-MM-DD
	Expires  string `json:"expires,omitempty"` // YYYY-MM-DD (last valid day), empty = unlimited
	Note     string `json:"note,omitempty"`
}

// Errors of Parse.
var (
	ErrFormat    = errors.New("this is not an Enterprise Wiki license key")
	ErrSignature = errors.New("the signature of the license key is invalid")
)

// Encode builds the payload part (for the issuing tool and tests).
func Encode(l License) (string, error) {
	b, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	return Prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// Sign returns a complete key (for the issuing tool and tests).
func Sign(l License, priv ed25519.PrivateKey) (string, error) {
	head, err := Encode(l)
	if err != nil {
		return "", err
	}
	return head + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, []byte(head))), nil
}

// Parse verifies a key with the compiled-in public keys.
func Parse(key string) (*License, error) { return ParseWith(key, PublicKeys) }

// ParseWith verifies a key with the given public keys (base64).
func ParseWith(key string, pubs []string) (*License, error) {
	key = strings.Join(strings.Fields(key), "") // pasted keys may contain line breaks
	if !strings.HasPrefix(key, Prefix) {
		return nil, ErrFormat
	}
	i := strings.LastIndexByte(key, '.')
	if i <= len(Prefix) {
		return nil, ErrFormat
	}
	head, sigB64 := key[:i], key[i+1:]
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, ErrFormat
	}
	ok := false
	for _, p := range pubs {
		pub, err := base64.StdEncoding.DecodeString(p)
		if err == nil && len(pub) == ed25519.PublicKeySize && ed25519.Verify(ed25519.PublicKey(pub), []byte(head), sig) {
			ok = true
			break
		}
	}
	if !ok {
		return nil, ErrSignature
	}
	payload, err := base64.RawURLEncoding.DecodeString(head[len(Prefix):])
	if err != nil {
		return nil, ErrFormat
	}
	var l License
	if err := json.Unmarshal(payload, &l); err != nil || l.Licensee == "" {
		return nil, ErrFormat
	}
	return &l, nil
}

// Status is the license state shown to administrators.
type Status struct {
	Installed   bool     `json:"installed"`
	Valid       bool     `json:"valid"` // signature ok and not expired
	Error       string   `json:"error,omitempty"`
	License     *License `json:"license,omitempty"`
	ActiveUsers int      `json:"active_users"`
	OverUsers   bool     `json:"over_users"`
	Expired     bool     `json:"expired"`
	DaysLeft    *int     `json:"days_left,omitempty"`
	// Level: ok | info (no license: private use only) | warning (expires
	// within 30 days, over the user limit) | error (invalid or expired)
	Level string `json:"level"`
}

// Evaluate checks a configured key against the number of active users.
func Evaluate(key string, activeUsers int, now time.Time) Status {
	return EvaluateWith(key, PublicKeys, activeUsers, now)
}

// EvaluateWith is Evaluate with the given public keys.
func EvaluateWith(key string, pubs []string, activeUsers int, now time.Time) Status {
	st := Status{ActiveUsers: activeUsers, Level: "ok"}
	if strings.TrimSpace(key) == "" {
		st.Level = "info"
		return st
	}
	st.Installed = true
	l, err := ParseWith(key, pubs)
	if err != nil {
		st.Error, st.Level = err.Error(), "error"
		return st
	}
	st.License, st.Valid = l, true
	if l.Expires != "" {
		end, err := time.ParseInLocation("2006-01-02", l.Expires, time.Local)
		if err != nil {
			st.Error, st.Level, st.Valid = fmt.Sprintf("invalid expiry date %q", l.Expires), "error", false
			return st
		}
		end = end.AddDate(0, 0, 1) // valid through the last day
		days := int(end.Sub(now).Hours() / 24)
		st.DaysLeft = &days
		if !now.Before(end) {
			st.Expired, st.Valid, st.Level = true, false, "error"
		} else if days < 30 {
			st.Level = "warning"
		}
	}
	if l.Users > 0 && activeUsers > l.Users {
		st.OverUsers = true
		if st.Level == "ok" {
			st.Level = "warning"
		}
	}
	return st
}
