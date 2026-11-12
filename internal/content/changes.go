package content

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Durable change hooks: what follows from a change and must never be lost
// (notifications of watchers, of mentioned people ...).
//
// The change is written to the queue change_events - for pages and
// categories that are created or saved in the same transaction as the
// content, otherwise before the change is confirmed to the caller. The
// hooks run right after the commit; the entry is removed when all of them
// succeeded. What is left (an error, a crash between commit and hook) is
// repeated by the worker (RetryChanges) until it succeeds. A hook can
// therefore run more than once for a change and must cope with that.

// DurableHook is a change hook whose work must not be lost; an error
// means "repeat later".
type DurableHook func(ctx context.Context, event, entityType string, id int64) error

type durableHook struct {
	name string
	h    DurableHook
}

// OnChangeDurable registers a durable hook under a unique, stable name
// (it is stored with the waiting changes).
func (s *Service) OnChangeDurable(name string, h DurableHook) {
	s.durable = append(s.durable, durableHook{name, h})
}

// SetActorContext wires how a repeated change gets the context of the
// account that made it (nil account: nobody).
func (s *Service) SetActorContext(f func(ctx context.Context, uid int64) context.Context) {
	s.actorCtx = f
}

// note writes a change to the queue with q (the transaction of the
// change) and returns its entry; 0 without durable hooks.
func (s *Service) note(ctx context.Context, q db.Querier, event, etype string, id int64) (int64, error) {
	if len(s.durable) == 0 {
		return 0, nil
	}
	names := make([]string, len(s.durable))
	for i, d := range s.durable {
		names[i] = d.name
	}
	var actor *int64
	if uid := identity.From(ctx).ID(); uid > 0 {
		actor = &uid
	}
	var entry int64
	err := q.QueryRow(ctx, `INSERT INTO change_events(event, entity_type, entity_id, actor_id, silent, hooks) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		event, etype, id, actor, IsSilent(ctx), names).Scan(&entry)
	return entry, err
}

// deliver runs the durable hooks named in names for a change and settles
// its entry with q: removed when all succeeded, otherwise kept for the
// worker with the hooks that are still open.
func (s *Service) deliver(ctx context.Context, q db.Querier, entry int64, names []string, attempts int, event, etype string, id int64) {
	var open []string
	for _, d := range s.durable {
		wanted := names == nil
		for _, n := range names {
			wanted = wanted || n == d.name
		}
		if !wanted {
			continue
		}
		if err := d.h(ctx, event, etype, id); err != nil {
			open = append(open, d.name)
			s.log.Warn("change hook failed, it is repeated", "hook", d.name, "event", event, "type", etype, "id", id, "attempt", attempts+1, "err", err)
		}
	}
	if entry == 0 {
		if len(open) > 0 { // the queue itself could not be written: say it loudly
			s.log.Error("change hooks failed and the change is not queued", "hooks", open, "event", event, "type", etype, "id", id)
		}
		return
	}
	var err error
	if len(open) == 0 {
		_, err = q.Exec(ctx, `DELETE FROM change_events WHERE id=$1`, entry)
	} else {
		// 1, 2, 4 ... minutes, at most an hour
		wait := time.Minute << min(attempts, 6)
		_, err = q.Exec(ctx, `UPDATE change_events SET hooks=$2, attempts=attempts+1, next_at=now()+$3::interval WHERE id=$1`, entry, open, min(wait, time.Hour).String())
	}
	if err != nil { // the entry stays: the worker repeats it
		s.log.Warn("settling a queued change failed", "entry", entry, "err", err)
	}
}

// RetryChanges repeats the durable hooks of changes that are still in the
// queue (worker). It returns how many changes were settled.
func (s *Service) RetryChanges(ctx context.Context) (int, error) {
	type row struct {
		id, entity   int64
		event, etype string
		actor        *int64
		silent       bool
		hooks        []string
		attempts     int
	}
	n := 0
	for range 20 { // at most 2000 per run
		var list []row
		// one worker per entry (cluster): the rows stay locked while their hooks run
		err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT id, event, entity_type, entity_id, actor_id, silent, hooks, attempts FROM change_events
				WHERE next_at <= now() ORDER BY id LIMIT 100 FOR UPDATE SKIP LOCKED`)
			if err != nil {
				return err
			}
			list, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
				var x row
				return x, r.Scan(&x.id, &x.event, &x.etype, &x.entity, &x.actor, &x.silent, &x.hooks, &x.attempts)
			})
			if err != nil {
				return err
			}
			for _, x := range list {
				c := ctx
				if x.actor != nil && s.actorCtx != nil {
					c = s.actorCtx(c, *x.actor)
				}
				if x.silent {
					c = WithSilent(c)
				}
				s.deliver(c, tx, x.id, x.hooks, x.attempts, x.event, x.etype, x.entity) // settled with the lock held
			}
			return nil
		})
		if err != nil {
			return n, err
		}
		n += len(list)
		if len(list) < 100 {
			break
		}
	}
	return n, nil
}
