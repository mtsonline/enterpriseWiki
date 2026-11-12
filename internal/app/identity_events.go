package app

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/webhook"
)

// IdentityBatch is how many account changes one run turns into events.
const IdentityBatch = 500

// identityEvents turns the account changes (identity_changes, written by
// triggers for every way of creating, disabling, deleting or grouping
// people) into the events user.* and group.* of this site: webhooks and
// workflow triggers. A site starts with the changes after its creation.
func (a *App) identityEvents(ctx context.Context) error {
	for {
		n, err := a.identityBatch(ctx)
		if err != nil || n < IdentityBatch {
			return err
		}
	}
}

func (a *App) identityBatch(ctx context.Context) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		var cur int64
		err := tx.QueryRow(ctx, `SELECT seq FROM identity_cursor WHERE id=1 FOR UPDATE`).Scan(&cur)
		if errors.Is(err, pgx.ErrNoRows) { // first run: only what happens from now on
			_, err = tx.Exec(ctx, `INSERT INTO identity_cursor(id, seq) SELECT 1, COALESCE(max(seq), 0) FROM identity_changes ON CONFLICT DO NOTHING`)
			return err
		}
		if err != nil {
			return err
		}
		// details of people and groups as they are now (a deleted person
		// keeps the name stored with the change)
		rows, err := tx.Query(ctx, `SELECT c.seq, c.event, c.user_id, COALESCE(u.username, c.username), COALESCE(u.display_name, c.display_name),
				COALESCE(u.email, ''), c.group_id, COALESCE(g.name, ''), c.at
			FROM identity_changes c LEFT JOIN users u ON u.id = c.user_id AND c.event <> 'user.deleted' LEFT JOIN groups g ON g.id = c.group_id
			WHERE c.seq > $1 ORDER BY c.seq LIMIT $2`, cur, IdentityBatch)
		if err != nil {
			return err
		}
		type change struct {
			seq, user     int64
			event         string
			name, display string
			email         string
			group         *int64
			groupName     string
			at            time.Time
		}
		var list []change
		for rows.Next() {
			var c change
			if err := rows.Scan(&c.seq, &c.event, &c.user, &c.name, &c.display, &c.email, &c.group, &c.groupName, &c.at); err != nil {
				rows.Close()
				return err
			}
			list = append(list, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, c := range list {
			cur = c.seq
			if c.name == "" { // the person is gone already; user.deleted follows
				continue
			}
			data := map[string]any{"user_id": c.user, "username": c.name, "display_name": c.display}
			if c.email != "" {
				data["email"] = c.email
			}
			if c.group != nil {
				data["group_id"], data["group_name"] = *c.group, c.groupName
			}
			title := c.display
			if title == "" {
				title = c.name
			}
			a.Webhooks.EmitTx(ctx, tx, webhook.Event{Name: c.event, OccurredAt: c.at.UTC(), Actor: "system", Data: data,
				Object: webhook.Object{Type: "user", ID: c.user, Title: title}})
		}
		n = len(list)
		if n == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `UPDATE identity_cursor SET seq=$1 WHERE id=1`, cur)
		return err
	})
	return n, err
}
