package content

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Page views are counted per element and day only (no people, no
// addresses): counted in memory and written once a minute.

type viewCounter struct {
	mu     sync.Mutex
	counts map[int64]int
}

const maxPendingViews = 50000

// CountView counts a view of an element the caller may see.
func (s *Service) CountView(ctx context.Context, id int64) error {
	if _, err := s.ElementCategory(ctx, id, acl.View); err != nil {
		return err
	}
	s.views.mu.Lock()
	defer s.views.mu.Unlock()
	if s.views.counts == nil {
		s.views.counts = map[int64]int{}
	}
	if len(s.views.counts) < maxPendingViews {
		s.views.counts[id]++
	}
	return nil
}

// FlushViews writes the counted views (worker); views older than 400 days are dropped.
func (s *Service) FlushViews(ctx context.Context) error {
	s.views.mu.Lock()
	counts := s.views.counts
	s.views.counts = nil
	s.views.mu.Unlock()
	if len(counts) > 0 {
		ids := make([]int64, 0, len(counts))
		ns := make([]int32, 0, len(counts))
		for id, n := range counts {
			ids, ns = append(ids, id), append(ns, int32(min(n, 1<<30))) // #nosec G115 -- capped
		}
		if _, err := s.pool.Exec(ctx, `INSERT INTO element_views(element_id, day, views)
			SELECT v.id, current_date, v.n FROM unnest($1::bigint[], $2::int[]) AS v(id, n) JOIN elements e ON e.id = v.id
			ON CONFLICT (element_id, day) DO UPDATE SET views = element_views.views + EXCLUDED.views`, ids, ns); err != nil {
			return err
		}
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM element_views WHERE day < current_date - 400`)
	return err
}

// InsightRow is a page in an evaluation.
type InsightRow struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Views     int       `json:"views,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Contributor is a person with the number of saved versions.
type Contributor struct {
	Name     string `json:"name"`
	Versions int    `json:"versions"`
}

// Insights is the evaluation of a category with everything below it.
type Insights struct {
	Days         int           `json:"days"`
	Pages        int           `json:"pages"`
	NewPages     int           `json:"new_pages"`
	Views        int           `json:"views"`
	ViewsPerDay  []DayViews    `json:"views_per_day"`
	Popular      []InsightRow  `json:"popular"`
	Stale        []InsightRow  `json:"stale"`
	Contributors []Contributor `json:"contributors"`
}

// DayViews are the views of one day.
type DayViews struct {
	Day   string `json:"day"`
	Views int    `json:"views"`
}

// Insights evaluates a category and the categories below it the caller may
// view (Manage on the category): views, popular and outdated pages,
// contributors.
func (s *Service) Insights(ctx context.Context, id int64, days int, lang string) (*Insights, error) {
	if err := s.require(ctx, id, acl.Manage); err != nil {
		return nil, err
	}
	if days <= 0 || days > 365 {
		days = 30
	}
	if lang == "" || !s.validLang(lang) {
		lang = s.DefaultLanguage()
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id FROM categories c WHERE c.path <@ (SELECT path FROM categories WHERE id=$1) AND c.deleted_at IS NULL`, id)
	if err != nil {
		return nil, err
	}
	all, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	cats, err := s.acl.Filter(ctx, identity.From(ctx), all, acl.View)
	if err != nil {
		return nil, err
	}
	out := &Insights{Days: days, ViewsPerDay: []DayViews{}, Popular: []InsightRow{}, Stale: []InsightRow{}, Contributors: []Contributor{}}
	if err := s.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE created_at > now() - make_interval(days => $2))
		FROM elements WHERE category_id = ANY($1) AND deleted_at IS NULL`, cats, days).Scan(&out.Pages, &out.NewPages); err != nil {
		return nil, err
	}
	title := `COALESCE(e.titles->>$3, e.titles->>e.original_lang, '')`
	rows, err = s.pool.Query(ctx, `SELECT to_char(v.day, 'YYYY-MM-DD'), sum(v.views)::int FROM element_views v JOIN elements e ON e.id = v.element_id
		WHERE e.category_id = ANY($1) AND e.deleted_at IS NULL AND v.day > current_date - $2::int GROUP BY v.day ORDER BY v.day`, cats, days)
	if err != nil {
		return nil, err
	}
	if out.ViewsPerDay, err = pgx.CollectRows(rows, pgx.RowToStructByPos[DayViews]); err != nil {
		return nil, err
	}
	for _, d := range out.ViewsPerDay {
		out.Views += d.Views
	}
	rows, err = s.pool.Query(ctx, `SELECT e.id, `+title+`, sum(v.views)::int, e.updated_at FROM element_views v JOIN elements e ON e.id = v.element_id
		WHERE e.category_id = ANY($1) AND e.deleted_at IS NULL AND v.day > current_date - $2::int
		GROUP BY e.id ORDER BY 3 DESC, e.id LIMIT 10`, cats, days, lang)
	if err != nil {
		return nil, err
	}
	if out.Popular, err = pgx.CollectRows(rows, pgx.RowToStructByPos[InsightRow]); err != nil {
		return nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT e.id, `+title+`, 0, e.updated_at FROM elements e
		WHERE e.category_id = ANY($1) AND e.deleted_at IS NULL AND e.updated_at < now() - interval '180 days' AND $2::int > 0
		ORDER BY e.updated_at, e.id LIMIT 10`, cats, days, lang)
	if err != nil {
		return nil, err
	}
	if out.Stale, err = pgx.CollectRows(rows, pgx.RowToStructByPos[InsightRow]); err != nil {
		return nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT COALESCE(NULLIF(u.display_name, ''), u.username), count(*)::int FROM element_revisions r
		JOIN elements e ON e.id = r.element_id JOIN users u ON u.id = r.created_by
		WHERE e.category_id = ANY($1) AND e.deleted_at IS NULL AND r.created_at > now() - make_interval(days => $2)
		GROUP BY u.id, 1 ORDER BY 2 DESC, 1 LIMIT 10`, cats, days)
	if err != nil {
		return nil, err
	}
	if out.Contributors, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Contributor]); err != nil {
		return nil, err
	}
	return out, nil
}
