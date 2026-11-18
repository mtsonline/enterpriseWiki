package i18n

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Labels per instance: a plugin declares which of its UI texts may be
// changed per instance (a blog's "New post", a FAQ's "Ask a question" …;
// plugin.json "instance_labels"). The text of one instance is stored as a
// UI translation with the key "<key>@<plugin>/<instance>" in every language
// – never in the instance itself – and falls back to the plugin's text.
// The instance's labels are read with the page (InstanceLabels) and changed
// by those who manage the instance (SetInstanceLabels, the caller checks the
// permission). Administrators with core.i18n.manage also see them in the
// translation list, export and import. Purging an instance removes its
// labels (content.RemoveRefs).

// MaxLabel is the longest label (characters).
const MaxLabel = 200

var instKeyRe = regexp.MustCompile(`^([a-z0-9][a-z0-9._-]{0,150})@([a-z0-9][a-z0-9._-]{0,80})/([1-9][0-9]{0,18})$`)

// InstanceKey is the UI key of a label of one instance.
func InstanceKey(base, plugin string, id int64) string {
	return base + "@" + plugin + "/" + strconv.FormatInt(id, 10)
}

// parseInstanceKey splits an instance key.
func parseInstanceKey(k string) (base, plugin string, id int64, ok bool) {
	m := instKeyRe.FindStringSubmatch(k)
	if m == nil {
		return "", "", 0, false
	}
	id, err := strconv.ParseInt(m[3], 10, 64)
	return m[1], m[2], id, err == nil
}

// RegisterInstanceLabels declares the texts of owner that may be changed per
// instance; each must be a text of owner in English.
func (s *Service) RegisterInstanceLabels(owner string, keys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.labelable == nil {
		s.labelable = map[string]string{}
	}
	for _, k := range keys {
		if s.owner[k] != owner {
			return apperr.BadRequest("%s: instance label %q is not a text of the plugin (en.json)", owner, k)
		}
		if strings.HasSuffix(k, "_other") || strings.HasSuffix(k, "_one") {
			return apperr.BadRequest("%s: plural texts cannot be instance labels (%s)", owner, k)
		}
		s.labelable[k] = owner
	}
	return nil
}

// InstanceLabelKeys returns the texts of owner that may be changed per instance.
func (s *Service) InstanceLabelKeys(owner string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []string{}
	for k, o := range s.labelable {
		if o == owner {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// knownInstanceKey reports whether k is a label of an instance of a plugin
// that declared its base text. Callers hold s.mu.
func (s *Service) knownInstanceKey(k string) bool {
	base, plugin, _, ok := parseInstanceKey(k)
	return ok && s.labelable[base] == plugin
}

// InstanceLabel is one changeable text of an instance.
type InstanceLabel struct {
	Key string `json:"key"`
	// Default is the plugin's text per configured language.
	Default map[string]string `json:"default"`
	// Value is the text of the instance per language (only where set).
	Value map[string]string `json:"value"`
}

// InstanceLabels returns the changeable texts of instance id of plugin.
func (s *Service) InstanceLabels(ctx context.Context, plugin string, id int64) ([]InstanceLabel, error) {
	langs := s.cfg.Get().I18n.Languages
	keys := s.InstanceLabelKeys(plugin)
	ikeys := make([]string, len(keys))
	for i, k := range keys {
		ikeys[i] = InstanceKey(k, plugin, id)
	}
	rows, err := s.pool.Query(ctx, `SELECT lang, key, value FROM ui_translations WHERE key = ANY($1)`, ikeys)
	if err != nil {
		return nil, err
	}
	vals := map[string]map[string]string{} // instance key -> lang -> value
	for rows.Next() {
		var lang, k, v string
		if err := rows.Scan(&lang, &k, &v); err != nil {
			rows.Close()
			return nil, err
		}
		if vals[k] == nil {
			vals[k] = map[string]string{}
		}
		vals[k][lang] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]InstanceLabel, 0, len(keys))
	for i, k := range keys {
		l := InstanceLabel{Key: k, Default: map[string]string{}, Value: map[string]string{}}
		for _, lang := range langs {
			l.Default[lang] = s.T(ctx, lang, k, nil)
			if v, ok := vals[ikeys[i]][lang]; ok {
				l.Value[lang] = v
			}
		}
		out = append(out, l)
	}
	return out, nil
}

// SetInstanceLabels stores the texts of instance id of plugin: values per
// language and key; an empty text removes it (the plugin's text applies).
// The caller checks that the person may manage the instance.
func (s *Service) SetInstanceLabels(ctx context.Context, plugin string, id int64, values map[string]map[string]string) error {
	langs := s.cfg.Get().I18n.Languages
	allowed := s.InstanceLabelKeys(plugin)
	for lang, kv := range values {
		if !slices.Contains(langs, lang) {
			return apperr.BadRequest("language %q is not active", lang)
		}
		for k, v := range kv {
			if !slices.Contains(allowed, k) {
				return apperr.BadRequest("%q cannot be changed per instance", k)
			}
			if err := checkLabel(k, v); err != nil {
				return err
			}
		}
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for lang, kv := range values {
			for k, v := range kv {
				ik := InstanceKey(k, plugin, id)
				v = strings.TrimSpace(v)
				if v == "" {
					if _, err := tx.Exec(ctx, `DELETE FROM ui_translations WHERE lang=$1 AND key=$2`, lang, ik); err != nil {
						return err
					}
					continue
				}
				if _, err := tx.Exec(ctx, `INSERT INTO ui_translations(lang, key, value, source_hash, updated_by) VALUES ($1,$2,$3,'',$4)
					ON CONFLICT (lang, key) DO UPDATE SET value=EXCLUDED.value, updated_by=EXCLUDED.updated_by, updated_at=now()`,
					lang, ik, v, uidOf(ctx)); err != nil {
					return err
				}
			}
		}
		// the change is in the audit log (same transaction)
		return audit.Log(ctx, tx, "instance.labels", "element", id, nil, values)
	})
	if err != nil {
		return err
	}
	s.bus.Publish(ctx, "i18n", "")
	return nil
}

// checkLabel: plain text of at most MaxLabel characters, one line.
func checkLabel(k, v string) error {
	if utf8.RuneCountInString(v) > MaxLabel {
		return apperr.BadRequest("%s: at most %d characters", k, MaxLabel)
	}
	if strings.ContainsFunc(v, func(r rune) bool { return unicode.IsControl(r) }) {
		return apperr.BadRequest("%s: one line of text", k)
	}
	return nil
}

func uidOf(ctx context.Context) *int64 {
	if uid := identity.From(ctx).ID(); uid != 0 {
		return &uid
	}
	return nil
}

// manages reports whether the caller may see and change instance labels in
// the translation list (they are not part of it for translators of one
// language: a label may belong to an instance they may not see).
func manages(ctx context.Context) bool {
	return perm.Has(ctx, ManagePerm)
}
