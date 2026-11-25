package server

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/csvsafe"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Consent to cookies and to the privacy policy (config.Consent; concept
// "Zustimmung zu Cookies und Datenschutz"). Cookie choices stay in the
// browser; the acceptance of the privacy policy by signed-in people is
// recorded per version and enforced for sessions in the browser (not for
// API keys: a person creates one only after accepting). Administrators are
// asked as well but never locked out.

// consentVersion: the version in force (at least 1).
func consentVersion(v int) int { return max(v, 1) }

// siteConsent is what everybody learns about the asking for consent.
func siteConsent(c config.Consent) any {
	out := map[string]any{}
	if c.Cookies.Enabled {
		out["cookies"] = map[string]any{"required": c.Cookies.Required, "version": consentVersion(c.Cookies.Version), "text": nonNilTexts(c.Cookies.Text), "page": c.Cookies.Page}
	}
	if c.Privacy.Enabled {
		out["privacy"] = map[string]any{"version": consentVersion(c.Privacy.Version), "page": c.Privacy.Page, "guests": c.Privacy.Guests, "text": nonNilTexts(c.Privacy.Text)}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func nonNilTexts(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// privacyAccepted reports whether the person accepted this version.
func (s *Server) privacyAccepted(ctx context.Context, userID int64, version int) (bool, error) {
	key := fmt.Sprintf("%d/%d", userID, version)
	if _, ok := s.consentOK.Load(key); ok {
		return true, nil
	}
	var n int
	if err := s.app.DB.QueryRow(ctx, `SELECT count(*) FROM consents WHERE user_id=$1 AND kind='privacy' AND version=$2`, userID, version).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		s.consentOK.Store(key, true)
	}
	return n > 0, nil
}

// consentOpen are the requests that work before the privacy policy is
// accepted: what the page needs, signing out, the own consent.
var consentOpen = []string{"/api/v1/site", "/api/v1/i18n/", "/api/v1/auth/", "/api/v1/x/core.template/", "/api/v1/me/consent"}

// consentRefuses reports whether the request has to wait for the
// acceptance of the privacy policy.
func (s *Server) consentRefuses(ctx context.Context, c config.ConsentPrivacy, r *http.Request, p *identity.Principal) (bool, error) {
	if !c.Enabled || p == nil || p.Via != "session" || p.Admin() {
		return false, nil
	}
	path := r.URL.Path
	if !strings.HasPrefix(path, "/api/") || hasPrefixAny(path, consentOpen) {
		return false, nil
	}
	// the policy itself can be read
	if r.Method == http.MethodGet && path == "/api/v1/elements/"+strconv.FormatInt(c.Page, 10) {
		return false, nil
	}
	ok, err := s.privacyAccepted(ctx, p.UserID, consentVersion(c.Version))
	return !ok, err
}

var errConsentRequired = apperr.New(http.StatusForbidden, "consent_required", "please accept the privacy policy first")

// routesConsent: the own consent, the settings and the proof.
func (s *Server) routesConsent() {
	// the own consent to the privacy policy
	// action: core.consent.mine
	s.handle("GET /api/v1/me/consent", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p := identity.From(r.Context())
		if p == nil || p.UserID == 0 {
			return nil, apperr.ErrUnauthorized
		}
		c := s.app.Config.Get().Consent.Privacy
		out := map[string]any{"enabled": c.Enabled, "version": consentVersion(c.Version), "page": c.Page, "accepted_version": 0, "accepted_at": nil}
		var v int
		var at time.Time
		err := s.app.DB.QueryRow(r.Context(), `SELECT version, accepted_at FROM consents WHERE user_id=$1 AND kind='privacy' ORDER BY version DESC LIMIT 1`, p.UserID).Scan(&v, &at)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			out["accepted_version"], out["accepted_at"] = v, at
		}
		return out, nil
	})
	// accept the privacy policy in the version in force
	// action: core.consent.accept
	s.handle("POST /api/v1/me/consent", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p := identity.From(r.Context())
		if p == nil || p.UserID == 0 {
			return nil, apperr.ErrUnauthorized
		}
		var in struct {
			Kind    string `json:"kind"`
			Version int    `json:"version"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		c := s.app.Config.Get().Consent.Privacy
		if in.Kind != "privacy" || !c.Enabled {
			return nil, apperr.BadRequest("only the privacy policy can be accepted, and only while it is asked for")
		}
		if in.Version != consentVersion(c.Version) {
			// the policy changed while the page was open: read it again
			return nil, apperr.New(http.StatusConflict, "consent_version", "the privacy policy has changed; please read the current version")
		}
		tx, err := s.app.DB.Begin(r.Context())
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback(r.Context()) }()
		tag, err := tx.Exec(r.Context(), `INSERT INTO consents (user_id, kind, version) VALUES ($1, 'privacy', $2) ON CONFLICT DO NOTHING`, p.UserID, in.Version)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() > 0 {
			if err := audit.Log(r.Context(), tx, "consent.privacy.accept", "user", p.UserID, nil, map[string]any{"version": in.Version}); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			return nil, err
		}
		s.consentOK.Store(fmt.Sprintf("%d/%d", p.UserID, in.Version), true)
		return map[string]any{"accepted_version": in.Version}, nil
	})
	// the settings of the asking for consent
	// action: core.consent.get
	s.handle("GET /api/v1/admin/consent", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.app.Config.Get().Consent, nil
	})
	// change the asking for consent (a higher version asks everybody again)
	// action: core.consent.set
	s.handle("PUT /api/v1/admin/consent", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		var in config.Consent
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		for _, m := range []map[string]string{in.Cookies.Text, in.Privacy.Text} {
			for l, t := range m {
				if m[l] = strings.TrimSpace(t); m[l] == "" {
					delete(m, l)
				}
			}
		}
		before := s.app.Config.Get().Consent
		// no version given: the version stays
		if in.Cookies.Version == 0 {
			in.Cookies.Version = before.Cookies.Version
		}
		if in.Privacy.Version == 0 {
			in.Privacy.Version = before.Privacy.Version
		}
		// versions only go up: lowering one would count old consents again
		if in.Cookies.Version < before.Cookies.Version || in.Privacy.Version < before.Privacy.Version {
			return nil, apperr.BadRequest("a version cannot go down; to ask everybody again, raise it")
		}
		if err := config.ValidateConsent(in); err != nil {
			return nil, apperr.BadRequest("%s", err.Error())
		}
		if in.Privacy.Enabled {
			if _, err := s.app.Content.GetElement(r.Context(), in.Privacy.Page, "", false); err != nil {
				return nil, apperr.BadRequest("the page with the privacy policy does not exist or you may not see it")
			}
		}
		if err := s.app.Config.Update(func(c *config.Config) error { c.Consent = in; return nil }); err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), s.app.DB, "consent.update", "settings", 0, before, in)
		return in, nil
	})
	// who accepted which version of the privacy policy, and when (proof)
	// action: core.consent.list
	s.handle("GET /api/v1/admin/consents", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		version := h.QueryInt(r, "version", 0)
		limit := min(max(h.QueryInt(r, "limit", 100), 1), 10000)
		offset := max(h.QueryInt(r, "offset", 0), 0)
		csvOut := r.URL.Query().Get("format") == "csv"
		if csvOut {
			limit, offset = 1_000_000, 0
		}
		var total int
		if err := s.app.DB.QueryRow(r.Context(), `SELECT count(*) FROM consents WHERE kind='privacy' AND ($1 = 0 OR version = $1)`, version).Scan(&total); err != nil {
			return nil, err
		}
		rows, err := s.app.DB.Query(r.Context(), `SELECT c.user_id, u.username, u.display_name, c.version, c.accepted_at
			FROM consents c JOIN users u ON u.id = c.user_id
			WHERE c.kind='privacy' AND ($1 = 0 OR c.version = $1)
			ORDER BY c.accepted_at DESC, c.user_id LIMIT $2 OFFSET $3`, version, limit, offset)
		if err != nil {
			return nil, err
		}
		type item struct {
			UserID      int64     `json:"user_id"`
			Username    string    `json:"username"`
			DisplayName string    `json:"display_name"`
			Version     int       `json:"version"`
			AcceptedAt  time.Time `json:"accepted_at"`
			// Open: the caller may open the person (menu on the name)
			Open bool `json:"open,omitempty"`
		}
		items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (item, error) {
			var it item
			return it, r.Scan(&it.UserID, &it.Username, &it.DisplayName, &it.Version, &it.AcceptedAt)
		})
		if err != nil {
			return nil, err
		}
		if !csvOut {
			ids := make([]int64, 0, len(items))
			for _, it := range items {
				ids = append(ids, it.UserID)
			}
			refs, err := s.app.Content.PeopleRefs(r.Context(), ids)
			if err != nil {
				return nil, err
			}
			for i := range items {
				items[i].Open = refs[items[i].UserID].ID != nil
			}
			return map[string]any{"items": items, "total": total}, nil
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="privacy-consents.csv"`)
		_, _ = w.Write([]byte("\xef\xbb\xbf")) // BOM for spreadsheet programs
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"user_id", "username", "display_name", "version", "accepted_at"})
		for _, it := range items {
			_ = cw.Write([]string{strconv.FormatInt(it.UserID, 10), csvsafe.Cell(it.Username), csvsafe.Cell(it.DisplayName), strconv.Itoa(it.Version), it.AcceptedAt.UTC().Format(time.RFC3339)})
		}
		cw.Flush()
		return h.Handled{}, nil
	})
}
