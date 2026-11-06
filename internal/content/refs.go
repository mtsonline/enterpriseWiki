package content

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/db"
)

// refTables reference elements, categories and comments by type and id
// (no foreign keys, partly tables of plugins that may be missing).
var refTables = []struct{ table, typ, id string }{
	{"path_slugs", "target_type", "target_id"},
	{"url_aliases", "target_type", "target_id"},
	{"watches", "target_type", "target_id"},
	{"watch_events", "entity_type", "entity_id"},
	{"watch_assignments", "target_type", "target_id"},
	{"notifications", "entity_type", "entity_id"},
	{"plugin_mentions", "entity_type", "entity_id"},
	{"plugin_favorites", "target_type", "target_id"},
	{"plugin_reactions", "target_type", "target_id"},
}

// RemoveRefs deletes what references the given elements and categories by
// type and id: addresses, watches, notifications, mentions, favorites and
// reactions (also those of the elements' comments). Call it in the
// transaction that deletes the objects, before deleting them.
func RemoveRefs(ctx context.Context, tx db.Querier, elements, categories []int64) error {
	if len(elements) == 0 && len(categories) == 0 {
		return nil
	}
	exists := func(t string) (bool, error) {
		var ok bool
		err := tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, t).Scan(&ok)
		return ok, err
	}
	var comments []int64
	if len(elements) > 0 {
		ok, err := exists("plugin_comments")
		if err != nil {
			return err
		}
		if ok {
			rows, err := tx.Query(ctx, `SELECT id FROM plugin_comments WHERE element_id = ANY($1)`, elements)
			if err != nil {
				return err
			}
			if comments, err = pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
				return err
			}
		}
	}
	for _, r := range refTables {
		ok, err := exists(r.table)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		for _, k := range []struct {
			kind string
			ids  []int64
		}{{"element", elements}, {"category", categories}, {"comment", comments}} {
			if len(k.ids) == 0 {
				continue
			}
			if _, err := tx.Exec(ctx, `DELETE FROM `+pgx.Identifier{r.table}.Sanitize()+` WHERE `+r.typ+` = $1 AND `+r.id+` = ANY($2)`, k.kind, k.ids); err != nil {
				return fmt.Errorf("references in %s: %w", r.table, err)
			}
		}
	}
	return nil
}
