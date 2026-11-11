package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// ErrEmailCodeSent is returned by Login when the password was correct and a
// code was e-mailed as second factor; repeat the login with the code.
var ErrEmailCodeSent = &apperr.Error{Status: 401, Code: "mfa_email_sent", Msg: "a sign-in code was sent by e-mail"}

// ErrTOTPOrEmail is returned by Login when an authenticator code is needed
// and a code by e-mail can be requested instead (login with mfa "email").
var ErrTOTPOrEmail = &apperr.Error{Status: 401, Code: "totp_required_email", Msg: "two-factor code required (or request one by e-mail)"}

// ErrNetworkDenied is returned when an account is used from an address
// outside its allowed networks.
var ErrNetworkDenied = &apperr.Error{Status: 403, Code: "network_denied", Msg: "this account cannot be used from your network"}

const (
	codeTTL      = 10 * time.Minute
	codeAttempts = 5
)

// ParseNetworks validates a list of IP addresses and CIDR networks and
// returns them normalized (10.0.0.0/8, 192.0.2.7/32, 2001:db8::/32).
func ParseNetworks(list []string) ([]string, error) {
	out := []string{}
	for _, n := range list {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		var p netip.Prefix
		if strings.Contains(n, "/") {
			var err error
			if p, err = netip.ParsePrefix(n); err != nil {
				return nil, apperr.BadRequest("invalid network %q (use e.g. 10.0.0.0/8 or 2001:db8::/32)", n)
			}
		} else {
			a, err := netip.ParseAddr(n)
			if err != nil {
				return nil, apperr.BadRequest("invalid IP address %q", n)
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		s := p.Masked().String()
		if !containsStr(out, s) {
			out = append(out, s)
		}
	}
	if len(out) > 50 {
		return nil, apperr.BadRequest("at most 50 networks")
	}
	return out, nil
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// IPAllowed reports whether ip lies in one of the networks (no networks:
// everywhere). Unknown or invalid addresses are not allowed when networks
// are set.
func IPAllowed(networks []string, ip string) bool {
	if len(networks) == 0 {
		return true
	}
	a, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, n := range networks {
		if p, err := netip.ParsePrefix(n); err == nil && p.Contains(a) {
			return true
		}
	}
	return false
}

// checkNetwork rejects a sign-in from outside the account's networks.
func (s *Service) checkNetwork(ctx context.Context, uid int64, ip string) error {
	var nets []string
	if err := s.pool.QueryRow(ctx, `SELECT allowed_networks FROM users WHERE id=$1`, uid).Scan(&nets); err != nil {
		return err
	}
	if !IPAllowed(nets, ip) {
		s.log.Warn("sign-in from a network that is not allowed", "user", uid, "ip", ip)
		_ = audit.Log(identity.With(ctx, &identity.Principal{UserID: uid}), s.pool, "auth.network_denied", "user", uid, nil, map[string]string{"ip": ip})
		return ErrNetworkDenied
	}
	return nil
}

// checkPrincipalNetwork rejects requests from outside the networks of the
// principal's account (sessions, API keys, SSH tokens, feed links).
func checkPrincipalNetwork(ctx context.Context, p *identity.Principal) error {
	if p == nil || len(p.Networks) == 0 {
		return nil
	}
	if !IPAllowed(p.Networks, audit.ClientIP(ctx)) {
		return ErrNetworkDenied
	}
	return nil
}

func newCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return fmt.Sprintf("%06d", n.Int64())
}

// sendCode creates a one-time code for purpose and e-mails it.
func (s *Service) sendCode(ctx context.Context, uid int64, purpose string) error {
	if s.SendCode == nil {
		return apperr.BadRequest("e-mail codes are not available")
	}
	if !s.limiter.allow(fmt.Sprintf("mfacode:%d", uid), 5, 15*time.Minute) {
		return apperr.TooMany("too many codes requested, try again later")
	}
	code := newCode()
	if _, err := s.pool.Exec(ctx, `INSERT INTO mfa_codes(user_id, purpose, code_hash, expires_at) VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id, purpose) DO UPDATE SET code_hash=EXCLUDED.code_hash, attempts=0, expires_at=EXCLUDED.expires_at`,
		uid, purpose, HashToken(code), time.Now().Add(codeTTL)); err != nil {
		return err
	}
	if err := s.SendCode(ctx, uid, purpose, code); err != nil {
		s.log.Error("sending the e-mail code failed", "user", uid, "err", err)
		return &apperr.Error{Status: 503, Code: "mail_failed", Msg: "the code could not be sent by e-mail"}
	}
	return nil
}

// verifyCode checks and consumes a code.
func (s *Service) verifyCode(ctx context.Context, uid int64, purpose, code string) (bool, error) {
	code = strings.TrimSpace(code)
	var hash []byte
	var attempts int
	var expires time.Time
	err := s.pool.QueryRow(ctx, `UPDATE mfa_codes SET attempts = attempts + 1 WHERE user_id=$1 AND purpose=$2
		RETURNING code_hash, attempts, expires_at`, uid, purpose).Scan(&hash, &attempts, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if attempts > codeAttempts || time.Now().After(expires) || code == "" ||
		subtle.ConstantTimeCompare(hash, HashToken(code)) != 1 {
		if attempts >= codeAttempts {
			_, _ = s.pool.Exec(ctx, `DELETE FROM mfa_codes WHERE user_id=$1 AND purpose=$2`, uid, purpose)
		}
		return false, nil
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM mfa_codes WHERE user_id=$1 AND purpose=$2`, uid, purpose)
	return err == nil, err
}

// checkSecondFactor verifies TOTP or the e-mail code given with the password.
// With e-mail as (only or chosen) factor and no code, a code is sent.
func (s *Service) checkSecondFactor(ctx context.Context, uid int64, code string, emailCode bool) error {
	var totpOn, mailOn bool
	if err := s.pool.QueryRow(ctx, `SELECT totp_enabled AND totp_secret IS NOT NULL, mfa_email AND email <> '' FROM users WHERE id=$1`, uid).Scan(&totpOn, &mailOn); err != nil {
		return err
	}
	if !totpOn && !mailOn {
		return nil
	}
	if mailOn && (emailCode || !totpOn) {
		if strings.TrimSpace(code) == "" {
			if err := s.sendCode(ctx, uid, "login"); err != nil {
				return err
			}
			return ErrEmailCodeSent
		}
		ok, err := s.verifyCode(ctx, uid, "login", code)
		if err != nil {
			return err
		}
		if !ok {
			return ErrTOTPInvalid
		}
		return nil
	}
	if mailOn && strings.TrimSpace(code) == "" {
		return ErrTOTPOrEmail
	}
	return s.checkTOTP(ctx, uid, code)
}

// MFAStatus describes the second factors and network restriction of the
// signed-in user.
type MFAStatus struct {
	TOTP    bool   `json:"totp"`
	Email   bool   `json:"email"`
	Address string `json:"email_address"`
	Local   bool   `json:"local"`
	// Applies: the wiki asks for the second factor at this account's
	// sign-in (false: the identity provider does, setting "mfa: false")
	Applies  bool     `json:"applies"`
	Methods  []string `json:"methods"` // factors set up
	Networks []string `json:"networks"`
}

// MFA returns the status of the signed-in user.
func (s *Service) MFA(ctx context.Context) (*MFAStatus, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return nil, apperr.ErrUnauthorized
	}
	st := &MFAStatus{}
	err := s.pool.QueryRow(ctx, `SELECT totp_enabled, mfa_email, email, provider='local', allowed_networks FROM users WHERE id=$1`, p.UserID).
		Scan(&st.TOTP, &st.Email, &st.Address, &st.Local, &st.Networks)
	st.Applies = s.mfaApplies(p.Provider)
	st.Methods = s.EnrolledMFA(ctx, p.UserID)
	return st, err
}

// BeginEmailMFA sends a code to the user's address (to enable or disable
// the e-mail factor).
func (s *Service) BeginEmailMFA(ctx context.Context) error {
	st, err := s.MFA(ctx)
	if err != nil {
		return err
	}
	if !st.Applies {
		return apperr.BadRequest("the second factor of this account is managed by its identity provider")
	}
	if st.Address == "" {
		return apperr.BadRequest("enter an e-mail address in your profile first")
	}
	return s.sendCode(ctx, identity.From(ctx).UserID, "setup")
}

// SetEmailMFA enables or disables the e-mail factor with a code from
// BeginEmailMFA.
func (s *Service) SetEmailMFA(ctx context.Context, on bool, code string) error {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return apperr.ErrUnauthorized
	}
	ok, err := s.verifyCode(ctx, p.UserID, "setup", code)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTOTPInvalid
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET mfa_email=$2, updated_at=now() WHERE id=$1`, p.UserID, on); err != nil {
		return err
	}
	action := "user.mfa_email_disable"
	if on {
		action = "user.mfa_email_enable"
	}
	return audit.Log(ctx, s.pool, action, "user", p.UserID, nil, nil)
}

// SetOwnNetworks restricts the signed-in account to networks. The current
// address must be included, so nobody locks themselves out by accident.
func (s *Service) SetOwnNetworks(ctx context.Context, list []string) ([]string, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return nil, apperr.ErrUnauthorized
	}
	nets, err := ParseNetworks(list)
	if err != nil {
		return nil, err
	}
	if ip := audit.ClientIP(ctx); ip != "" && !IPAllowed(nets, ip) {
		return nil, apperr.BadRequest("your current address %s is not in these networks: you would lock yourself out", ip)
	}
	return nets, s.setNetworks(ctx, p.UserID, nets)
}

func (s *Service) setNetworks(ctx context.Context, uid int64, nets []string) error {
	var before []string
	_ = s.pool.QueryRow(ctx, `SELECT allowed_networks FROM users WHERE id=$1`, uid).Scan(&before)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET allowed_networks=$2, updated_at=now() WHERE id=$1`, uid, nets); err != nil {
		return err
	}
	s.bump(ctx)
	return audit.Log(ctx, s.pool, "user.networks", "user", uid, map[string]any{"networks": before}, map[string]any{"networks": nets})
}
