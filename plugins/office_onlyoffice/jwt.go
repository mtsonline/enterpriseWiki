package office_onlyoffice

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var b64 = base64.RawURLEncoding

// signJWT returns an HS256 JWT of claims.
func signJWT(claims any, secret string) (string, error) {
	head := b64.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	msg := head + "." + b64.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	return msg + "." + b64.EncodeToString(mac.Sum(nil)), nil
}

var errJWT = errors.New("invalid JWT")

// verifyJWT checks an HS256 JWT and decodes its claims into v. Only HS256
// is accepted (no "none", no algorithm confusion); exp and nbf are honoured.
func verifyJWT(tok, secret string, v any) error {
	if secret == "" || len(tok) > 1<<20 {
		return errJWT
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return errJWT
	}
	hb, err := b64.DecodeString(parts[0])
	if err != nil {
		return errJWT
	}
	var head struct {
		Alg string `json:"alg"`
	}
	if json.Unmarshal(hb, &head) != nil || head.Alg != "HS256" {
		return errJWT
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return errJWT
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return errJWT
	}
	body, err := b64.DecodeString(parts[1])
	if err != nil {
		return errJWT
	}
	var std struct {
		Exp *float64 `json:"exp"`
		Nbf *float64 `json:"nbf"`
	}
	if json.Unmarshal(body, &std) != nil {
		return errJWT
	}
	now := float64(time.Now().Unix())
	if (std.Exp != nil && now > *std.Exp+60) || (std.Nbf != nil && now < *std.Nbf-60) {
		return errJWT
	}
	if v != nil && json.Unmarshal(body, v) != nil {
		return errJWT
	}
	return nil
}
