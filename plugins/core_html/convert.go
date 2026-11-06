package core_html

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

// ConvertComment is the version note of the one-time conversion.
const ConvertComment = "Converted to Markdown"

// ConvertAll converts pages still stored as HTML (old editor) to Markdown,
// once: the current version of every language becomes a new version (the
// history keeps the HTML), drafts and templates are converted in place.
// It returns the number of converted versions, drafts and templates.
func ConvertAll(ctx context.Context, h *sdk.Host) (int, error) {
	ctx = identity.With(ctx, identity.CLI())
	type cur struct {
		id    int64
		lang  string
		rev   int
		title string
		body  json.RawMessage
	}
	rows, err := h.DB.Query(ctx, `SELECT DISTINCT ON (r.element_id, r.lang) r.element_id, r.lang, r.rev, r.title, r.body
		FROM element_revisions r JOIN elements e ON e.id = r.element_id
		WHERE e.type = 'core.html' ORDER BY r.element_id, r.lang, r.rev DESC`)
	if err != nil {
		return 0, err
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (cur, error) {
		var c cur
		err := r.Scan(&c.id, &c.lang, &c.rev, &c.title, &c.body)
		return c, err
	})
	if err != nil {
		return 0, err
	}
	n := 0
	t := &Type{}
	for _, c := range all {
		b, err := Decode(c.body)
		if err != nil || b.Format != "html" {
			continue
		}
		body, err := t.Normalize(c.body)
		if err != nil {
			h.Log.Warn("markdown conversion failed", "element", c.id, "lang", c.lang, "err", err)
			continue
		}
		base := c.rev
		if _, err := h.Content.SaveElement(ctx, c.id, content.SaveInput{Lang: c.lang, Title: c.title, Body: body, Comment: ConvertComment, BaseRev: &base}); err != nil {
			h.Log.Warn("markdown conversion failed", "element", c.id, "lang", c.lang, "err", err)
			continue
		}
		n++
	}
	for _, table := range []string{"element_drafts", "element_templates"} {
		rows, err := h.DB.Query(ctx, `SELECT id, body FROM `+table+` WHERE type = 'core.html' AND body->>'format' IS DISTINCT FROM 'markdown'`)
		if err != nil {
			return n, err
		}
		type item struct {
			id   int64
			body json.RawMessage
		}
		items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (item, error) {
			var i item
			err := r.Scan(&i.id, &i.body)
			return i, err
		})
		if err != nil {
			return n, err
		}
		for _, it := range items {
			body, err := t.Normalize(it.body)
			if err != nil {
				continue
			}
			if _, err := h.DB.Exec(ctx, `UPDATE `+table+` SET body=$2 WHERE id=$1`, it.id, body); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

// convertInBackground runs ConvertAll shortly after the start.
func convertInBackground(h *sdk.Host) {
	go func() {
		time.Sleep(2 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if n, err := ConvertAll(ctx, h); err != nil {
			h.Log.Error("markdown conversion", "err", err)
		} else if n > 0 {
			h.Log.Info("pages converted to Markdown", "count", n)
		}
	}()
}
