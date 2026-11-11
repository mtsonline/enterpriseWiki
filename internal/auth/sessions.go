package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Sessions: a user sees and ends the own sessions ("my sessions"),
// administrators those of a user. Single sign-on providers may end the
// provider session on logout (RP-initiated logout, SAML SLO) and end wiki
// sessions when the provider says so (OIDC back-channel logout, SAML
// IdP-initiated logout). The session token itself is never shown.

// Session is a browser or SSH session as shown to its user.
type Session struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"` // web | ssh
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Provider  string    `json:"provider,omitempty"` // single sign-on provider, if any
	Current   bool      `json:"current"`
}

// SSOSession identifies the provider session behind a wiki session.
type SSOSession struct {
	Subject   string
	SessionID string // OIDC sid, SAML SessionIndex
	Hint      string // OIDC id token, SAML NameID
}

// LogoutProvider is implemented by redirect providers that can end the
// provider session (single logout).
type LogoutProvider interface {
	// LogoutURL returns the URL to send the browser to, or "" if the
	// provider has no logout endpoint or single logout is turned off.
	// returnURL is the absolute URL the provider should send the browser
	// back to.
	LogoutURL(ctx context.Context, sess SSOSession, returnURL string) (string, error)
}

// LogoutMatch selects the wiki sessions a provider ends. At least one of
// SessionIDs, Subject or Hint is set; set fields must all match.
type LogoutMatch struct {
	SessionIDs []string
	Subject    string
	Hint       string
}

// LogoutHandler is implemented by providers that receive logout messages
// from the identity provider (OIDC back-channel logout, SAML LogoutRequest
// and LogoutResponse at /api/v1/auth/sso/<id>/logout). HandleLogout
// validates the message (signature, issuer, audience, replay); match is nil
// if no session is to end (e.g. a LogoutResponse). respond writes the answer
// after the sessions ended.
type LogoutHandler interface {
	HandleLogout(ctx context.Context, r *http.Request, base string) (match *LogoutMatch, respond func(http.ResponseWriter, *http.Request), err error)
}

const sessionCols = `id, kind, created_at, last_seen_at, expires_at, ip, user_agent, sso_provider`

func scanSessions(rows pgx.Rows, cur []byte) ([]Session, error) {
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var x Session
		var h []byte
		if err := rows.Scan(&x.ID, &x.Kind, &x.CreatedAt, &x.LastSeen, &x.ExpiresAt, &x.IP, &x.UserAgent, &x.Provider, &h); err != nil {
			return nil, err
		}
		x.Current = cur != nil && string(h) == string(cur)
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Service) sessionsOf(ctx context.Context, uid int64, cur []byte) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+sessionCols+`, token_hash FROM sessions
		WHERE user_id=$1 AND expires_at > now() ORDER BY last_seen_at DESC LIMIT 200`, uid)
	if err != nil {
		return nil, err
	}
	return scanSessions(rows, cur)
}

// selfUser returns the caller if signed in with a session or SSH token.
// API keys cannot list or end sessions (a leaked key must not lock out the
// owner nor reveal where they are signed in).
func selfUser(ctx context.Context) (*identity.Principal, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return nil, apperr.ErrUnauthorized
	}
	if p.Via == "apikey" {
		return nil, apperr.Forbidden("sessions cannot be managed with an API key")
	}
	return p, nil
}

func curHash(token string) []byte {
	if token == "" {
		return nil
	}
	return HashToken(token)
}

// MySessions lists the caller's sessions; token marks the current one.
func (s *Service) MySessions(ctx context.Context, token string) ([]Session, error) {
	p, err := selfUser(ctx)
	if err != nil {
		return nil, err
	}
	return s.sessionsOf(ctx, p.UserID, curHash(token))
}

// EndMySession ends one of the caller's sessions.
func (s *Service) EndMySession(ctx context.Context, id int64) error {
	p, err := selfUser(ctx)
	if err != nil {
		return err
	}
	return s.endSessions(ctx, "session.end", p.UserID, `id=$2`, id)
}

// EndMyOtherSessions ends all sessions of the caller except the one of
// token ("sign out everywhere else").
func (s *Service) EndMyOtherSessions(ctx context.Context, token string) error {
	p, err := selfUser(ctx)
	if err != nil {
		return err
	}
	return s.endOthers(ctx, p.UserID, token)
}

func (s *Service) endOthers(ctx context.Context, uid int64, token string) error {
	h := curHash(token)
	if h == nil {
		h = []byte{}
	}
	return s.endSessions(ctx, "session.end_others", uid, `token_hash <> $2`, h)
}

// endSessions deletes sessions of uid matching cond ($2 = arg) and drops
// cached principals at once (also on the other instances via the bus).
func (s *Service) endSessions(ctx context.Context, action string, uid int64, cond string, arg any) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND `+cond, uid, arg)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 && action == "session.end" {
		return apperr.NotFound("session")
	}
	s.bump(ctx)
	return audit.Log(ctx, s.pool, action, "user", uid, nil, map[string]any{"count": tag.RowsAffected()})
}

func (s *Service) requireSessionAdmin(ctx context.Context, uid int64) error {
	if err := perm.Require(ctx, "core.users.manage"); err != nil {
		return err
	}
	return guardTarget(ctx, s.pool, uid)
}

// UserSessions lists the sessions of a user (administrators).
func (s *Service) UserSessions(ctx context.Context, uid int64) ([]Session, error) {
	if err := s.requireSessionAdmin(ctx, uid); err != nil {
		return nil, err
	}
	return s.sessionsOf(ctx, uid, nil)
}

// EndUserSession ends one session of a user, or all if id is 0
// (administrators).
func (s *Service) EndUserSession(ctx context.Context, uid, id int64) error {
	if err := s.requireSessionAdmin(ctx, uid); err != nil {
		return err
	}
	if id == 0 {
		return s.endSessions(ctx, "session.end_all", uid, `$2::bigint IS NULL`, nil)
	}
	return s.endSessions(ctx, "session.end", uid, `id=$2`, id)
}

// Logout deletes the session of token. If the session came from a single
// sign-on provider that supports single logout, it returns the URL that
// ends the provider session too ("" otherwise).
func (s *Service) Logout(ctx context.Context, token string) (string, error) {
	h := HashToken(token)
	var prov string
	var sess SSOSession
	err := s.pool.QueryRow(ctx, `DELETE FROM sessions WHERE token_hash=$1
		RETURNING sso_provider, sso_subject, sso_session, sso_hint`, h).Scan(&prov, &sess.Subject, &sess.SessionID, &sess.Hint)
	s.cache.Delete(string(h))
	s.bus.Publish(ctx, "auth", "")
	if err == pgx.ErrNoRows || prov == "" {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	lp, ok := s.redirectProvider(prov).(LogoutProvider)
	if !ok {
		return "", nil
	}
	back := strings.TrimRight(s.cfg.Get().Server.BaseURL, "/") + "/login?logged_out=1"
	u, err := lp.LogoutURL(ctx, sess, back)
	if err != nil {
		// the wiki session has ended anyway; the provider session stays
		s.log.Warn("single logout", "provider", prov, "err", err)
		return "", nil
	}
	return u, nil
}

// ProviderLogout handles a logout message of the identity provider at
// /api/v1/auth/sso/<provider>/logout and ends the matching sessions. After
// 30 invalid messages in 5 minutes, ip is refused before any verification
// (no key downloads or signature checks for floods).
func (s *Service) ProviderLogout(ctx context.Context, provider string, r *http.Request, ip string) (func(http.ResponseWriter, *http.Request), error) {
	lh, ok := s.redirectProvider(provider).(LogoutHandler)
	if !ok {
		return nil, apperr.NotFound("provider")
	}
	const maxFail, window = 30, 5 * time.Minute
	if s.limiter.exceeded("slo:"+ip, maxFail, window) {
		return nil, apperr.TooMany("too many invalid logout messages")
	}
	m, respond, err := lh.HandleLogout(ctx, r, strings.TrimRight(s.cfg.Get().Server.BaseURL, "/"))
	if err != nil {
		s.limiter.allow("slo:"+ip, maxFail, window)
		return nil, err
	}
	if m != nil {
		if err := s.endProviderSessions(ctx, provider, m); err != nil {
			return nil, err
		}
	}
	return respond, nil
}

func (s *Service) endProviderSessions(ctx context.Context, provider string, m *LogoutMatch) error {
	sids := []string{}
	for _, x := range m.SessionIDs {
		if x != "" {
			sids = append(sids, x)
		}
	}
	if len(sids) == 0 && m.Subject == "" && m.Hint == "" {
		return apperr.BadRequest("logout message names no session")
	}
	rows, err := s.pool.Query(ctx, `DELETE FROM sessions WHERE sso_provider=$1
		AND (cardinality($2::text[]) = 0 OR sso_session = ANY($2))
		AND ($3 = '' OR sso_subject = $3) AND ($4 = '' OR sso_hint = $4)
		RETURNING user_id`, provider, sids, m.Subject, m.Hint)
	if err != nil {
		return err
	}
	uids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	s.bump(ctx)
	for _, uid := range uids {
		_ = audit.Log(ctx, s.pool, "session.provider_logout", "user", uid, nil, map[string]any{"provider": provider})
	}
	return nil
}
