package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default, used by all authenticator apps
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// TOTP (RFC 6238): 6 digits, 30 second steps, SHA-1, ±1 step tolerance.

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random base32 secret (160 bit).
func NewTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b32.EncodeToString(b)
}

// TOTPCode computes the code for a time step.
func TOTPCode(secret string, step int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000), nil
}

// VerifyTOTP checks code against now ±1 step and returns the matched step.
// Steps <= lastStep are rejected (replay protection).
func VerifyTOTP(secret, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0, false
	}
	cur := now.Unix() / 30
	for _, s := range []int64{cur - 1, cur, cur + 1} {
		if s <= lastStep {
			continue
		}
		c, err := TOTPCode(secret, s)
		if err == nil && hmac.Equal([]byte(c), []byte(code)) {
			return s, true
		}
	}
	return 0, false
}

// ErrTOTPRequired is returned by Login when the password was correct but a
// two-factor code is needed.
var ErrTOTPRequired = &apperr.Error{Status: 401, Code: "totp_required", Msg: "two-factor code required"}

// ErrTOTPInvalid is returned for a wrong two-factor code.
var ErrTOTPInvalid = &apperr.Error{Status: 401, Code: "totp_invalid", Msg: "invalid two-factor code"}

// checkTOTP verifies the second factor of a local account (if enabled).
func (s *Service) checkTOTP(ctx context.Context, uid int64, code string) error {
	var secret *string
	var enabled bool
	var last int64
	if err := s.pool.QueryRow(ctx, `SELECT totp_secret, totp_enabled, totp_last_step FROM users WHERE id=$1`, uid).Scan(&secret, &enabled, &last); err != nil {
		return err
	}
	if !enabled || secret == nil {
		return nil
	}
	if strings.TrimSpace(code) == "" {
		return ErrTOTPRequired
	}
	step, ok := VerifyTOTP(*secret, code, time.Now(), last)
	if !ok {
		return ErrTOTPInvalid
	}
	_, err := s.pool.Exec(ctx, `UPDATE users SET totp_last_step=$2 WHERE id=$1`, uid, step)
	return err
}

// TOTPSetup is returned when two-factor authentication is being set up.
type TOTPSetup struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// TOTPStatus reports whether the caller has 2FA enabled.
func (s *Service) TOTPStatus(ctx context.Context) (bool, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return false, apperr.ErrUnauthorized
	}
	var on bool
	err := s.pool.QueryRow(ctx, `SELECT totp_enabled FROM users WHERE id=$1`, p.UserID).Scan(&on)
	return on, err
}

// BeginTOTP creates a new (not yet active) secret for the caller's local account.
func (s *Service) BeginTOTP(ctx context.Context, issuer string) (*TOTPSetup, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return nil, apperr.ErrUnauthorized
	}
	if p.Provider != "local" {
		return nil, apperr.BadRequest("two-factor authentication is managed by %s", p.Provider)
	}
	var on bool
	if err := s.pool.QueryRow(ctx, `SELECT totp_enabled FROM users WHERE id=$1`, p.UserID).Scan(&on); err != nil {
		return nil, err
	}
	if on {
		return nil, apperr.Conflict("two-factor authentication is already enabled")
	}
	secret := NewTOTPSecret()
	if _, err := s.pool.Exec(ctx, `UPDATE users SET totp_secret=$2, totp_last_step=0 WHERE id=$1`, p.UserID, secret); err != nil {
		return nil, err
	}
	label := url.PathEscape(issuer + ":" + p.Username)
	uri := fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30", label, secret, url.QueryEscape(issuer))
	return &TOTPSetup{Secret: secret, URI: uri}, nil
}

// EnableTOTP activates 2FA after the user proved possession with a code.
func (s *Service) EnableTOTP(ctx context.Context, code string) error {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return apperr.ErrUnauthorized
	}
	var secret *string
	var last int64
	if err := s.pool.QueryRow(ctx, `SELECT totp_secret, totp_last_step FROM users WHERE id=$1 AND provider='local'`, p.UserID).Scan(&secret, &last); err != nil || secret == nil {
		return apperr.BadRequest("start the setup first")
	}
	step, ok := VerifyTOTP(*secret, code, time.Now(), last)
	if !ok {
		return ErrTOTPInvalid
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET totp_enabled=true, totp_last_step=$2 WHERE id=$1`, p.UserID, step); err != nil {
		return err
	}
	return audit.Log(ctx, s.pool, "user.totp_enable", "user", p.UserID, nil, nil)
}

// DisableTOTP switches 2FA off; a valid current code is required.
func (s *Service) DisableTOTP(ctx context.Context, code string) error {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return apperr.ErrUnauthorized
	}
	if err := s.checkTOTP(ctx, p.UserID, code); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET totp_enabled=false, totp_secret=NULL WHERE id=$1`, p.UserID); err != nil {
		return err
	}
	return audit.Log(ctx, s.pool, "user.totp_disable", "user", p.UserID, nil, nil)
}
