// Package i18n aggregates the UI strings of the core and all plugins and
// tracks how complete each language is.
//
// Every component ships <lang>.json files (flat "key": "text" maps). English
// ("en") is the reference language: its keys define what must be translated.
// Translations edited in the admin UI (or imported) are stored in the
// database and take precedence over the shipped files; they remember which
// English text they translate, so a changed English text marks them as
// outdated.
//
// Plural texts have one key per CLDR plural form ("inbox.new_one",
// "inbox.new_other"); a language provides the forms it needs (Polish:
// one, few, many, other). The form is chosen by the parameter "count" (or
// "n").
package i18n

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/events"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Reference is the reference language of all UI strings.
const Reference = "en"

// ManagePerm allows everything: languages, all translations, import/export.
const ManagePerm = "core.i18n.manage"

const translatePrefix = "core.i18n.translate."

// TranslatePerm is the permission to translate the UI into one language.
func TranslatePerm(lang string) string {
	return translatePrefix + strings.ReplaceAll(strings.ToLower(lang), "-", "_")
}

// Service manages UI translations.
type Service struct {
	pool  *pgxpool.Pool
	cfg   *config.Store
	bus   *events.Bus
	mu    sync.RWMutex
	files map[string]map[string]string // lang -> key -> text (from all sources)
	owner map[string]string            // key -> source (core / plugin id), English keys
	cache map[string]map[string]string // merged per lang
}

// New creates the i18n service.
func New(pool *pgxpool.Pool, cfg *config.Store, bus *events.Bus) *Service {
	s := &Service{pool: pool, cfg: cfg, bus: bus, files: map[string]map[string]string{}, owner: map[string]string{}}
	inv := func(string) { s.mu.Lock(); s.cache = nil; s.mu.Unlock() }
	bus.Subscribe("i18n", inv)
	bus.Subscribe("*", inv)
	s.syncPerms(cfg.Get().I18n.Languages)
	cfg.OnChange(func(c *config.Config) { s.syncPerms(c.I18n.Languages) })
	return s
}

var (
	siteLangsMu sync.Mutex
	siteLangs   = map[string][]string{}
)

// syncPerms declares one translate permission per configured language (of
// all sites: the permission list is shared).
func (s *Service) syncPerms(langs []string) {
	site := s.cfg.Site()
	siteLangsMu.Lock()
	siteLangs[site] = langs
	all := []string{}
	seen := map[string]bool{}
	for _, ls := range siteLangs {
		for _, l := range ls {
			if !seen[l] {
				seen[l] = true
				all = append(all, l)
			}
		}
	}
	siteLangsMu.Unlock()
	sort.Strings(all)
	defs := make([]perm.Def, 0, len(all))
	for _, l := range all {
		name := l
		if tag, err := language.Parse(l); err == nil {
			if n := display.English.Tags().Name(tag); n != "" {
				name = n
			}
		}
		defs = append(defs, perm.Def{Key: TranslatePerm(l), Scope: perm.Global,
			Label:       fmt.Sprintf("Translate the user interface: %s", name),
			Description: fmt.Sprintf("Translate, import and export the user interface texts in %s (%s).", name, l)})
	}
	_ = perm.Replace("core", translatePrefix, defs...)
}

// CanTranslate reports whether the caller may translate the UI into lang.
func CanTranslate(ctx context.Context, lang string) bool {
	return perm.Has(ctx, ManagePerm) || perm.Has(ctx, TranslatePerm(lang))
}

func (s *Service) requireTranslate(ctx context.Context, lang string) error {
	if identity.From(ctx) == nil {
		return apperr.ErrUnauthorized
	}
	if !s.configured(lang) {
		return apperr.NotFound("language")
	}
	if !CanTranslate(ctx, lang) {
		return apperr.ErrForbidden
	}
	return nil
}

func (s *Service) configured(lang string) bool {
	for _, l := range s.cfg.Get().I18n.Languages {
		if l == lang {
			return true
		}
	}
	return lang == Reference
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

type override struct{ value, hash string }

func (s *Service) overrides(ctx context.Context, lang string) (map[string]override, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value, source_hash FROM ui_translations WHERE lang=$1`, lang)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]override{}
	for rows.Next() {
		var k string
		var o override
		if err := rows.Scan(&k, &o.value, &o.hash); err != nil {
			return nil, err
		}
		m[k] = o
	}
	return m, rows.Err()
}

// knownKey reports whether key may be translated: an English key or a
// plural form of an English plural text. Callers hold s.mu.
func (s *Service) knownKey(key string) bool {
	if _, ok := s.owner[key]; ok {
		return true
	}
	base, _, ok := splitForm(key)
	if !ok {
		return false
	}
	_, ok = s.owner[base+"_other"]
	return ok
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
	for k, o := range refOv {
		if s.knownKey(k) {
			out[k] = o.value
		}
	}
	if lang != Reference {
		for k, v := range s.files[lang] {
			if s.knownKey(k) {
				out[k] = v
			}
		}
		for k, o := range ov {
			if s.knownKey(k) {
				out[k] = o.value
			}
		}
	}
	if s.cache == nil {
		s.cache = map[string]map[string]string{}
	}
	s.cache[lang] = out
	return out, nil
}

// String is one translatable text: a single string or a plural text with
// one value per plural form.
type String struct {
	Key    string `json:"key"`    // the key (plural texts: without _one/_other)
	Source string `json:"source"` // component that defines it (core or plugin id)
	Plural bool   `json:"plural"`
	// Forms are the plural forms of the language (plural texts only).
	Forms []string `json:"forms,omitempty"`
	// Reference holds the English text per form ("" for a single string).
	Reference map[string]string `json:"reference"`
	// Value holds the text in the language per form (missing forms absent).
	Value map[string]string `json:"value"`
	// Custom lists the forms translated in the administration.
	Custom []string `json:"custom,omitempty"`
	// Status: file (shipped), custom, outdated (the English text changed
	// since it was translated) or missing.
	Status string `json:"status"`
	// Hash identifies the English text (sent back to confirm an outdated translation).
	Hash string `json:"hash"`
}

// unit is a translatable text of the reference language.
type unit struct {
	key    string
	plural bool
	ref    map[string]string // form -> English ("" for single strings)
}

// units returns all texts of the reference language (callers hold s.mu).
func (s *Service) units(refOv map[string]override) []unit {
	en := func(k string) string {
		if o, ok := refOv[k]; ok && o.value != "" {
			return o.value
		}
		return s.files[Reference][k]
	}
	byKey := map[string]*unit{}
	for k := range s.files[Reference] {
		if base, form, ok := splitForm(k); ok {
			if _, isPlural := s.files[Reference][base+"_other"]; isPlural {
				u := byKey[base]
				if u == nil {
					u = &unit{key: base, plural: true, ref: map[string]string{}}
					byKey[base] = u
				}
				u.ref[form] = en(k)
				continue
			}
		}
		byKey[k] = &unit{key: k, ref: map[string]string{"": en(k)}}
	}
	out := make([]unit, 0, len(byKey))
	for _, u := range byKey {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// hash identifies the English text of a unit.
func (u unit) hash() string {
	keys := make([]string, 0, len(u.ref))
	for k := range u.ref {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\x00", k, u.ref[k])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// formKey is the message key of a form ("" = single string).
func (u unit) formKey(form string) string {
	if form == "" {
		return u.key
	}
	return u.key + "_" + form
}

// forms returns the forms a translation of u into lang needs.
func (u unit) forms(lang string) []string {
	if !u.plural {
		return []string{""}
	}
	if lang == Reference {
		var out []string
		for _, f := range Forms {
			if _, ok := u.ref[f]; ok {
				out = append(out, f)
			}
		}
		return out
	}
	return PluralForms(lang)
}

// strings builds the translation state of every text (callers hold s.mu).
func (s *Service) strings(lang string, refOv, ov map[string]override) []String {
	units := s.units(refOv)
	out := make([]String, 0, len(units))
	for _, u := range units {
		st := String{Key: u.key, Plural: u.plural, Reference: map[string]string{}, Value: map[string]string{}, Hash: u.hash()}
		st.Source = s.owner[u.formKey("other")]
		if !u.plural {
			st.Source = s.owner[u.key]
		}
		forms := u.forms(lang)
		if u.plural {
			st.Forms = forms
		}
		missing, outdated := false, false
		for _, f := range forms {
			ref, ok := u.ref[f]
			if !ok {
				ref = u.ref["other"] // e.g. "few" in Polish: English has no such form
			}
			st.Reference[f] = ref
			k := u.formKey(f)
			v := s.files[lang][k]
			if lang == Reference {
				v = s.files[Reference][k]
			}
			if o, ok := ov[k]; ok && o.value != "" {
				v = o.value
				st.Custom = append(st.Custom, f)
				if lang != Reference && o.hash != "" && o.hash != st.Hash {
					outdated = true
				}
			}
			if v == "" {
				missing = true
				continue
			}
			st.Value[f] = v
		}
		switch {
		case missing:
			st.Status = "missing"
		case outdated:
			st.Status = "outdated"
		case len(st.Custom) > 0:
			st.Status = "custom"
		default:
			st.Status = "file"
		}
		out = append(out, st)
	}
	return out
}

// Strings lists all texts of a language for the translation editor.
func (s *Service) Strings(ctx context.Context, lang string) ([]String, error) {
	if err := s.requireTranslate(ctx, lang); err != nil {
		return nil, err
	}
	return s.loadStrings(ctx, lang)
}

func (s *Service) loadStrings(ctx context.Context, lang string) ([]String, error) {
	refOv, err := s.overrides(ctx, Reference)
	if err != nil {
		return nil, err
	}
	ov := refOv
	if lang != Reference {
		if ov, err = s.overrides(ctx, lang); err != nil {
			return nil, err
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.strings(lang, refOv, ov), nil
}

// LangStatus describes the translation progress of a language.
type LangStatus struct {
	Lang       string `json:"lang"`
	Total      int    `json:"total"`
	Translated int    `json:"translated"` // complete and current
	Outdated   int    `json:"outdated"`
	Missing    int    `json:"missing"`
	Percent    int    `json:"percent"`
	Default    bool   `json:"default"`
	// CanTranslate: the caller may translate this language.
	CanTranslate bool `json:"can_translate"`
}

// Status returns the progress of every configured language.
func (s *Service) Status(ctx context.Context) ([]LangStatus, error) {
	cfg := s.cfg.Get().I18n
	out := []LangStatus{}
	for _, lang := range cfg.Languages {
		list, err := s.loadStrings(ctx, lang)
		if err != nil {
			return nil, err
		}
		st := Summarize(lang, list)
		st.Default = lang == cfg.DefaultLanguage
		st.CanTranslate = identity.From(ctx) != nil && CanTranslate(ctx, lang)
		out = append(out, st)
	}
	return out, nil
}

// Summarize counts the states of a list of texts.
func Summarize(lang string, list []String) LangStatus {
	st := LangStatus{Lang: lang, Total: len(list)}
	for _, x := range list {
		switch x.Status {
		case "missing":
			st.Missing++
		case "outdated":
			st.Outdated++
		default:
			st.Translated++
		}
	}
	st.Percent = 100
	if st.Total > 0 {
		st.Percent = st.Translated * 100 / st.Total
	}
	return st
}

// SetResult reports what SetStrings and Import changed.
type SetResult struct {
	Written   int      `json:"written"`
	Removed   int      `json:"removed"`
	Unchanged int      `json:"unchanged"`
	Unknown   []string `json:"unknown,omitempty"` // keys that do not exist (ignored on import)
}

// SetStrings stores translations made in the administration. Keys are
// single keys or plural form keys (inbox.new_few); an empty value removes
// the custom text (the shipped text applies again). Saving a text again
// confirms it for the current English text (no longer outdated).
func (s *Service) SetStrings(ctx context.Context, lang string, values map[string]string) (SetResult, error) {
	if err := s.requireTranslate(ctx, lang); err != nil {
		return SetResult{}, err
	}
	res, err := s.set(ctx, lang, values, false)
	if err == nil && len(res.Unknown) > 0 {
		return res, apperr.BadRequest("unknown keys: %s", strings.Join(res.Unknown, ", "))
	}
	return res, err
}

// set writes translations; with lenient, unknown keys and empty values are
// skipped instead of removing custom texts (imports).
func (s *Service) set(ctx context.Context, lang string, values map[string]string, lenient bool) (SetResult, error) {
	res := SetResult{}
	refOv, err := s.overrides(ctx, Reference)
	if err != nil {
		return res, err
	}
	ov, err := s.overrides(ctx, lang)
	if err != nil {
		return res, err
	}
	type row struct{ key, value, hash string }
	var writes []row
	var removes []string
	s.mu.RLock()
	hashOf := map[string]string{} // translatable key -> hash of its English text
	for _, u := range s.units(refOv) {
		for _, f := range u.forms(lang) {
			hashOf[u.formKey(f)] = u.hash()
		}
	}
	file := s.files[lang]
	if lang == Reference {
		file = s.files[Reference]
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := strings.TrimSpace(values[k])
		h, ok := hashOf[k]
		if !ok {
			res.Unknown = append(res.Unknown, k)
			continue
		}
		if len(v) > 5000 {
			s.mu.RUnlock()
			return res, apperr.BadRequest("%s: text too long", k)
		}
		cur, has := ov[k]
		switch {
		case v == "" && lenient:
			res.Unchanged++
		case v == "":
			if has {
				removes = append(removes, k)
			} else {
				res.Unchanged++
			}
		case v == file[k] && !has:
			res.Unchanged++ // same as the shipped text
		case has && cur.value == v && cur.hash == h:
			res.Unchanged++
		default:
			writes = append(writes, row{k, v, h})
		}
	}
	s.mu.RUnlock()
	if len(writes) == 0 && len(removes) == 0 {
		return res, nil
	}
	uid := identity.From(ctx).ID()
	var uptr *int64
	if uid != 0 {
		uptr = &uid
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, k := range removes {
			if _, err := tx.Exec(ctx, `DELETE FROM ui_translations WHERE lang=$1 AND key=$2`, lang, k); err != nil {
				return err
			}
		}
		for _, w := range writes {
			if _, err := tx.Exec(ctx, `INSERT INTO ui_translations(lang, key, value, source_hash, updated_by) VALUES ($1,$2,$3,$4,$5)
				ON CONFLICT (lang, key) DO UPDATE SET value=EXCLUDED.value, source_hash=EXCLUDED.source_hash, updated_by=EXCLUDED.updated_by, updated_at=now()`,
				lang, w.key, w.value, w.hash, uptr); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	res.Written, res.Removed = len(writes), len(removes)
	s.bus.Publish(ctx, "i18n", lang)
	return res, nil
}

// T translates key into lang (falling back to the reference language) and
// replaces {name} placeholders. For plural texts (key_one, key_other, …)
// the parameter "count" (or "n") selects the form. Missing keys return the
// key itself.
func (s *Service) T(ctx context.Context, lang, key string, params map[string]string) string {
	msgs, err := s.Messages(ctx, lang)
	if err != nil {
		return key
	}
	return Format(msgs, lang, key, params)
}

// Format looks up key in msgs (with plural forms) and fills in params.
func Format(msgs map[string]string, lang, key string, params map[string]string) string {
	v, ok := msgs[key]
	if n, has := countParam(params); has {
		if _, plural := msgs[key+"_other"]; plural {
			if x := msgs[key+"_"+PluralForm(lang, n)]; x != "" {
				v, ok = x, true
			} else {
				v, ok = msgs[key+"_other"], true
			}
		}
	} else if !ok {
		v, ok = msgs[key+"_other"]
	}
	if !ok || v == "" {
		return key
	}
	for k, p := range params {
		v = strings.ReplaceAll(v, "{"+k+"}", p)
	}
	return v
}
