package content

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// ReservedShortURLs are first path segments used by the application.
var ReservedShortURLs = map[string]bool{
	"api": true, "assets": true, "admin": true, "c": true, "e": true, "login": true, "logout": true, "search": true,
	"profile": true, "trash": true, "tasks": true, "tags": true, "healthz": true, "readyz": true, "favicon.ico": true,
	"robots.txt": true, "index.html": true, "sso": true, "auth": true, "files": true, "x": true, "feeds": true,
	"static": true, "plugins": true, "setup": true, "help": true, "sitemap.xml": true,
}

// NormalizeShortURL cleans a short URL ("Mein Kalender" -> "mein-kalender",
// "/team/hr/" -> "team/hr"). An empty result removes the short URL.
func NormalizeShortURL(raw string) (string, error) {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	var segs []string
	for _, seg := range strings.Split(raw, "/") {
		s := Slugify(seg)
		if s == "" {
			return "", apperr.BadRequest("invalid short URL %q: use letters, digits and - (segments separated by /)", raw)
		}
		segs = append(segs, s)
	}
	if len(segs) > 4 {
		return "", apperr.BadRequest("a short URL may have at most 4 segments")
	}
	out := strings.Join(segs, "/")
	if len(out) > 120 {
		return "", apperr.BadRequest("a short URL may have at most 120 characters")
	}
	if ReservedShortURLs[segs[0]] {
		return "", apperr.BadRequest("the short URL /%s is reserved by the application", segs[0])
	}
	return out, nil
}

// setShortURL stores a (normalized) short URL after checking that it is not
// used by another element or category.
func (s *Service) setShortURL(ctx context.Context, tx pgx.Tx, etype string, id int64, short string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('ewiki.short_url'))`); err != nil {
		return err
	}
	if short != "" {
		var otherType string
		var otherID int64
		err := tx.QueryRow(ctx, `SELECT 'element', id FROM elements WHERE short_url=$1 AND NOT ($2 = 'element' AND id = $3)
			UNION ALL SELECT 'category', id FROM categories WHERE short_url=$1 AND NOT ($2 = 'category' AND id = $3) LIMIT 1`, short, etype, id).Scan(&otherType, &otherID)
		if err == nil {
			return apperr.Conflict("the short URL /%s is already used by %s %d", short, otherType, otherID)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	table := "elements"
	if etype == "category" {
		table = "categories"
	}
	_, err := tx.Exec(ctx, `UPDATE `+table+` SET short_url=NULLIF($2, '') WHERE id=$1`, id, short)
	return err
}

// SetShortURL changes the short URL of an element or category (Edit permission).
func (s *Service) SetShortURL(ctx context.Context, etype string, id int64, raw string) (string, error) {
	short, err := NormalizeShortURL(raw)
	if err != nil {
		return "", err
	}
	var cat int64
	switch etype {
	case "element":
		if cat, err = s.ElementCategory(ctx, id, acl.Edit); err != nil {
			return "", err
		}
	case "category":
		cat = id
		if err := s.require(ctx, cat, acl.Edit); err != nil {
			return "", err
		}
	default:
		return "", apperr.BadRequest("type must be element or category")
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.setShortURL(ctx, tx, etype, id, short); err != nil {
			return err
		}
		return auditLog(ctx, s, etype+".short_url", etype, id, nil, map[string]string{"short_url": short})
	})
	if err != nil {
		return "", err
	}
	s.fire(ctx, "update", etype, id)
	return short, nil
}

// Resolved is the target of a short URL.
type Resolved struct {
	Type string `json:"type"` // element | category
	ID   int64  `json:"id"`
}

// Resolve finds the element or category of a short URL the caller may view.
func (s *Service) Resolve(ctx context.Context, raw string) (*Resolved, error) {
	short, err := NormalizeShortURL(raw)
	if err != nil || short == "" {
		return nil, apperr.NotFound("page")
	}
	r := &Resolved{}
	var cat int64
	err = s.pool.QueryRow(ctx, `SELECT 'element', e.id, e.category_id FROM elements e WHERE e.short_url=$1 AND e.deleted_at IS NULL
		UNION ALL SELECT 'category', c.id, c.id FROM categories c WHERE c.short_url=$1 AND c.deleted_at IS NULL LIMIT 1`, short).Scan(&r.Type, &r.ID, &cat)
	if err != nil {
		return nil, apperr.NotFound("page")
	}
	if !s.acl.Can(ctx, identity.From(ctx), cat, acl.View) {
		if identity.From(ctx) == nil {
			return nil, apperr.ErrUnauthorized
		}
		return nil, apperr.NotFound("page")
	}
	return r, nil
}
