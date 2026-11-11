package auth

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Linking of sign-ins. A user is one profile; besides the sign-in the
// account was created with (users.provider/subject) further ones can be
// linked (user_identities). A new sign-in whose e-mail address is the
// confirmed address of an account is
//
//   - linked automatically if the provider confirms the address and the
//     administrator trusts that provider (ExternalIdentity.TrustEmail), or
//   - held back until the person signs in once with the existing account
//     and confirms the link (identity_links; protection against account
//     takeover by a provider that hands out arbitrary addresses).
//
// Invited accounts (provider "invite", created with an e-mail address before
// the first sign-in) take over the first trusted sign-in with that address.

const linkTTL = 30 * time.Minute

// user names allowed by the users table
var inviteName = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,128}$`)

// InviteProvider is the provider of accounts created before the first sign-in.
const InviteProvider = "invite"

// linkedIdentity resolves a sign-in of an external provider: linked before,
// linked now by e-mail, or not linked (linked=false: the account of the
// sign-in itself is used, created at the first sign-in unless NoJIT).
func (s *Service) linkedIdentity(ctx context.Context, ext *ExternalIdentity) (int64, bool, error) {
	var uid int64
	var disabled bool
	err := s.pool.QueryRow(ctx, `UPDATE user_identities i SET last_login_at=now(), username=$3 FROM users u
		WHERE i.provider=$1 AND i.subject=$2 AND u.id=i.user_id RETURNING i.user_id, u.disabled`,
		ext.Provider, ext.Subject, ext.Username).Scan(&uid, &disabled)
	if err == nil {
		if disabled {
			return 0, false, apperr.ErrInvalidLogin
		}
		_, _ = s.pool.Exec(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, uid)
		return uid, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, err
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE provider=$1 AND subject=$2)`, ext.Provider, ext.Subject).Scan(&exists); err != nil {
		return 0, false, err
	}
	if exists {
		return 0, false, nil
	}
	// a new sign-in: does its address belong to an account?
	addr, _ := NormalizeEmail(ext.Email)
	if addr != "" {
		var owner int64
		var provider string
		err := s.pool.QueryRow(ctx, `SELECT u.id, u.provider, u.disabled FROM user_emails e JOIN users u ON u.id=e.user_id
			WHERE e.email=$1 AND e.verified`, addr).Scan(&owner, &provider, &disabled)
		switch {
		case err == nil && disabled:
			return 0, false, apperr.ErrInvalidLogin
		case err == nil && ext.TrustEmail && ext.EmailVerified:
			if err := s.attachIdentity(ctx, owner, provider, ext); err != nil {
				return 0, false, err
			}
			return owner, true, nil
		case err == nil:
			tok := NewToken("")
			if _, err := s.pool.Exec(ctx, `INSERT INTO identity_links(token_hash, provider, subject, username, email, expires_at)
				VALUES ($1,$2,$3,$4,$5,$6)`, HashToken(tok), ext.Provider, ext.Subject, ext.Username, addr, time.Now().Add(linkTTL)); err != nil {
				return 0, false, err
			}
			return 0, false, apperr.LinkRequired(tok)
		case !errors.Is(err, pgx.ErrNoRows):
			return 0, false, err
		}
	}
	if ext.NoJIT {
		return 0, false, apperr.Forbidden("there is no account for this sign-in yet: please ask an administrator")
	}
	return 0, false, nil
}

// attachIdentity links ext to account uid: an invited account takes the
// sign-in as its own, other accounts get it as a further sign-in.
func (s *Service) attachIdentity(ctx context.Context, uid int64, provider string, ext *ExternalIdentity) error {
	return db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if provider == InviteProvider {
			tag, err := tx.Exec(ctx, `UPDATE users SET provider=$2, subject=$3, username=$4,
				display_name = CASE WHEN display_name='' THEN $5 ELSE display_name END, last_login_at=now(), updated_at=now()
				WHERE id=$1 AND NOT EXISTS (SELECT 1 FROM users WHERE provider=$2 AND username=$4)`,
				uid, ext.Provider, ext.Subject, ext.Username, ext.DisplayName)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				return audit.Log(identity.With(ctx, &identity.Principal{UserID: uid, Via: "sso"}), tx, "user.invite_accepted", "user", uid, nil,
					map[string]any{"provider": ext.Provider, "username": ext.Username})
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_identities(provider, subject, user_id, username, last_login_at) VALUES ($1,$2,$3,$4,now())`,
			ext.Provider, ext.Subject, uid, ext.Username); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, uid); err != nil {
			return err
		}
		return audit.Log(identity.With(ctx, &identity.Principal{UserID: uid, Via: "sso"}), tx, "user.identity_linked", "user", uid, nil,
			map[string]any{"provider": ext.Provider, "username": ext.Username, "automatic": true})
	})
}

// LinkInfo describes a sign-in waiting for confirmation (shown before the
// person signs in with the existing account).
type LinkInfo struct {
	Provider string `json:"provider"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// PendingLink returns the sign-in of a link token (without using it).
func (s *Service) PendingLink(ctx context.Context, token string) (*LinkInfo, error) {
	var li LinkInfo
	err := s.pool.QueryRow(ctx, `SELECT provider, username, email FROM identity_links WHERE token_hash=$1 AND expires_at > now()`,
		HashToken(token)).Scan(&li.Provider, &li.Username, &li.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.BadRequest("the link request expired: please sign in with the other method again")
	}
	return &li, err
}

// ConfirmLink links a waiting sign-in to the signed-in user. The user must
// own the address the sign-in came with (confirmed).
func (s *Service) ConfirmLink(ctx context.Context, token string) (*LinkInfo, error) {
	uid, err := self(ctx)
	if err != nil {
		return nil, err
	}
	if token == "" || len(token) > 200 {
		return nil, apperr.BadRequest("invalid link request")
	}
	var li LinkInfo
	var subject string
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `DELETE FROM identity_links WHERE token_hash=$1 AND expires_at > now() RETURNING provider, subject, username, email`,
			HashToken(token)).Scan(&li.Provider, &subject, &li.Username, &li.Email)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.BadRequest("the link request expired: please sign in with the other method again")
		}
		if err != nil {
			return err
		}
		var owns bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_emails WHERE user_id=$1 AND email=$2 AND verified)`, uid, li.Email).Scan(&owns); err != nil {
			return err
		}
		if !owns {
			return apperr.Forbidden("this sign-in belongs to the account with the address %s - sign in with that account", li.Email)
		}
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_identities WHERE provider=$1 AND subject=$2)
			OR EXISTS(SELECT 1 FROM users WHERE provider=$1 AND subject=$2)`, li.Provider, subject).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return apperr.Conflict("this sign-in already belongs to an account")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_identities(provider, subject, user_id, username) VALUES ($1,$2,$3,$4)`,
			li.Provider, subject, uid, li.Username); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "user.identity_linked", "user", uid, nil, map[string]any{"provider": li.Provider, "username": li.Username})
	})
	if err != nil {
		return nil, err
	}
	s.bump(ctx)
	return &li, nil
}

// Identity is a sign-in of a user.
type Identity struct {
	Provider    string     `json:"provider"`
	Subject     string     `json:"subject"`
	Username    string     `json:"username"`
	Primary     bool       `json:"primary"` // the sign-in the account was created with
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

// Identities lists the sign-ins of user uid.
func (s *Service) Identities(ctx context.Context, uid int64) ([]Identity, error) {
	rows, err := s.pool.Query(ctx, `SELECT provider, subject, username, true, created_at, last_login_at FROM users WHERE id=$1
		UNION ALL SELECT provider, subject, username, false, created_at, last_login_at FROM user_identities WHERE user_id=$1
		ORDER BY 4 DESC, 1, 3`, uid)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Identity, error) {
		var i Identity
		return i, r.Scan(&i.Provider, &i.Subject, &i.Username, &i.Primary, &i.CreatedAt, &i.LastLoginAt)
	})
}

// MyIdentities lists the sign-ins of the signed-in user.
func (s *Service) MyIdentities(ctx context.Context) ([]Identity, error) {
	uid, err := self(ctx)
	if err != nil {
		return nil, err
	}
	return s.Identities(ctx, uid)
}

// UnlinkMyIdentity removes a further sign-in of the signed-in user (not the
// one the account was created with).
func (s *Service) UnlinkMyIdentity(ctx context.Context, provider, subject string) error {
	uid, err := self(ctx)
	if err != nil {
		return err
	}
	return s.unlink(ctx, uid, provider, subject)
}

// UnlinkUserIdentity removes a further sign-in of a user (administrators).
func (s *Service) UnlinkUserIdentity(ctx context.Context, uid int64, provider, subject string) error {
	if err := s.requireUserAdmin(ctx); err != nil {
		return err
	}
	return s.unlink(ctx, uid, provider, subject)
}

func (s *Service) unlink(ctx context.Context, uid int64, provider, subject string) error {
	return db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM user_identities WHERE user_id=$1 AND provider=$2 AND subject=$3`, uid, provider, subject)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("linked sign-in")
		}
		// groups and roles that came only with this sign-in
		if _, err := tx.Exec(ctx, `DELETE FROM user_groups WHERE user_id=$1 AND source=$2
			AND NOT EXISTS (SELECT 1 FROM users WHERE id=$1 AND provider=$2)
			AND NOT EXISTS (SELECT 1 FROM user_identities WHERE user_id=$1 AND provider=$2)`, uid, provider); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1 AND source=$2
			AND NOT EXISTS (SELECT 1 FROM users WHERE id=$1 AND provider=$2)
			AND NOT EXISTS (SELECT 1 FROM user_identities WHERE user_id=$1 AND provider=$2)`, uid, provider); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "user.identity_unlinked", "user", uid, nil, map[string]any{"provider": provider, "subject": subject})
	})
}

// ---------------------------------------------------------------- invitations

// Invite holds an account created before the first sign-in.
type Invite struct {
	Email       string  `json:"email"`
	DisplayName string  `json:"display_name"`
	Language    string  `json:"language"`
	Groups      []int64 `json:"groups"`
	Roles       []int64 `json:"roles"`
}

// InviteUser creates an account for an e-mail address: groups and roles
// apply from the first sign-in with a trusted provider that confirms the
// address (administrators, API, CLI, SCIM).
func (s *Service) InviteUser(ctx context.Context, in Invite) (*User, error) {
	if err := s.requireUserAdmin(ctx); err != nil {
		return nil, err
	}
	addr, err := NormalizeEmail(in.Email)
	if err != nil {
		return nil, err
	}
	if addr == "" {
		return nil, apperr.BadRequest("e-mail address is required")
	}
	var id int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_emails WHERE email=$1 AND verified)`, addr).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return apperr.Conflict("an account with the address %s exists", addr)
		}
		name := strings.TrimSpace(in.DisplayName)
		// the address is the user name of the invitation (until the first sign-in)
		uname := addr
		if !inviteName.MatchString(uname) {
			uname = "invite-" + NewToken("")[:12]
		}
		err := tx.QueryRow(ctx, `INSERT INTO users(username, provider, subject, display_name, language) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			uname, InviteProvider, addr, name, in.Language).Scan(&id)
		if db.IsUniqueViolation(err) {
			return apperr.Conflict("%s is already invited", addr)
		}
		if err != nil {
			return apperr.BadRequest("invalid invitation: %v", err)
		}
		if _, err := adoptEmail(ctx, tx, id, addr, true); err != nil {
			return err
		}
		if err := guardRoles(ctx, tx, in.Roles, in.Groups); err != nil {
			return err
		}
		if err := setMembership(ctx, tx, id, in.Groups, in.Roles); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "user.invite", "user", id, nil, map[string]any{"email": addr, "groups": in.Groups, "roles": in.Roles})
	})
	if err != nil {
		return nil, err
	}
	s.bump(ctx)
	return s.GetUser(identity.With(ctx, identity.CLI()), id)
}
