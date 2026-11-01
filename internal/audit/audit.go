// Package audit writes the immutable audit trail. Every mutation of users,
// permissions, categories, elements and settings is recorded with a before
// and after snapshot, which is also what category history and rollback use.
package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Entry is one audit record.
type Entry struct {
	ID         int64           `json:"id"`
	At         time.Time       `json:"at"`
	UserID     *int64          `json:"user_id"`
	Actor      string          `json:"actor"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   *int64          `json:"entity_id"`
	Before     json.RawMessage `json:"before,omitempty"`
	After      json.RawMessage `json:"after,omitempty"`
}

func toJSON(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return nil
	}
	return b
}

// Log appends an audit record using q (pass the transaction so the record is
// committed atomically with the change).
func Log(ctx context.Context, q db.Querier, action, entityType string, entityID int64, before, after any) error {
	p := identity.From(ctx)
	var uid *int64
	if p.ID() != 0 {
		id := p.ID()
		uid = &id
	}
	var eid *int64
	if entityID != 0 {
		eid = &entityID
	}
	_, err := q.Exec(ctx, `INSERT INTO audit_log(user_id, actor, action, entity_type, entity_id, before, after)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, uid, p.Handle(), action, entityType, eid, toJSON(before), toJSON(after))
	return err
}

// Query lists audit entries, newest first.
func Query(ctx context.Context, q db.Querier, entityType string, entityID int64, limit, offset int) ([]Entry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := q.Query(ctx, `SELECT id, at, user_id, actor, action, entity_type, entity_id, before, after FROM audit_log
		WHERE ($1 = '' OR entity_type = $1) AND ($2 = 0 OR entity_id = $2)
		ORDER BY at DESC, id DESC LIMIT $3 OFFSET $4`, entityType, entityID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.At, &e.UserID, &e.Actor, &e.Action, &e.EntityType, &e.EntityID, &e.Before, &e.After); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
