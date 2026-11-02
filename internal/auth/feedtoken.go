package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Feed tokens let feed readers and calendar apps (which cannot sign in) read
// feeds with the permissions of a user. They are only accepted by feed and
// calendar subscription routes, never by the rest of the API.

func newFeedToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "ft_" + base64.RawURLEncoding.EncodeToString(b)
}

// FeedToken returns the feed token of a user, creating it if needed.
func (s *Service) FeedToken(ctx context.Context, uid int64) (string, error) {
	var tok *string
	if err := s.pool.QueryRow(ctx, `SELECT feed_token FROM users WHERE id=$1`, uid).Scan(&tok); err != nil {
		return "", apperr.NotFound("user")
	}
	if tok != nil && *tok != "" {
		return *tok, nil
	}
	return s.RotateFeedToken(ctx, uid)
}

// RotateFeedToken replaces the feed token (old feed links stop working).
func (s *Service) RotateFeedToken(ctx context.Context, uid int64) (string, error) {
	t := newFeedToken()
	_, err := s.pool.Exec(ctx, `UPDATE users SET feed_token=$2 WHERE id=$1`, uid, t)
	return t, err
}

// RevokeFeedToken removes the feed token.
func (s *Service) RevokeFeedToken(ctx context.Context, uid int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET feed_token=NULL WHERE id=$1`, uid)
	return err
}

// FeedContext returns the request context for a feed route: with ?token=
// the principal of the token's user, otherwise the caller (session, API key
// or guest). The bool reports whether the result is personal.
func (s *Service) FeedContext(r *http.Request) (context.Context, bool, error) {
	ctx := r.Context()
	tok := r.URL.Query().Get("token")
	if tok == "" {
		return ctx, identity.From(ctx) != nil, nil
	}
	if !strings.HasPrefix(tok, "ft_") || len(tok) > 64 {
		return nil, false, apperr.ErrUnauthorized
	}
	var uid int64
	var stored string
	if err := s.pool.QueryRow(ctx, `SELECT id, feed_token FROM users WHERE feed_token=$1 AND NOT disabled`, tok).Scan(&uid, &stored); err != nil ||
		subtle.ConstantTimeCompare([]byte(stored), []byte(tok)) != 1 {
		return nil, false, apperr.ErrUnauthorized
	}
	p, err := s.LoadPrincipal(ctx, uid)
	if err != nil {
		return nil, false, apperr.ErrUnauthorized
	}
	p.Via = "feed"
	return identity.With(ctx, p), true, nil
}
