package mfa_totp

import (
	"testing"
	"time"
)

func TestTOTPRFC6238(t *testing.T) {
	// RFC 6238 test secret "12345678901234567890"
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for ts, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924"} {
		got, err := TOTPCode(secret, ts/30)
		if err != nil || got != want {
			t.Errorf("t=%d: %s, want %s", ts, got, want)
		}
	}
	now := time.Unix(1234567890, 0)
	if _, ok := VerifyTOTP(secret, "005924", now, 0); !ok {
		t.Fatal("valid code rejected")
	}
	if _, ok := VerifyTOTP(secret, "005924", now, now.Unix()/30); ok {
		t.Fatal("replay accepted")
	}
	if _, ok := VerifyTOTP(secret, "123456", now, 0); ok {
		t.Fatal("wrong code accepted")
	}
}
