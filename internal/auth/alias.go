package auth

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// The wiki user name (alias): optional and unique (case-insensitive). The
// person signs in with it as well, whatever sign-in method the account
// uses. It is set by the person, by an administrator or by a sign-in
// provider (setting username_from: LDAP uid/cn, an OIDC claim, a SAML
// attribute); a provider keeps it up to date at every sign-in, and the
// person cannot change a name the provider manages. Local user names win:
// an alias never equals the user name of a local account.

var aliasRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$`)

// ErrAliasTaken is returned for a user name that is used already.
var ErrAliasTaken = apperr.Conflict("this user name is already taken")

// checkAlias validates alias for user uid ("" removes it).
func checkAlias(ctx context.Context, q db.Querier, uid int64, alias string) error {
	if alias == "" {
		return nil
	}
	if !aliasRe.MatchString(alias) {
		return apperr.BadRequest("the user name must be 2-64 letters, digits, dots, hyphens or underscores (starting with a letter or digit)")
	}
	var taken bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id <> $1 AND (lower(alias) = lower($2)
		OR (provider = 'local' AND lower(username) = lower($2))))`, uid, alias).Scan(&taken); err != nil {
		return err
	}
	if taken {
		return ErrAliasTaken
	}
	return nil
}

func (s *Service) setAlias(ctx context.Context, uid int64, alias, source string) error {
	alias = strings.TrimSpace(alias)
	return db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := checkAlias(ctx, tx, uid, alias); err != nil {
			return err
		}
		var before *string
		if err := tx.QueryRow(ctx, `SELECT alias FROM users WHERE id=$1`, uid).Scan(&before); err != nil {
			return apperr.NotFound("user")
		}
		var v any
		if alias != "" {
			v = alias
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET alias=$2, alias_source=$3, updated_at=now() WHERE id=$1`, uid, v, source); err != nil {
			if db.IsUniqueViolation(err) {
				return ErrAliasTaken
			}
			return err
		}
		return audit.Log(ctx, tx, "user.alias", "user", uid, map[string]any{"alias": before}, map[string]any{"alias": alias, "source": source})
	})
}

// SetMyAlias sets the caller's user name ("" removes it), unless a sign-in
// provider manages it.
func (s *Service) SetMyAlias(ctx context.Context, alias string) error {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return apperr.ErrUnauthorized
	}
	if err := s.requireShared(ctx); err != nil {
		return err
	}
	var source string
	if err := s.pool.QueryRow(ctx, `SELECT alias_source FROM users WHERE id=$1`, p.UserID).Scan(&source); err != nil {
		return err
	}
	if source != "" {
		return apperr.BadRequest("your user name comes from %s and cannot be changed here", source)
	}
	if err := s.setAlias(ctx, p.UserID, alias, ""); err != nil {
		return err
	}
	s.bump(ctx)
	return nil
}

// SetUserAlias sets the user name of a user (administrators). A provider
// that manages the name sets it again at the next sign-in.
func (s *Service) SetUserAlias(ctx context.Context, uid int64, alias string) error {
	if err := s.requireUserAdmin(ctx); err != nil {
		return err
	}
	if err := guardTarget(ctx, s.pool, uid); err != nil {
		return err
	}
	if err := s.setAlias(ctx, uid, alias, ""); err != nil {
		return err
	}
	s.bump(ctx)
	return nil
}

// syncAlias takes over the user name a provider maps; a name that is
// invalid or taken is skipped (the sign-in goes on).
func (s *Service) syncAlias(ctx context.Context, uid int64, ext *ExternalIdentity) {
	if ext.Alias == "" {
		return
	}
	var cur *string
	var source string
	if err := s.pool.QueryRow(ctx, `SELECT alias, alias_source FROM users WHERE id=$1`, uid).Scan(&cur, &source); err != nil {
		return
	}
	if cur != nil && *cur == ext.Alias && source == ext.Provider {
		return
	}
	if err := s.setAlias(ctx, uid, ext.Alias, ext.Provider); err != nil {
		s.log.Warn("user name of the provider not taken over", "provider", ext.Provider, "alias", ext.Alias, "err", err)
	}
}

// userByAlias finds the account of a wiki user name (for signing in):
// its id, provider and user name at the provider.
func (s *Service) userByAlias(ctx context.Context, alias string) (int64, string, string, bool) {
	if !aliasRe.MatchString(alias) {
		return 0, "", "", false
	}
	var id int64
	var provider, username string
	err := s.pool.QueryRow(ctx, `SELECT id, provider, username FROM users WHERE lower(alias) = lower($1)`, alias).Scan(&id, &provider, &username)
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return 0, "", "", false
	}
	return id, provider, username, true
}
