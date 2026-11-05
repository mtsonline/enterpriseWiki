package auth

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// ProfileFields are the fields of a user's profile ("business card").
// photo is the SHA-256 of an uploaded image (GET /api/v1/files/{hash}).
var ProfileFields = []string{"job_title", "department", "phone", "mobile", "office", "about", "photo"}

var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Profile returns the profile of a user.
func (s *Service) Profile(ctx context.Context, uid int64) (map[string]string, error) {
	var p map[string]string
	if err := s.pool.QueryRow(ctx, `SELECT profile FROM users WHERE id=$1`, uid).Scan(&p); err != nil {
		return nil, apperr.NotFound("user")
	}
	if p == nil {
		p = map[string]string{}
	}
	return p, nil
}

// SetProfile replaces the given fields of a profile (empty = remove). The
// caller must be the user or an administrator.
func (s *Service) SetProfile(ctx context.Context, uid int64, in map[string]string) (map[string]string, error) {
	p := identity.From(ctx)
	if p == nil || (p.UserID != uid && !p.Admin()) {
		return nil, apperr.ErrForbidden
	}
	if p.UserID != uid {
		if err := requireShared(ctx); err != nil {
			return nil, err
		}
	}
	cur, err := s.Profile(ctx, uid)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, f := range ProfileFields {
		allowed[f] = true
	}
	for k, v := range in {
		if !allowed[k] {
			return nil, apperr.BadRequest("unknown profile field %q (allowed: %s)", k, strings.Join(ProfileFields, ", "))
		}
		v = strings.TrimSpace(v)
		max := 200
		if k == "about" {
			max = 2000
		}
		if utf8.RuneCountInString(v) > max {
			return nil, apperr.BadRequest("%s: at most %d characters", k, max)
		}
		if k == "photo" && v != "" {
			var mime string
			if !hashRe.MatchString(v) || s.pool.QueryRow(ctx, `SELECT mime FROM files WHERE hash=$1`, v).Scan(&mime) != nil ||
				!strings.HasPrefix(mime, "image/") || strings.Contains(mime, "svg") {
				return nil, apperr.BadRequest("photo must be an uploaded image (PNG, JPEG, GIF or WebP)")
			}
		}
		if v == "" {
			delete(cur, k)
		} else {
			cur[k] = v
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET profile=$2, updated_at=now() WHERE id=$1`, uid, cur); err != nil {
		return nil, err
	}
	return cur, nil
}
