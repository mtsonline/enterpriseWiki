package auth

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Feed keys let feed readers and calendar apps (which cannot sign in) read
// feeds with the permissions of a person. They are only accepted by feed and
// calendar subscription routes, never by the rest of the API.
//
// Rules:
//   - only the SHA-256 of a key is stored; the key (and the link with it) is
//     shown once, when it is made;
//   - a person may have several (one per feed reader), each with a name, up
//     to the limit of the expert settings (FeedKeysMax), and deletes each one
//     on their own; those who manage people (core.users.manage) see the
//     names and can delete all keys of a person;
//   - a key works only while its person is not disabled, and never with more
//     than the rights the person has at the moment of the request.

// feedKeyPrefix starts every feed key (and tells it apart from API keys).
const feedKeyPrefix = "ft_"

// FeedKey is a personal feed key as it is listed: never the key itself,
// except once in the answer to CreateFeedKey (Token).
type FeedKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Token      string     `json:"token,omitempty"`
}

// feedKeysMax is the number of feed keys a person may have (expert
// setting core.feeds.keys_per_user; 50 without the app).
func (s *Service) feedKeysMax() int {
	if s.FeedKeysMax != nil {
		return s.FeedKeysMax()
	}
	return 50
}

// signedInSelf is the person of a request that may manage their own feed
// keys: signed in with a session (not with an API key or a feed key).
func signedInSelf(ctx context.Context) (*identity.Principal, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return nil, apperr.ErrUnauthorized
	}
	if p.Via == "apikey" || p.Via == "feed" {
		return nil, apperr.ErrForbidden
	}
	return p, nil
}

// cleanKeyName checks the name of a key: 1–200 characters, no control
// characters (it is shown in lists and in the audit log).
func cleanKeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 200 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", apperr.BadRequest("name: 1 to 200 characters, no control characters")
	}
	return name, nil
}

// CreateFeedKey makes a new feed key for the caller. The answer carries the
// key once (Token); afterwards only its name and start are known.
func (s *Service) CreateFeedKey(ctx context.Context, name string) (*FeedKey, error) {
	p, err := signedInSelf(ctx)
	if err != nil {
		return nil, err
	}
	if name, err = cleanKeyName(name); err != nil {
		return nil, err
	}
	tok := NewToken(feedKeyPrefix)
	k := &FeedKey{Name: name, Prefix: tok[:len(feedKeyPrefix)+8], Token: tok}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// the row of the person serialises parallel requests, so the limit holds
		var n int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM feed_keys WHERE user_id=u.id) FROM users u WHERE u.id=$1 FOR UPDATE`, p.UserID).Scan(&n); err != nil {
			return err
		}
		if max := s.feedKeysMax(); n >= max {
			return apperr.Conflict("you have %d feed keys, the most allowed; delete one first", max)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO feed_keys(user_id, name, prefix, token_hash) VALUES ($1,$2,$3,$4) RETURNING id, created_at`,
			p.UserID, k.Name, k.Prefix, HashToken(tok)).Scan(&k.ID, &k.CreatedAt); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "feedkey.create", "feedkey", k.ID, nil, map[string]any{"name": k.Name, "prefix": k.Prefix})
	})
	if err != nil {
		return nil, err
	}
	return k, nil
}

// FeedKeys lists the caller's feed keys, newest first.
func (s *Service) FeedKeys(ctx context.Context) ([]FeedKey, error) {
	p, err := signedInSelf(ctx)
	if err != nil {
		return nil, err
	}
	return s.feedKeysOf(ctx, p.UserID)
}

// UserFeedKeys lists the feed keys of a person for those who manage
// people (core.users.manage): names and last use, never the keys.
func (s *Service) UserFeedKeys(ctx context.Context, uid int64) ([]FeedKey, error) {
	if err := s.requireUserAdmin(ctx); err != nil {
		return nil, err
	}
	return s.feedKeysOf(ctx, uid)
}

func (s *Service) feedKeysOf(ctx context.Context, uid int64) ([]FeedKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, prefix, created_at, last_used_at FROM feed_keys WHERE user_id=$1 ORDER BY created_at DESC, id DESC`, uid)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (FeedKey, error) {
		var k FeedKey
		return k, r.Scan(&k.ID, &k.Name, &k.Prefix, &k.CreatedAt, &k.LastUsedAt)
	})
}

// DeleteFeedKey deletes one of the caller's feed keys: links with it stop
// working at once.
func (s *Service) DeleteFeedKey(ctx context.Context, id int64) error {
	p, err := signedInSelf(ctx)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(ctx, `DELETE FROM feed_keys WHERE id=$1 AND user_id=$2 RETURNING name`, id, p.UserID).Scan(&name); err != nil {
			if err == pgx.ErrNoRows {
				return apperr.NotFound("feed key")
			}
			return err
		}
		return audit.Log(ctx, tx, "feedkey.delete", "feedkey", id, map[string]any{"name": name}, nil)
	})
}

// DeleteFeedKeys deletes all of the caller's feed keys and returns how
// many there were.
func (s *Service) DeleteFeedKeys(ctx context.Context) (int64, error) {
	p, err := signedInSelf(ctx)
	if err != nil {
		return 0, err
	}
	return s.deleteFeedKeysOf(ctx, p.UserID)
}

// DeleteUserFeedKeys deletes all feed keys of a person, for those who
// manage people (e.g. after a link got out).
func (s *Service) DeleteUserFeedKeys(ctx context.Context, uid int64) (int64, error) {
	if err := s.requireUserAdmin(ctx); err != nil {
		return 0, err
	}
	return s.deleteFeedKeysOf(ctx, uid)
}

func (s *Service) deleteFeedKeysOf(ctx context.Context, uid int64) (int64, error) {
	var n int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM feed_keys WHERE user_id=$1`, uid)
		if err != nil {
			return err
		}
		if n = tag.RowsAffected(); n == 0 {
			return nil
		}
		return audit.Log(ctx, tx, "feedkey.delete_all", "user", uid, map[string]any{"count": n}, nil)
	})
	return n, err
}

// FeedContext returns the request context for a feed route: with ?token=
// the principal of the person of that feed key, otherwise the caller
// (session, API key or guest). The bool reports whether the result is
// personal.
func (s *Service) FeedContext(r *http.Request) (context.Context, bool, error) {
	ctx := r.Context()
	tok := r.URL.Query().Get("token")
	if tok == "" {
		return ctx, identity.From(ctx) != nil, nil
	}
	// "ft_" and the hexadecimal digits of NewToken; anything else is never
	// looked up
	if !strings.HasPrefix(tok, feedKeyPrefix) || len(tok) != len(feedKeyPrefix)+2*TokenBytes {
		return nil, false, apperr.ErrUnauthorized
	}
	// the key is found by its hash (the stored value is no secret); last
	// use is noted at most every 5 minutes. Disabled people are left out
	// here and again by LoadPrincipal (two checks, on purpose).
	var uid int64
	err := s.pool.QueryRow(ctx, `WITH k AS (
			SELECT f.id, f.user_id FROM feed_keys f JOIN users u ON u.id=f.user_id WHERE f.token_hash=$1 AND NOT u.disabled),
		touch AS (UPDATE feed_keys SET last_used_at=now() WHERE id=(SELECT id FROM k) AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes'))
		SELECT user_id FROM k`, HashToken(tok)).Scan(&uid)
	if err != nil {
		return nil, false, apperr.ErrUnauthorized
	}
	p, err := s.LoadPrincipal(ctx, uid)
	if err != nil {
		return nil, false, apperr.ErrUnauthorized
	}
	p.Via = "feed"
	if err := s.checkRequest(ctx, p); err != nil {
		return nil, false, err
	}
	return identity.With(ctx, p), true, nil
}
