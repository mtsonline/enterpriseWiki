package content

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/events"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Hook is called after content changes (plugins may subscribe, e.g. for
// notifications or external indexing). It must not block.
type Hook func(ctx context.Context, event string, entityType string, id int64)

// Service is the content service.
type Service struct {
	pool    *pgxpool.Pool
	acl     *acl.Service
	bus     *events.Bus
	cfg     *config.Store
	log     *slog.Logger
	types   typeRegistry
	macros  macroRegistry
	backend backendRegistry
	enabled func(pluginID string) bool
	// templateOK reports whether id is an active template plugin
	templateOK func(id string) bool
	hooks      []Hook
	durable    []durableHook // see changes.go
	actorCtx   func(ctx context.Context, uid int64) context.Context

	views     viewCounter
	resolvers map[string]SourceResolver

	mu             sync.Mutex
	publishOptions map[string]publishOption
	fields         map[string]elementField
}

// New creates the content service.
func New(pool *pgxpool.Pool, a *acl.Service, bus *events.Bus, cfg *config.Store, log *slog.Logger) *Service {
	s := &Service{pool: pool, acl: a, bus: bus, cfg: cfg, log: log, enabled: func(string) bool { return true }}
	s.OnChange(s.onChangeSlugs)
	return s
}

// SetTemplateFunc wires the check for active template plugins.
func (s *Service) SetTemplateFunc(f func(string) bool) { s.templateOK = f }

// SetEnabledFunc wires the plugin enabled check.
func (s *Service) SetEnabledFunc(f func(string) bool) { s.enabled = f }

// OnChange registers a hook.
func (s *Service) OnChange(h Hook) { s.hooks = append(s.hooks, h) }

// fire reports a change that is committed. Where the change was written
// to the queue of durable hooks in its own transaction use fireNoted.
func (s *Service) fire(ctx context.Context, event, etype string, id int64) {
	s.fireNoted(ctx, 0, event, etype, id)
}

// fireNoted is fire for a change whose queue entry (note) was committed
// with it; entry 0: it is queued now, before the caller gets the answer.
func (s *Service) fireNoted(ctx context.Context, entry int64, event, etype string, id int64) {
	dctx := context.WithoutCancel(ctx) // a closed browser must not cancel what follows from a stored change
	if entry == 0 && len(s.durable) > 0 {
		var err error
		for try := 0; try < 3; try++ { // the change is stored already: insist
			if entry, err = s.note(dctx, s.pool, event, etype, id); err == nil {
				break
			}
			time.Sleep(time.Duration(try+1) * 100 * time.Millisecond)
		}
		if err != nil {
			s.log.Error("queueing a change failed", "event", event, "type", etype, "id", id, "err", err)
		}
	}
	for _, h := range s.hooks {
		h(ctx, event, etype, id)
	}
	if len(s.durable) > 0 {
		s.deliver(dctx, s.pool, entry, nil, 0, event, etype, id)
	}
}

func (s *Service) aclChanged(ctx context.Context) {
	s.acl.Invalidate()
	s.bus.Publish(ctx, "acl", "")
}

// require checks that the caller has want on category id.
func (s *Service) require(ctx context.Context, categoryID int64, want acl.Perm) error {
	p := identity.From(ctx)
	if s.acl.Can(ctx, p, categoryID, want) {
		return nil
	}
	if p == nil {
		// Guests get 401 for anything they cannot see, so the UI can offer a login.
		return apperr.ErrUnauthorized
	}
	if !s.acl.Can(ctx, p, categoryID, acl.View) && !s.acl.Can(ctx, p, categoryID, acl.Manage) {
		return apperr.NotFound("category")
	}
	return apperr.ErrForbidden
}

// Require checks that the caller has want on category id (for plugins).
func (s *Service) Require(ctx context.Context, categoryID int64, want acl.Perm) error {
	return s.require(ctx, categoryID, want)
}

// ElementCategory returns the category of a (not deleted) element after
// checking that the caller has want on it.
func (s *Service) ElementCategory(ctx context.Context, elementID int64, want acl.Perm) (int64, error) {
	var cat int64
	err := s.pool.QueryRow(ctx, `SELECT e.category_id FROM elements e JOIN categories c ON c.id=e.category_id
		WHERE e.id=$1 AND e.deleted_at IS NULL AND c.deleted_at IS NULL`, elementID).Scan(&cat)
	if err != nil {
		if identity.From(ctx) == nil {
			return 0, apperr.ErrUnauthorized
		}
		return 0, apperr.NotFound("element")
	}
	if err := s.require(ctx, cat, want); err != nil {
		if errors.Is(err, apperr.ErrUnauthorized) || errors.Is(err, apperr.ErrForbidden) {
			return cat, err
		}
		return 0, apperr.NotFound("element")
	}
	return cat, nil
}

func requireAdmin(ctx context.Context) error {
	p := identity.From(ctx)
	if p == nil {
		return apperr.ErrUnauthorized
	}
	if !p.Admin() {
		return apperr.ErrForbidden
	}
	return nil
}

// Languages returns the configured content languages.
func (s *Service) Languages() []string { return s.cfg.Get().I18n.Languages }

// DefaultLanguage returns the configured default language.
func (s *Service) DefaultLanguage() string { return s.cfg.Get().I18n.DefaultLanguage }

func (s *Service) validLang(l string) bool {
	for _, x := range s.Languages() {
		if x == l {
			return true
		}
	}
	return false
}

// normalizeTexts validates a language map (titles, descriptions).
func (s *Service) normalizeTexts(m map[string]string, maxLen int, required bool) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range m {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !config.ValidLanguage(k) {
			return nil, apperr.BadRequest("invalid language %q", k)
		}
		if utf8.RuneCountInString(v) > maxLen {
			return nil, apperr.BadRequest("text too long (max %d characters)", maxLen)
		}
		out[k] = v
	}
	if required && len(out) == 0 {
		return nil, apperr.BadRequest("a title in at least one language is required")
	}
	return out, nil
}

var tagRe = regexp.MustCompile(`^[\p{L}\p{N}_.+#-]{1,50}$`)

// NormalizeTags lowercases, trims, validates and de-duplicates tags.
func NormalizeTags(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#")))
		if t == "" || seen[t] {
			continue
		}
		if !tagRe.MatchString(t) {
			return nil, apperr.BadRequest("invalid tag %q (letters, digits, _ . + # - ; max 50 characters)", t)
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > 30 {
		return nil, apperr.BadRequest("at most 30 tags allowed")
	}
	return out, nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify builds a URL slug from a title.
func Slugify(s string) string {
	s = strings.ToLower(s)
	r := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", "é", "e", "è", "e", "à", "a", "ç", "c")
	s = slugRe.ReplaceAllString(r.Replace(s), "-")
	s = strings.Trim(s, "-")
	if len(s) > 80 {
		s = strings.Trim(s[:80], "-")
	}
	return s
}

// firstText returns the text in lang, the default language or any language.
func firstText(m map[string]string, lang, def string) string {
	if v := m[lang]; v != "" {
		return v
	}
	if v := m[def]; v != "" {
		return v
	}
	for _, v := range m {
		return v
	}
	return ""
}

func auditLog(ctx context.Context, s *Service, action, etype string, id int64, before, after any) error {
	return audit.Log(ctx, s.pool, action, etype, id, before, after)
}

// Touch reports a change of an element that is stored outside its
// revisions (e.g. events of a calendar): watchers are notified and caches
// invalidated, no revision is created.
func (s *Service) Touch(ctx context.Context, elementID int64) {
	s.fire(ctx, "update", "element", elementID)
}
