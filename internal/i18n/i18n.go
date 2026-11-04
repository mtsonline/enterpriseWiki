// Package i18n aggregates the UI strings of the core and all plugins and
// tracks how complete each language is.
//
// Every component ships <lang>.json files (flat "key": "text" maps). English
// ("en") is the reference language: its keys define what must be translated.
// Translations edited in the admin UI are stored in the database and take
// precedence over the shipped files.
package i18n

import (
	"context"
	"encoding/json"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/events"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Reference is the reference language of all UI strings.
const Reference = "en"

// Service manages UI translations.
type Service struct {
	pool  *pgxpool.Pool
	cfg   *config.Store
	bus   *events.Bus
	mu    sync.RWMutex
	files map[string]map[string]string // lang -> key -> text (from all sources)
	owner map[string]string            // key -> source (core / plugin id)
	cache map[string]map[string]string // merged per lang
}

// New creates the i18n service.
func New(pool *pgxpool.Pool, cfg *config.Store, bus *events.Bus) *Service {
	s := &Service{pool: pool, cfg: cfg, bus: bus, files: map[string]map[string]string{}, owner: map[string]string{}}
	inv := func(string) { s.mu.Lock(); s.cache = nil; s.mu.Unlock() }
	bus.Subscribe("i18n", inv)
	bus.Subscribe("*", inv)
	return s
}

// Register loads <lang>.json files of a component.
func (s *Service) Register(source string, files fs.FS) error {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return err
	}
	// every locale set must contain the reference language (English): it is
	// the fallback and the base for translations done in the admin area
	hasRef, hasAny := false, false
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			hasAny = true
			hasRef = hasRef || e.Name() == Reference+".json"
		}
	}
	if hasAny && !hasRef {
		return apperr.BadRequest("%s: locales must include %s.json", source, Reference)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		lang := strings.TrimSuffix(e.Name(), ".json")
		b, err := fs.ReadFile(files, e.Name())
		if err != nil {
			return err
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			return apperr.BadRequest("%s/%s: %v", source, e.Name(), err)
		}
		if s.files[lang] == nil {
			s.files[lang] = map[string]string{}
		}
		for k, v := range m {
			s.files[lang][k] = v
			if lang == Reference {
				s.owner[k] = source
			}
		}
	}
	s.cache = nil
	return nil
}

func (s *Service) overrides(ctx context.Context, lang string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value FROM ui_translations WHERE lang=$1`, lang)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// Messages returns all UI strings for lang, falling back to the reference
// language for missing keys.
func (s *Service) Messages(ctx context.Context, lang string) (map[string]string, error) {
	s.mu.RLock()
	if m, ok := s.cache[lang]; ok {
		s.mu.RUnlock()
		return m, nil
	}
	s.mu.RUnlock()
	out := map[string]string{}
	refOv, err := s.overrides(ctx, Reference)
	if err != nil {
		return nil, err
	}
	ov, err := s.overrides(ctx, lang)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.files[Reference] {
		out[k] = v
	}
	for k, v := range refOv {
		out[k] = v
	}
	if lang != Reference {
		for k, v := range s.files[lang] {
			if _, ok := s.owner[k]; ok {
				out[k] = v
			}
		}
		for k, v := range ov {
			out[k] = v
		}
	}
	if s.cache == nil {
		s.cache = map[string]map[string]string{}
	}
	s.cache[lang] = out
	return out, nil
}

// LangStatus describes the translation progress of a language.
type LangStatus struct {
	Lang       string `json:"lang"`
	Total      int    `json:"total"`
	Translated int    `json:"translated"`
	Percent    int    `json:"percent"`
	Default    bool   `json:"default"`
}

// Status returns the progress of every configured language.
func (s *Service) Status(ctx context.Context) ([]LangStatus, error) {
	cfg := s.cfg.Get().I18n
	out := []LangStatus{}
	for _, lang := range cfg.Languages {
		st, err := s.status(ctx, lang)
		if err != nil {
			return nil, err
		}
		st.Default = lang == cfg.DefaultLanguage
		out = append(out, st)
	}
	return out, nil
}

func (s *Service) status(ctx context.Context, lang string) (LangStatus, error) {
	ov, err := s.overrides(ctx, lang)
	if err != nil {
		return LangStatus{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := LangStatus{Lang: lang, Total: len(s.files[Reference])}
	for k := range s.files[Reference] {
		if lang == Reference || s.files[lang][k] != "" || ov[k] != "" {
			st.Translated++
		}
	}
	if st.Total > 0 {
		st.Percent = st.Translated * 100 / st.Total
	} else {
		st.Percent = 100
	}
	return st, nil
}

// String is one translatable UI string.
type String struct {
	Key       string `json:"key"`
	Source    string `json:"source"`    // component that defines the key
	Reference string `json:"reference"` // English text
	Value     string `json:"value"`     // current text in the language ("" = missing)
	Origin    string `json:"origin"`    // file | custom | missing
}

// Strings lists all strings of a language for the translation editor.
func (s *Service) Strings(ctx context.Context, lang string) ([]String, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	ov, err := s.overrides(ctx, lang)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]String, 0, len(s.files[Reference]))
	for k, ref := range s.files[Reference] {
		st := String{Key: k, Source: s.owner[k], Reference: ref, Origin: "missing"}
		if v := s.files[lang][k]; v != "" {
			st.Value, st.Origin = v, "file"
		}
		if lang == Reference {
			st.Value, st.Origin = ref, "file"
		}
		if v, ok := ov[k]; ok && v != "" {
			st.Value, st.Origin = v, "custom"
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// SetStrings stores custom translations; an empty value removes the custom text.
func (s *Service) SetStrings(ctx context.Context, lang string, values map[string]string) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	if !config.ValidLanguage(lang) {
		return apperr.BadRequest("invalid language")
	}
	uid := identity.From(ctx).ID()
	var uptr *int64
	if uid != 0 {
		uptr = &uid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for k, v := range values {
			if len(k) > 200 || len(v) > 5000 {
				return apperr.BadRequest("key or value too long")
			}
			if strings.TrimSpace(v) == "" {
				if _, err := tx.Exec(ctx, `DELETE FROM ui_translations WHERE lang=$1 AND key=$2`, lang, k); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO ui_translations(lang, key, value, updated_by) VALUES ($1,$2,$3,$4)
				ON CONFLICT (lang, key) DO UPDATE SET value=EXCLUDED.value, updated_by=EXCLUDED.updated_by, updated_at=now()`, lang, k, v, uptr); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.bus.Publish(ctx, "i18n", lang)
	return nil
}

func requireAdmin(ctx context.Context) error { return perm.Require(ctx, "core.i18n.manage") }

// T translates key into lang (falling back to the reference language) and
// replaces {name} placeholders. Missing keys return the key itself.
func (s *Service) T(ctx context.Context, lang, key string, params map[string]string) string {
	msgs, err := s.Messages(ctx, lang)
	if err != nil {
		return key
	}
	v, ok := msgs[key]
	if !ok || v == "" {
		return key
	}
	for k, p := range params {
		v = strings.ReplaceAll(v, "{"+k+"}", p)
	}
	return v
}
