package content

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
)

// CategoryDisplay is how a category page shows its pages (the block
// core.category.overview). It is inherited as a whole by the subcategories
// until one chooses its own.
type CategoryDisplay struct {
	// View: list (title, excerpt, date), cards (with preview picture), tiles
	// (large pictures), tree (subcategories to open) or handbook (a
	// navigation in the side column, also on the pages of the category)
	View string `json:"view"`
	// Sort: manual (the order of the category), title, updated (newest
	// first) or created (newest first)
	Sort string `json:"sort"`
	// PageSize: pages per page of the overview (0 = the expert setting
	// core.categories.page_size)
	PageSize int `json:"page_size"`
	// Subcategories: also the pages of the subcategories (not hidden or
	// archived ones)
	Subcategories bool `json:"subcategories"`
	// Excerpt: the beginning of the text of each page
	Excerpt bool `json:"excerpt"`
	// Columns: further columns of the table (keys a plugin offers, e.g.
	// properties; shown only where the reader may see them)
	Columns []string `json:"columns,omitempty"`
	// Date: the date of the timeline, updated or created
	Date string `json:"date,omitempty"`
}

// DisplayViews are the views of the overview of a category: list, cards,
// tiles, tree, handbook, table (sortable, with columns of plugins),
// accordion and tabs (the pages to open), az (index by the first letter),
// timeline (by date) and all (the pages in full one below the other).
var DisplayViews = []string{"list", "cards", "tiles", "tree", "handbook", "table", "accordion", "tabs", "az", "timeline", "all"}

// FullViews show the whole content of their pages: at most
// core.categories.full per page.
var FullViews = []string{"accordion", "tabs", "all"}

// DisplayDates are the dates of the timeline.
var DisplayDates = []string{"updated", "created"}

// columnRe is a key of a column (as the keys of properties).
var columnRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

// DisplaySorts are the orders of the overview of a category.
var DisplaySorts = []string{"manual", "title", "updated", "created"}

// DefaultDisplay is the display when no category above chose one: the list
// as before.
func DefaultDisplay() CategoryDisplay {
	return CategoryDisplay{View: "list", Sort: "manual", Excerpt: true, Date: "updated"}
}

func cleanDisplay(d CategoryDisplay) (CategoryDisplay, error) {
	if d.View == "" {
		d.View = "list"
	}
	if d.Sort == "" {
		d.Sort = "manual"
	}
	if !slices.Contains(DisplayViews, d.View) {
		return d, apperr.BadRequest("display.view: one of %v", DisplayViews)
	}
	if !slices.Contains(DisplaySorts, d.Sort) {
		return d, apperr.BadRequest("display.sort: one of %v", DisplaySorts)
	}
	if d.PageSize < 0 || d.PageSize > 1000 {
		return d, apperr.BadRequest("display.page_size: between 0 (as the expert setting) and 1000")
	}
	if d.Date == "" {
		d.Date = "updated"
	}
	if !slices.Contains(DisplayDates, d.Date) {
		return d, apperr.BadRequest("display.date: one of %v", DisplayDates)
	}
	if len(d.Columns) > 12 {
		return d, apperr.BadRequest("display.columns: at most 12")
	}
	seen := map[string]bool{}
	cols := []string{}
	for _, c := range d.Columns {
		if !columnRe.MatchString(c) {
			return d, apperr.BadRequest("display.columns: %q is no key", c)
		}
		if !seen[c] {
			seen[c] = true
			cols = append(cols, c)
		}
	}
	d.Columns = cols
	return d, nil
}

// OverviewItem is a page in the overview of a category.
type OverviewItem struct {
	ID           int64             `json:"id"`
	CategoryID   int64             `json:"category_id"`
	Type         string            `json:"type"`
	Titles       map[string]string `json:"titles"`
	OriginalLang string            `json:"original_lang"`
	PreviewImage string            `json:"preview_image"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	Hidden       bool              `json:"hidden,omitempty"`
	Excerpt      string            `json:"excerpt,omitempty"`
}

// Overview is one page of the overview of a category.
type Overview struct {
	// CategoryTitles: the title of the category (a navigation shows it)
	CategoryTitles map[string]string `json:"category_titles"`
	Display        CategoryDisplay   `json:"display"`
	Items          []OverviewItem    `json:"items"`
	Total          int               `json:"total"`
	Page           int               `json:"page"`
	Pages          int               `json:"pages"`
	// PageSize: the pages per page that applied
	PageSize int `json:"page_size"`
}

// excerptRunes is the length of an excerpt.
const excerptRunes = 240

// CategoryOverview returns one page (from 1) of the overview of a category
// with its effective display: the pages the caller may view, in the order of
// the display. lang chooses the language of the excerpts (else the original
// language). The display of the category applies unless override names
// another view (the arrangement of one block, e.g. a tree in the side
// column).
func (s *Service) CategoryOverview(ctx context.Context, id int64, lang string, page int, override string) (*Overview, error) {
	if err := s.require(ctx, id, acl.View); err != nil {
		return nil, err
	}
	st, err := s.effectiveSettings(ctx, s.pool, id)
	if err != nil {
		return nil, err
	}
	cat, err := s.loadCategory(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	d := *st.Effective.Display
	if override != "" {
		if !slices.Contains(DisplayViews, override) {
			return nil, apperr.BadRequest("view: one of %v", DisplayViews)
		}
		d.View = override
	}
	size := int(limits.Value(s.cfg.Get(), limits.CategoryPageSize))
	if d.PageSize > 0 && d.PageSize < size {
		size = d.PageSize
	}
	// whole pages: fewer at once
	if slices.Contains(FullViews, d.View) {
		size = min(size, int(limits.Value(s.cfg.Get(), limits.CategoryFull)))
	}
	// the index goes by title, the timeline by its date (newest first)
	switch d.View {
	case "az":
		d.Sort = "title"
	case "timeline":
		d.Sort = d.Date
		if d.Sort == "" {
			d.Sort = "updated"
		}
	}
	page = max(page, 1)
	p := identity.From(ctx)
	// hidden pages: only for those who may edit them here (marked)
	editors := s.acl.Can(ctx, p, id, acl.Edit)
	f, err := s.acl.ElementFilter(ctx, p, acl.View)
	if err != nil {
		return nil, err
	}
	args := []any{id, editors, d.Subcategories && d.View != "tree"}
	// the category itself, with subcategories those below that are neither
	// hidden nor archived (on every level up to the category); the filter
	// also checks the permissions of every subcategory
	where := `e.deleted_at IS NULL AND (e.category_id = $1 AND ($2 OR NOT e.hidden)
		OR $3 AND NOT e.hidden AND e.category_id IN (
			SELECT c.id FROM categories c, categories top WHERE top.id = $1 AND c.path <@ top.path AND c.id <> top.id
			AND NOT EXISTS (SELECT 1 FROM categories a WHERE a.path @> c.path AND a.path <@ top.path AND a.id <> top.id
				AND (a.deleted_at IS NOT NULL OR a.hidden OR a.archived_at IS NOT NULL))))
		AND ` + f.SQL("e", &args)
	var total int
	// acl: filtered (ElementFilter)
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM elements e WHERE `+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	order := "e.position, e.id"
	switch d.Sort {
	case "title":
		args = append(args, lang)
		order = "lower(coalesce(nullif(e.titles->>$" + strconv.Itoa(len(args)) + ", ''), e.titles->>e.original_lang, '')), e.id"
	case "updated":
		order = "e.updated_at DESC, e.id DESC"
	case "created":
		order = "e.created_at DESC, e.id DESC"
	}
	args = append(args, size, (page-1)*size)
	n := len(args)
	// acl: filtered (ElementFilter)
	rows, err := s.pool.Query(ctx, `SELECT e.id, e.category_id, e.type, e.titles, e.original_lang, e.preview_image, e.created_at, e.updated_at, e.hidden
		FROM elements e WHERE `+where+` ORDER BY `+order+` LIMIT $`+strconv.Itoa(n-1)+` OFFSET $`+strconv.Itoa(n), args...)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (OverviewItem, error) {
		var x OverviewItem
		err := r.Scan(&x.ID, &x.CategoryID, &x.Type, &x.Titles, &x.OriginalLang, &x.PreviewImage, &x.CreatedAt, &x.UpdatedAt, &x.Hidden)
		if x.Titles == nil {
			x.Titles = map[string]string{}
		}
		return x, err
	})
	if err != nil {
		return nil, err
	}
	if d.Excerpt && len(items) > 0 {
		if err := s.addExcerpts(ctx, items, lang); err != nil {
			return nil, err
		}
	}
	if items == nil {
		items = []OverviewItem{}
	}
	pages := max((total+size-1)/size, 1)
	return &Overview{CategoryTitles: cat.Titles, Display: d, Items: items, Total: total, Page: page, Pages: pages, PageSize: size}, nil
}

// addExcerpts fills the excerpts from the text index (the language asked
// for, else the original language), all at once.
func (s *Service) addExcerpts(ctx context.Context, items []OverviewItem, lang string) error {
	ids := make([]int64, len(items))
	for i, x := range items {
		ids[i] = x.ID
	}
	// acl: the ids are pages the caller may view (CategoryOverview)
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (d.element_id) d.element_id, left(d.body_text, $3)
		FROM search_docs d JOIN elements e ON e.id = d.element_id
		WHERE d.element_id = ANY($1) AND (d.lang = $2 OR d.lang = e.original_lang)
		ORDER BY d.element_id, d.lang = $2 DESC`, ids, lang, excerptRunes+1)
	if err != nil {
		return err
	}
	text := map[int64]string{}
	for rows.Next() {
		var id int64
		var t string
		if err := rows.Scan(&id, &t); err != nil {
			rows.Close()
			return err
		}
		text[id] = clipText(t, excerptRunes)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range items {
		items[i].Excerpt = text[items[i].ID]
	}
	return nil
}

// clipText shortens a text to n runes at a word boundary, with "…".
func clipText(t string, n int) string {
	r := []rune(t)
	if len(r) <= n {
		return t
	}
	cut := n
	for i := n; i > n*2/3; i-- {
		if r[i] == ' ' {
			cut = i
			break
		}
	}
	return string(r[:cut]) + "…"
}
