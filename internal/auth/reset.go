package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
)

// Limits of "Forgot your password?".
const (
	resetTTL        = time.Hour
	resetPerAddress = 3 // links to one address per hour
	resetSendTime   = time.Minute
)

// PasswordResetOpen reports whether "Forgot your password?" is offered here:
// switched on, e-mail can be sent, and the accounts of this site are its
// own (on a further site with shared accounts only the main site offers it).
func (s *Service) PasswordResetOpen(ctx context.Context) bool {
	if !s.cfg.Get().Login.ResetEnabled() || s.SendPasswordReset == nil || (s.MailReady != nil && !s.MailReady()) {
		return false
	}
	return s.SeparateAccounts || identity.Site(ctx) == "main"
}

// RequestPasswordReset sends the link of "Forgot your password?" to a
// confirmed address of a local account. The answer is the same for every
// address, and the mail goes out in the background, so neither the answer
// nor its timing tells whether the address has an account. An address of an
// account without a wiki password (LDAP, single sign-on) gets a mail naming
// who manages the password; unknown addresses get nothing.
func (s *Service) RequestPasswordReset(ctx context.Context, email, ip string) error {
	if !s.PasswordResetOpen(ctx) {
		return apperr.NotFound("password reset")
	}
	c := s.cfg.Get()
	addr, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if addr == "" {
		return apperr.BadRequest("e-mail address is required")
	}
	if !s.limiter.allow("reset:"+ip, int(limits.Value(c, limits.ResetPerHour)), time.Hour) ||
		!s.limiter.allow("reset-addr:"+addr, resetPerAddress, time.Hour) {
		return apperr.TooMany("too many requests, try again later")
	}
	var uid int64
	var provider, name, lang string
	var hasPassword, disabled bool
	err = s.pool.QueryRow(ctx, `SELECT u.id, u.provider, coalesce(nullif(u.display_name,''), u.username), u.language,
			u.password_hash IS NOT NULL, u.disabled
		FROM user_emails e JOIN users u ON u.id=e.user_id WHERE e.email=$1 AND e.verified`, addr).
		Scan(&uid, &provider, &name, &lang, &hasPassword, &disabled)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && disabled) {
		return nil
	}
	if err != nil {
		return err
	}
	if lang == "" {
		lang = c.I18n.DefaultLanguage
	}
	link := ""
	if provider == "local" && hasPassword {
		tok := NewToken("")
		// only the newest link of an account is valid
		if err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `DELETE FROM password_resets WHERE user_id=$1`, uid); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO password_resets(token_hash, user_id, expires_at) VALUES ($1,$2,$3)`,
				HashToken(tok), uid, time.Now().Add(resetTTL))
			return err
		}); err != nil {
			return err
		}
		link = trimSlash(c.Server.BaseURL) + "/password-reset/confirm?token=" + tok
	}
	send := s.SendPasswordReset
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), resetSendTime)
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		defer cancel()
		if err := send(bg, addr, name, lang, link, provider); err != nil {
			s.log.Warn("password reset mail", "user", uid, "err", err)
		}
	}()
	return nil
}

// ResetPassword sets a new password with the token of the link. It ends all
// sessions of the account and lifts a lockout, but does not sign in: the
// further factors of the account still apply at the next sign-in.
func (s *Service) ResetPassword(ctx context.Context, token, password string) (*User, error) {
	if token == "" || len(token) > 200 {
		return nil, apperr.BadRequest("invalid link")
	}
	if !s.PasswordResetOpen(ctx) {
		return nil, apperr.NotFound("password reset")
	}
	if err := CheckPasswordPolicy(password, s.cfg.Get().Auth.MinPasswordLen); err != nil {
		return nil, apperr.BadRequest("%s", err.Error())
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	var uid int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var valid bool
		err := tx.QueryRow(ctx, `DELETE FROM password_resets WHERE token_hash=$1 RETURNING user_id, expires_at > now()`, HashToken(token)).Scan(&uid, &valid)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.BadRequest("the link is invalid or was already used")
		}
		if err != nil {
			return err
		}
		if !valid {
			return apperr.BadRequest("the link expired: please request a new one")
		}
		tag, err := tx.Exec(ctx, `UPDATE users SET password_hash=$2, failed_logins=0, locked_until=NULL, updated_at=now()
			WHERE id=$1 AND provider='local' AND NOT disabled`, uid, hash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.BadRequest("the link is invalid or was already used")
		}
		// whoever knew the old password is signed out everywhere
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, uid); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM password_resets WHERE user_id=$1`, uid); err != nil {
			return err
		}
		return audit.Log(identity.With(ctx, &identity.Principal{UserID: uid, Via: "password-reset"}), tx, "user.password_reset", "user", uid, nil, nil)
	})
	if err != nil {
		return nil, err
	}
	s.bump(ctx)
	return s.GetUser(identity.With(ctx, identity.CLI()), uid)
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// WaitBackground waits until mails sent in the background have gone out
// (shutdown, tests).
func (s *Service) WaitBackground() { s.background.Wait() }
