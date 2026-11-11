package auth

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// ProfileFields are the fields of a user's profile ("business card").
// photo is the SHA-256 of an uploaded image (GET /api/v1/files/{hash});
// photo_visibility is the person's choice who sees it (see PhotoPolicy).
var ProfileFields = []string{"job_title", "department", "phone", "mobile", "office", "about", "photo", "photo_visibility"}

// Visibility levels of profile photos, narrowest first.
var photoLevels = map[string]int{"private": 0, "users": 1, "public": 2}

// PhotoPolicy is the administrators' rule for profile photos.
type PhotoPolicy struct {
	Default string `json:"default"` // private | users | public
	Max     string `json:"max"`     // widest a person may choose
	Fixed   bool   `json:"fixed"`   // no choice: always Default
}

// PhotoPolicy returns the configured rule (default: signed-in people see
// photos, a person may make theirs public or private).
func (s *Service) PhotoPolicy() PhotoPolicy {
	a := s.cfg.Get().Auth
	p := PhotoPolicy{Default: a.PhotoVisibility, Max: a.PhotoVisibilityMax, Fixed: a.PhotoVisibilityFixed}
	if _, ok := photoLevels[p.Default]; !ok {
		p.Default = "users"
	}
	if _, ok := photoLevels[p.Max]; !ok {
		p.Max = "public"
	}
	if photoLevels[p.Default] > photoLevels[p.Max] {
		p.Default = p.Max
	}
	return p
}

// Effective returns who sees a photo for the person's choice.
func (p PhotoPolicy) Effective(choice string) string {
	if _, ok := photoLevels[choice]; !ok || p.Fixed {
		choice = p.Default
	}
	if photoLevels[choice] > photoLevels[p.Max] {
		choice = p.Max
	}
	return choice
}

// PhotoVisible reports whether the caller of ctx may see the photo of the
// person uid whose own choice is choice.
func (s *Service) PhotoVisible(ctx context.Context, uid int64, choice string) bool {
	p := identity.From(ctx)
	switch s.PhotoPolicy().Effective(choice) {
	case "public":
		return true
	case "users":
		return p != nil && p.UserID > 0
	default:
		return p != nil && p.UserID > 0 && (p.UserID == uid || p.Admin() || perm.Has(ctx, "core.users.manage"))
	}
}

// PhotoHashVisible reports whether the caller may see the image hash as the
// profile photo of somebody (any person using it who allows it).
func (s *Service) PhotoHashVisible(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, hash string) bool {
	rows, err := q.Query(ctx, `SELECT id, COALESCE(profile->>'photo_visibility', '') FROM users WHERE profile->>'photo' = $1 LIMIT 50`, hash)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var uid int64
		var choice string
		if rows.Scan(&uid, &choice) == nil && s.PhotoVisible(ctx, uid, choice) {
			return true
		}
	}
	return false
}

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
		if err := s.requireShared(ctx); err != nil {
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
		if k == "photo_visibility" && v != "" {
			pol := s.PhotoPolicy()
			if _, ok := photoLevels[v]; !ok {
				return nil, apperr.BadRequest("photo_visibility must be private, users or public")
			}
			if pol.Fixed && v != pol.Default {
				return nil, apperr.BadRequest("the visibility of profile photos is fixed (%s)", pol.Default)
			}
			if photoLevels[v] > photoLevels[pol.Max] {
				return nil, apperr.BadRequest("profile photos may be visible at most to: %s", pol.Max)
			}
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
