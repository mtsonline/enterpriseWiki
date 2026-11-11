package auth

import (
	"context"
	"errors"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// E-mail addresses: a user has one or more; a confirmed address belongs to
// exactly one user (unique index), so it can identify the user when a new
// sign-in arrives (see link.go). users.email mirrors the primary address.

// Email is an address of a user.
type Email struct {
	ID         int64      `json:"id"`
	Email      string     `json:"email"`
	Verified   bool       `json:"verified"`
	Primary    bool       `json:"primary"`
	CreatedAt  time.Time  `json:"created_at"`
	VerifiedAt *time.Time `json:"verified_at"`
}

const emailTokenTTL = 48 * time.Hour

// NormalizeEmail checks an address (no display name) and returns it in
// lower case ("" stays "").
func NormalizeEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Name != "" || a.Address != s || len(s) > 254 || strings.ContainsAny(s, " \t\r\n<>") {
		return "", apperr.BadRequest("invalid e-mail address %q", s)
	}
	return strings.ToLower(s), nil
}

// Emails lists the addresses of user uid.
func (s *Service) Emails(ctx context.Context, uid int64) ([]Email, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, email, verified, is_primary, created_at, verified_at FROM user_emails
		WHERE user_id=$1 ORDER BY is_primary DESC, email`, uid)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Email, error) {
		var e Email
		return e, r.Scan(&e.ID, &e.Email, &e.Verified, &e.Primary, &e.CreatedAt, &e.VerifiedAt)
	})
}

// adoptEmail adds addr to user uid (within q); verified addresses are
// confirmed (by an administrator, a trusted identity provider or the
// confirmation link). The first address, or the first confirmed one, becomes
// the primary address. It returns the id of the row ("" adds nothing).
func adoptEmail(ctx context.Context, q db.Querier, uid int64, addr string, verified bool) (int64, error) {
	addr, err := NormalizeEmail(addr)
	if err != nil || addr == "" {
		return 0, err
	}
	if verified {
		var owner int64
		err := q.QueryRow(ctx, `SELECT user_id FROM user_emails WHERE email=$1 AND verified`, addr).Scan(&owner)
		if err == nil && owner != uid {
			verified = false // confirmed by somebody else: stays unconfirmed here
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}
	var id int64
	err = q.QueryRow(ctx, `INSERT INTO user_emails(user_id, email, verified, verified_at)
		VALUES ($1,$2,$3, CASE WHEN $3 THEN now() END)
		ON CONFLICT (user_id, email) DO UPDATE SET
			verified = user_emails.verified OR EXCLUDED.verified,
			verified_at = COALESCE(user_emails.verified_at, EXCLUDED.verified_at)
		RETURNING id`, uid, addr, verified).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, choosePrimary(ctx, q, uid)
}

// choosePrimary makes sure the user has a primary address (preferably a
// confirmed one) and mirrors it into users.email.
func choosePrimary(ctx context.Context, q db.Querier, uid int64) error {
	// an unconfirmed primary gives way to a confirmed address
	if _, err := q.Exec(ctx, `UPDATE user_emails SET is_primary=false WHERE user_id=$1 AND is_primary AND NOT verified
		AND EXISTS (SELECT 1 FROM user_emails WHERE user_id=$1 AND verified)`, uid); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `UPDATE user_emails SET is_primary=true WHERE id = (
		SELECT id FROM user_emails WHERE user_id=$1 ORDER BY verified DESC, created_at, id LIMIT 1)
		AND NOT EXISTS (SELECT 1 FROM user_emails WHERE user_id=$1 AND is_primary)`, uid); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `UPDATE users SET email = COALESCE((SELECT email FROM user_emails WHERE user_id=$1 AND is_primary), ''),
		updated_at=now() WHERE id=$1`, uid)
	return err
}

// setAdminPrimary makes addr a confirmed address of uid and its primary one
// ("" removes the primary address).
func setAdminPrimary(ctx context.Context, q db.Querier, uid int64, addr string) error {
	addr, err := NormalizeEmail(addr)
	if err != nil {
		return err
	}
	if addr == "" {
		if _, err := q.Exec(ctx, `DELETE FROM user_emails WHERE user_id=$1 AND is_primary`, uid); err != nil {
			return err
		}
		return choosePrimary(ctx, q, uid)
	}
	var owner int64
	err = q.QueryRow(ctx, `SELECT user_id FROM user_emails WHERE email=$1 AND verified`, addr).Scan(&owner)
	if err == nil && owner != uid {
		return apperr.Conflict("the address %s is confirmed by another account", addr)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err := adoptEmail(ctx, q, uid, addr, true); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `UPDATE user_emails SET is_primary = (email=$2) WHERE user_id=$1`, uid, addr); err != nil {
		return err
	}
	return choosePrimary(ctx, q, uid)
}

// BackfillEmails adds the users.email of accounts without addresses (data
// written before addresses had their own table, or directly in the
// database) as unconfirmed primary addresses.
func (s *Service) BackfillEmails(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO user_emails(user_id, email, is_primary)
		SELECT id, lower(email), true FROM users u WHERE email ~ '^[^@\s]+@[^@\s]+$' AND length(email) <= 254
			AND NOT EXISTS (SELECT 1 FROM user_emails e WHERE e.user_id=u.id)
		ON CONFLICT DO NOTHING`)
	return err
}

// self returns the signed-in user (not the CLI without --as).
func self(ctx context.Context) (int64, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return 0, apperr.ErrUnauthorized
	}
	return p.UserID, nil
}

// MyEmails lists the addresses of the signed-in user.
func (s *Service) MyEmails(ctx context.Context) ([]Email, error) {
	uid, err := self(ctx)
	if err != nil {
		return nil, err
	}
	return s.Emails(ctx, uid)
}

// AddMyEmail adds an unconfirmed address to the signed-in user and sends the
// confirmation link.
func (s *Service) AddMyEmail(ctx context.Context, addr string) (*Email, error) {
	uid, err := self(ctx)
	if err != nil {
		return nil, err
	}
	addr, err = NormalizeEmail(addr)
	if err != nil {
		return nil, err
	}
	if addr == "" {
		return nil, apperr.BadRequest("e-mail address is required")
	}
	if s.SendConfirmation == nil || (s.MailReady != nil && !s.MailReady()) {
		return nil, apperr.BadRequest("sending e-mail is not configured: an administrator can add and confirm the address")
	}
	if !s.limiter.allow("email:"+itoa(uid), 10, time.Hour) {
		return nil, apperr.TooMany("too many e-mail confirmations, try again later")
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM user_emails WHERE user_id=$1`, uid).Scan(&n); err != nil {
		return nil, err
	}
	if n >= 10 {
		return nil, apperr.BadRequest("at most 10 e-mail addresses per user")
	}
	id, err := adoptEmail(ctx, s.pool, uid, addr, false)
	if err != nil {
		return nil, err
	}
	if err := s.sendConfirmation(ctx, uid, id); err != nil {
		return nil, err
	}
	s.bump(ctx)
	return s.email(ctx, uid, id)
}

// ResendConfirmation sends the confirmation link of an address again.
func (s *Service) ResendConfirmation(ctx context.Context, id int64) error {
	uid, err := self(ctx)
	if err != nil {
		return err
	}
	e, err := s.email(ctx, uid, id)
	if err != nil {
		return err
	}
	if e.Verified {
		return apperr.BadRequest("the address is already confirmed")
	}
	if !s.limiter.allow("email:"+itoa(uid), 10, time.Hour) {
		return apperr.TooMany("too many e-mail confirmations, try again later")
	}
	return s.sendConfirmation(ctx, uid, id)
}

func (s *Service) email(ctx context.Context, uid, id int64) (*Email, error) {
	var e Email
	err := s.pool.QueryRow(ctx, `SELECT id, email, verified, is_primary, created_at, verified_at FROM user_emails WHERE id=$1 AND user_id=$2`, id, uid).
		Scan(&e.ID, &e.Email, &e.Verified, &e.Primary, &e.CreatedAt, &e.VerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("e-mail address")
	}
	return &e, err
}

func (s *Service) sendConfirmation(ctx context.Context, uid, id int64) error {
	tok := NewToken("")
	if _, err := s.pool.Exec(ctx, `DELETE FROM email_tokens WHERE email_id=$1`, id); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO email_tokens(token_hash, email_id, expires_at) VALUES ($1,$2,$3)`,
		HashToken(tok), id, time.Now().Add(emailTokenTTL)); err != nil {
		return err
	}
	if s.SendConfirmation == nil {
		return apperr.BadRequest("e-mail is not configured: an administrator can confirm the address")
	}
	var addr string
	if err := s.pool.QueryRow(ctx, `SELECT email FROM user_emails WHERE id=$1`, id).Scan(&addr); err != nil {
		return err
	}
	link := strings.TrimRight(s.cfg.Get().Server.BaseURL, "/") + "/verify-email?token=" + tok
	return s.SendConfirmation(ctx, uid, addr, link)
}

// VerifyEmail confirms an address with the token of the confirmation link.
// Anyone with the link may confirm (it went to that address); the address
// must not be confirmed by another user meanwhile.
func (s *Service) VerifyEmail(ctx context.Context, token string) (string, error) {
	if token == "" || len(token) > 200 {
		return "", apperr.BadRequest("invalid confirmation link")
	}
	var addr string
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var id, uid int64
		var valid bool
		err := tx.QueryRow(ctx, `DELETE FROM email_tokens t USING user_emails e WHERE t.token_hash=$1 AND e.id=t.email_id
			RETURNING e.id, e.user_id, e.email, t.expires_at > now()`, HashToken(token)).Scan(&id, &uid, &addr, &valid)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.BadRequest("the confirmation link is invalid or was already used")
		}
		if err != nil {
			return err
		}
		if !valid {
			return apperr.BadRequest("the confirmation link expired: request a new one in your profile")
		}
		var owner int64
		err = tx.QueryRow(ctx, `SELECT user_id FROM user_emails WHERE email=$1 AND verified`, addr).Scan(&owner)
		if err == nil && owner != uid {
			return apperr.Conflict("the address is already confirmed by another account")
		}
		if _, err := tx.Exec(ctx, `UPDATE user_emails SET verified=true, verified_at=now() WHERE id=$1`, id); err != nil {
			return err
		}
		if err := choosePrimary(ctx, tx, uid); err != nil {
			return err
		}
		return audit.Log(identity.With(ctx, &identity.Principal{UserID: uid, Via: "email-link"}), tx, "user.email_verified", "user", uid, nil, map[string]any{"email": addr})
	})
	if err != nil {
		return "", err
	}
	s.bump(ctx)
	return addr, nil
}

// SetMyPrimaryEmail makes a confirmed address the primary one.
func (s *Service) SetMyPrimaryEmail(ctx context.Context, id int64) error {
	uid, err := self(ctx)
	if err != nil {
		return err
	}
	return s.setPrimary(ctx, uid, id)
}

func (s *Service) setPrimary(ctx context.Context, uid, id int64) error {
	e, err := s.email(ctx, uid, id)
	if err != nil {
		return err
	}
	if !e.Verified {
		return apperr.BadRequest("only a confirmed address can be the primary one")
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE user_emails SET is_primary = (id=$2) WHERE user_id=$1`, uid, id); err != nil {
			return err
		}
		return choosePrimary(ctx, tx, uid)
	})
	if err == nil {
		s.bump(ctx)
	}
	return err
}

// RemoveMyEmail removes an address (not the primary one).
func (s *Service) RemoveMyEmail(ctx context.Context, id int64) error {
	uid, err := self(ctx)
	if err != nil {
		return err
	}
	return s.removeEmail(ctx, uid, id)
}

func (s *Service) removeEmail(ctx context.Context, uid, id int64) error {
	e, err := s.email(ctx, uid, id)
	if err != nil {
		return err
	}
	if e.Primary {
		return apperr.BadRequest("choose another primary address first")
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM user_emails WHERE id=$1 AND user_id=$2`, id, uid); err != nil {
			return err
		}
		return choosePrimary(ctx, tx, uid)
	})
	if err == nil {
		s.bump(ctx)
	}
	return err
}

// ---------------------------------------------------------------- administration

// requireUserAdmin: managing accounts (core.users.manage, shared accounts).
func (s *Service) requireUserAdmin(ctx context.Context) error {
	if err := perm.Require(ctx, "core.users.manage"); err != nil {
		return err
	}
	return s.requireShared(ctx)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// UserEmails lists the addresses of a user (administrators).
func (s *Service) UserEmails(ctx context.Context, uid int64) ([]Email, error) {
	if err := s.requireUserAdmin(ctx); err != nil {
		return nil, err
	}
	return s.Emails(ctx, uid)
}

// AddUserEmail adds an address to a user; administrators may confirm it
// directly (verified), otherwise the confirmation link is sent.
func (s *Service) AddUserEmail(ctx context.Context, uid int64, addr string, verified bool) (*Email, error) {
	if err := s.requireUserAdmin(ctx); err != nil {
		return nil, err
	}
	addr, err := NormalizeEmail(addr)
	if err != nil {
		return nil, err
	}
	if addr == "" {
		return nil, apperr.BadRequest("e-mail address is required")
	}
	var id int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if verified {
			var owner int64
			err := tx.QueryRow(ctx, `SELECT user_id FROM user_emails WHERE email=$1 AND verified`, addr).Scan(&owner)
			if err == nil && owner != uid {
				return apperr.Conflict("the address is confirmed by another account")
			}
		}
		var err error
		if id, err = adoptEmail(ctx, tx, uid, addr, verified); err != nil {
			if db.IsForeignKeyViolation(err) {
				return apperr.NotFound("user")
			}
			return err
		}
		return audit.Log(ctx, tx, "user.email_add", "user", uid, nil, map[string]any{"email": addr, "verified": verified})
	})
	if err != nil {
		return nil, err
	}
	if !verified {
		if err := s.sendConfirmation(ctx, uid, id); err != nil {
			return nil, err
		}
	}
	s.bump(ctx)
	return s.email(ctx, uid, id)
}

// SetUserPrimaryEmail sets the primary address of a user (administrators).
func (s *Service) SetUserPrimaryEmail(ctx context.Context, uid, id int64) error {
	if err := s.requireUserAdmin(ctx); err != nil {
		return err
	}
	return s.setPrimary(ctx, uid, id)
}

// RemoveUserEmail removes an address of a user (administrators).
func (s *Service) RemoveUserEmail(ctx context.Context, uid, id int64) error {
	if err := s.requireUserAdmin(ctx); err != nil {
		return err
	}
	return s.removeEmail(ctx, uid, id)
}

// UserByEmail returns the user with the confirmed address addr.
func (s *Service) UserByEmail(ctx context.Context, addr string) (int64, bool, error) {
	addr, err := NormalizeEmail(addr)
	if err != nil || addr == "" {
		return 0, false, err
	}
	var uid int64
	err = s.pool.QueryRow(ctx, `SELECT user_id FROM user_emails WHERE email=$1 AND verified`, addr).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return uid, err == nil, err
}
