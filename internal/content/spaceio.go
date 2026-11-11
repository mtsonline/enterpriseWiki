package content

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
)

// Export and import of a category tree (e.g. a space) as JSON Lines: one
// header line, then categories (parents first), elements with the latest
// revision of every language, and the page templates of a space.
// Permissions and files are not part of the package (they depend on the
// installation); bodies keep their links.

// ExportFormat identifies the package format.
const ExportFormat = "ewiki-space"

// MaxImportBytes limits an import package.
const MaxImportBytes = 50 << 20

// MaxImportLine limits one line (an element with all languages).
const MaxImportLine = 8 << 20

type ioHeader struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exported_at"`
}

type ioRevision struct {
	Lang  string          `json:"lang"`
	Title string          `json:"title"`
	Body  json.RawMessage `json:"body"`
}

type ioLine struct {
	Kind string `json:"kind"` // category | element | template
	// category
	Ref          int64             `json:"ref,omitempty"`
	Parent       int64             `json:"parent,omitempty"`
	Titles       map[string]string `json:"titles,omitempty"`
	Descriptions map[string]string `json:"descriptions,omitempty"`
	Tags         []string          `json:"tags,omitempty"`
	Template     string            `json:"template,omitempty"`
	Key          string            `json:"key,omitempty"`
	Space        *Space            `json:"space,omitempty"`
	OriginalLang string            `json:"original_lang,omitempty"`
	// element
	Category  int64        `json:"category,omitempty"`
	Type      string       `json:"type,omitempty"`
	Revisions []ioRevision `json:"revisions,omitempty"`
	// template
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Lang        string          `json:"lang,omitempty"`
	Title       string          `json:"title,omitempty"`
	Body        json.RawMessage `json:"body,omitempty"`
}

// ExportTree writes a category with everything below it (Manage on it; only
// what the caller may view).
func (s *Service) ExportTree(ctx context.Context, id int64, w io.Writer) error {
	if err := s.require(ctx, id, acl.Manage); err != nil {
		return err
	}
	src, err := s.loadCategory(ctx, s.pool, id, false)
	if err != nil {
		return err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+catCols+` FROM categories c WHERE c.path <@ $1::ltree AND c.deleted_at IS NULL ORDER BY nlevel(c.path), c.position, c.id`, pathString(src.Path))
	if err != nil {
		return err
	}
	tree, err := pgx.CollectRows(rows, scanCategory)
	if err != nil {
		return err
	}
	if tree, err = s.decorate(ctx, tree); err != nil { // only visible ones
		return err
	}
	enc := json.NewEncoder(w)
	if err := enc.Encode(ioHeader{Format: ExportFormat, Version: 1, ExportedAt: time.Now().UTC()}); err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, c := range tree {
		if c.ID != src.ID && (c.ParentID == nil || !seen[*c.ParentID]) {
			continue // parent not visible
		}
		seen[c.ID] = true
		l := ioLine{Kind: "category", Ref: c.ID, Titles: c.Titles, Descriptions: c.Descriptions, Tags: c.Tags, Template: c.Template, OriginalLang: c.OriginalLang}
		if c.ID != src.ID {
			l.Parent = *c.ParentID
		} else if c.ParentID == nil {
			if sp, err := s.loadSpace(ctx, c.ID); err == nil {
				l.Key, l.Space = sp.Key, &sp.Space
			}
		}
		if err := enc.Encode(l); err != nil {
			return err
		}
		if err := s.exportElements(ctx, c.ID, enc); err != nil {
			return err
		}
	}
	if src.ParentID == nil {
		trows, err := s.pool.Query(ctx, `SELECT name, description, type, lang, title, body FROM element_templates WHERE space_id=$1 ORDER BY id`, src.ID)
		if err != nil {
			return err
		}
		defer trows.Close()
		for trows.Next() {
			l := ioLine{Kind: "template"}
			if err := trows.Scan(&l.Name, &l.Description, &l.Type, &l.Lang, &l.Title, &l.Body); err != nil {
				return err
			}
			if err := enc.Encode(l); err != nil {
				return err
			}
		}
		if err := trows.Err(); err != nil {
			return err
		}
	}
	_ = audit.Log(ctx, s.pool, "category.export", "category", id, nil, map[string]any{"categories": len(seen)})
	return nil
}

func (s *Service) exportElements(ctx context.Context, cat int64, enc *json.Encoder) error {
	rows, err := s.pool.Query(ctx, `SELECT e.id, e.type, e.original_lang, e.tags FROM elements e WHERE e.category_id=$1 AND e.deleted_at IS NULL ORDER BY e.position, e.id`, cat)
	if err != nil {
		return err
	}
	type el struct {
		id         int64
		typ, olang string
		tags       []string
	}
	var list []el
	for rows.Next() {
		var x el
		if err := rows.Scan(&x.id, &x.typ, &x.olang, &x.tags); err != nil {
			rows.Close()
			return err
		}
		list = append(list, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, x := range list {
		rrows, err := s.pool.Query(ctx, `SELECT r.lang, r.title, r.body FROM element_revisions r
			WHERE r.element_id=$1 AND r.rev = (SELECT max(rev) FROM element_revisions y WHERE y.element_id=$1 AND y.lang=r.lang)
			ORDER BY (r.lang = $2) DESC, r.lang`, x.id, x.olang)
		if err != nil {
			return err
		}
		revs, err := pgx.CollectRows(rrows, func(r pgx.CollectableRow) (ioRevision, error) {
			var v ioRevision
			return v, r.Scan(&v.Lang, &v.Title, &v.Body)
		})
		if err != nil {
			return err
		}
		if err := enc.Encode(ioLine{Kind: "element", Category: cat, Type: x.typ, OriginalLang: x.olang, Tags: x.tags, Revisions: revs}); err != nil {
			return err
		}
	}
	return nil
}

// ImportResult reports an import.
type ImportResult struct {
	CategoryID int64    `json:"category_id"`
	Categories int      `json:"categories"`
	Elements   int      `json:"elements"`
	Templates  int      `json:"templates"`
	Skipped    []string `json:"skipped"`
}

// ImportTree reads a package below parent (0 = new space; needs the rights
// to create there). Everything goes through the normal checks: element
// types and bodies are validated and cleaned like when saved in the editor.
func (s *Service) ImportTree(ctx context.Context, parent int64, r io.Reader) (*ImportResult, error) {
	sc := bufio.NewScanner(io.LimitReader(r, MaxImportBytes))
	sc.Buffer(make([]byte, 64<<10), MaxImportLine)
	if !sc.Scan() {
		return nil, apperr.BadRequest("empty package")
	}
	var head ioHeader
	if err := json.Unmarshal(sc.Bytes(), &head); err != nil || head.Format != ExportFormat || head.Version != 1 {
		return nil, apperr.BadRequest("not an export package of this wiki (format %s, version 1)", ExportFormat)
	}
	res := &ImportResult{Skipped: []string{}}
	skip := func(format string, a ...any) {
		if len(res.Skipped) < 200 {
			res.Skipped = append(res.Skipped, fmt.Sprintf(format, a...))
		}
	}
	mapping := map[int64]int64{}
	line := 1
	for sc.Scan() {
		line++
		var l ioLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			skip("line %d: invalid JSON", line)
			continue
		}
		switch l.Kind {
		case "category":
			var p *int64
			if len(mapping) == 0 {
				if parent != 0 {
					p = &parent
				}
			} else if np, ok := mapping[l.Parent]; ok {
				p = &np
			} else {
				skip("line %d: category without parent", line)
				continue
			}
			in := CategoryInput{ParentID: p, Titles: l.Titles, Descriptions: l.Descriptions, Tags: l.Tags}
			if l.Template != "" && s.templateOK != nil && s.templateOK(l.Template) {
				in.Template = &l.Template
			}
			if l.OriginalLang != "" && s.validLang(l.OriginalLang) {
				in.OriginalLang = &l.OriginalLang
			}
			c, err := s.CreateCategory(ctx, in)
			if err != nil {
				if len(mapping) == 0 {
					return nil, err // no place to import to
				}
				skip("line %d: category %q: %v", line, anyText(l.Titles), err)
				continue
			}
			if len(mapping) == 0 {
				res.CategoryID = c.ID
				if p == nil && l.Space != nil {
					key := l.Key
					var used bool
					_ = s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM categories WHERE space_key=$1 AND deleted_at IS NULL)`, key).Scan(&used)
					if used {
						skip("space key %s is already used – imported without key", key)
						key = ""
					}
					sp := *l.Space
					sp.IsTemplate = false
					if _, err := s.SetSpace(ctx, c.ID, SpaceInput{Key: &key, Space: sp}); err != nil {
						skip("space settings: %v", err)
					}
				}
			}
			mapping[l.Ref] = c.ID
			res.Categories++
		case "element":
			cat, ok := mapping[l.Category]
			if !ok || len(l.Revisions) == 0 {
				skip("line %d: element without category or content", line)
				continue
			}
			first := l.Revisions[0]
			v, err := s.CreateElement(ctx, ElementInput{CategoryID: cat, Type: l.Type, Lang: first.Lang, Title: first.Title, Body: first.Body, Tags: l.Tags, Comment: "Imported"})
			if err != nil {
				skip("line %d: %q: %v", line, first.Title, err)
				continue
			}
			for _, rv := range l.Revisions[1:] {
				zero := 0
				if _, err := s.SaveElement(ctx, v.ID, SaveInput{Lang: rv.Lang, Title: rv.Title, Body: rv.Body, Comment: "Imported", BaseRev: &zero}); err != nil {
					skip("line %d: %q (%s): %v", line, rv.Title, rv.Lang, err)
				}
			}
			res.Elements++
		case "template":
			if res.CategoryID == 0 || parent != 0 {
				continue // page templates belong to a new space
			}
			if _, err := s.SaveTemplate(ctx, 0, TemplateInput{Name: l.Name, Description: l.Description, Type: l.Type, Lang: l.Lang, Title: l.Title, Body: l.Body, SpaceID: res.CategoryID}); err != nil {
				skip("line %d: template %q: %v", line, l.Name, err)
				continue
			}
			res.Templates++
		default:
			skip("line %d: unknown kind %q", line, l.Kind)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, apperr.BadRequest("package too large or broken: %v", err)
	}
	if res.CategoryID == 0 {
		return nil, apperr.BadRequest("the package contains no category")
	}
	_ = audit.Log(ctx, s.pool, "category.import", "category", res.CategoryID, nil, map[string]any{"categories": res.Categories, "elements": res.Elements, "skipped": len(res.Skipped)})
	return res, nil
}

func anyText(m map[string]string) string {
	for _, v := range m {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
