package content

import (
	"context"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
)

// Category translations work like element translations: the title and
// description of the original language have a revision (text_rev); each
// translation remembers the revision it is based on. A changed original
// makes the translations outdated until they are revised or confirmed.

// CatTranslation is the translation state of a category in one language.
type CatTranslation struct {
	Lang   string `json:"lang"`
	Status string `json:"status"` // original | current | outdated | missing
	// BasedOn is the revision of the original the translation is based on.
	BasedOn int `json:"based_on,omitempty"`
}

// originalOf picks the original language of new texts.
func (s *Service) originalOf(titles map[string]string, want string) string {
	if want != "" && titles[want] != "" {
		return want
	}
	if d := s.DefaultLanguage(); titles[d] != "" {
		return d
	}
	keys := make([]string, 0, len(titles))
	for k, v := range titles {
		if v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		return keys[0]
	}
	return s.DefaultLanguage()
}

// catTranslations computes the translation state of c in all languages.
func (s *Service) catTranslations(c *Category) {
	orig := c.OriginalLang
	if orig == "" {
		orig = s.originalOf(c.Titles, "")
	}
	langs := append([]string{orig}, s.Languages()...)
	seen := map[string]bool{}
	done, n := 0, 0
	c.Translations = c.Translations[:0]
	for _, l := range langs {
		if seen[l] {
			continue
		}
		seen[l] = true
		t := CatTranslation{Lang: l}
		switch {
		case l == orig:
			t.Status = "original"
		case strings.TrimSpace(c.Titles[l]) == "":
			t.Status = "missing"
		case c.TranslationRevs[l] >= c.TextRev:
			t.Status, t.BasedOn = "current", c.TranslationRevs[l]
		default:
			t.Status, t.BasedOn = "outdated", c.TranslationRevs[l]
		}
		if l != orig {
			n++
			if t.Status == "current" {
				done++
			}
		}
		c.Translations = append(c.Translations, t)
	}
	c.TranslatedPercent = 100
	if n > 0 {
		c.TranslatedPercent = done * 100 / n
	}
	c.TranslationsUpToDate = done == n
}

// nextRevs computes text_rev and translation_revs after the texts of a
// category changed from (bt, bd) to (titles, descs).
func nextRevs(orig string, rev int, revs map[string]int, bt, bd, titles, descs map[string]string) (int, map[string]int) {
	out := map[string]int{}
	for k, v := range revs {
		out[k] = v
	}
	if bt[orig] != titles[orig] || bd[orig] != descs[orig] {
		rev++
	}
	for l, v := range titles {
		if l == orig {
			continue
		}
		if strings.TrimSpace(v) == "" {
			delete(out, l)
			continue
		}
		if bt[l] != v || bd[l] != descs[l] {
			out[l] = rev // changed together with or after the original: current
		}
	}
	for l := range out {
		if titles[l] == "" {
			delete(out, l)
		}
	}
	return rev, out
}

// CatTranslationInput translates the texts of a category into one language.
type CatTranslationInput struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	// Confirm marks the existing translation as current without changes
	// ("still correct" after the original changed).
	Confirm bool `json:"confirm"`
}

// TranslateCategory sets title and description of one language.
func (s *Service) TranslateCategory(ctx context.Context, id int64, lang string, in CatTranslationInput) (*Category, error) {
	if err := s.require(ctx, id, acl.Manage); err != nil {
		return nil, err
	}
	if !s.validLang(lang) {
		return nil, apperr.BadRequest("language %q is not configured", lang)
	}
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		c, err := s.loadCategoryForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		titles, descs := copyMap(c.Titles), copyMap(c.Descriptions)
		if in.Title != nil {
			titles[lang] = strings.TrimSpace(*in.Title)
		}
		if in.Description != nil {
			descs[lang] = strings.TrimSpace(*in.Description)
		}
		if lang == c.OriginalLang && titles[lang] == "" {
			return apperr.BadRequest("the original language needs a title")
		}
		if len([]rune(titles[lang])) > 200 || len([]rune(descs[lang])) > 2000 {
			return apperr.BadRequest("title or description too long")
		}
		if titles[lang] == "" {
			delete(titles, lang)
			delete(descs, lang)
		}
		if descs[lang] == "" {
			delete(descs, lang)
		}
		rev, revs := nextRevs(c.OriginalLang, c.TextRev, c.TranslationRevs, c.Titles, c.Descriptions, titles, descs)
		if in.Confirm && lang != c.OriginalLang && titles[lang] != "" {
			revs[lang] = rev
		}
		if _, err := tx.Exec(ctx, `UPDATE categories SET titles=$2, descriptions=$3, text_rev=$4, translation_revs=$5, updated_at=now() WHERE id=$1`,
			id, titles, descs, rev, revs); err != nil {
			return err
		}
		action := "category.translate"
		if in.Confirm && in.Title == nil && in.Description == nil {
			action = "category.translation_confirm"
		}
		return audit.Log(ctx, tx, action, "category", id,
			map[string]any{"lang": lang, "title": c.Titles[lang], "description": c.Descriptions[lang]},
			map[string]any{"lang": lang, "title": titles[lang], "description": descs[lang], "based_on": revs[lang]})
	})
	if err != nil {
		return nil, err
	}
	s.fire(ctx, "update", "category", id)
	return s.GetCategory(ctx, id)
}

func (s *Service) loadCategoryForUpdate(ctx context.Context, tx pgx.Tx, id int64) (*Category, error) {
	row, err := tx.Query(ctx, `SELECT `+catCols+` FROM categories c WHERE c.id=$1 AND c.deleted_at IS NULL FOR UPDATE`, id)
	if err != nil {
		return nil, err
	}
	c, err := pgx.CollectExactlyOneRow(row, scanCategory)
	if err != nil {
		return nil, apperr.NotFound("category")
	}
	if c.OriginalLang == "" {
		c.OriginalLang = s.originalOf(c.Titles, "")
	}
	return &c, nil
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
