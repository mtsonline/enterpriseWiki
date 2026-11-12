package workflow_process

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// WebhookAttempts is the number of tries before a webhook step fails.
const WebhookAttempts = 3

// runTimers continues waiting cases (delay steps, webhooks) and sends
// reminders for overdue tasks. Called by the background worker.
func (pl *Plugin) runTimers(ctx context.Context) error {
	for i := 0; i < 50; i++ {
		done, err := pl.wakeOne(ctx)
		if err != nil {
			return err
		}
		if done {
			break
		}
	}
	if err := pl.reminders(ctx); err != nil {
		return err
	}
	return pl.deadlines(ctx)
}

// wakeOne handles one due case; done is true if there was none.
func (pl *Plugin) wakeOne(ctx context.Context) (done bool, err error) {
	err = pgx.BeginFunc(ctx, pl.host.DB, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM plugin_workflow_instances WHERE status='open' AND wake_at <= now()
			ORDER BY wake_at LIMIT 1 FOR NO KEY UPDATE SKIP LOCKED`).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			done = true
			return nil
		}
		if err != nil {
			return err
		}
		x, err := pl.loadTx(ctx, tx, id)
		if err != nil {
			return err
		}
		s := x.Definition.Step(x.Step)
		if s == nil {
			_, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET wake_at=NULL, last_error='unknown step' WHERE id=$1`, id)
			return err
		}
		sys := actorInfo{}
		switch s.Type {
		case StepDelay:
			if err := event(ctx, tx, id, sp(s.ID), sp(s.Next), "timer", "", "", sys); err != nil {
				return err
			}
			return pl.enter(ctx, tx, x, s.Next, sys)
		case StepWebhook:
			var attempts int
			if err := tx.QueryRow(ctx, `SELECT attempts FROM plugin_workflow_instances WHERE id=$1`, id).Scan(&attempts); err != nil {
				return err
			}
			callErr := pl.callWebhook(ctx, x, s, pl.vars(ctx, tx, x, sys))
			if callErr == nil {
				if err := event(ctx, tx, id, sp(s.ID), sp(s.Next), "webhook", "", "", sys); err != nil {
					return err
				}
				return pl.enter(ctx, tx, x, s.Next, sys)
			}
			attempts++
			msg := callErr.Error()
			pl.host.Log.Warn("workflow webhook failed", "case", id, "attempt", attempts, "err", msg)
			if attempts < WebhookAttempts {
				_, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET attempts=$2, last_error=$3, wake_at=now() + make_interval(mins => $2 * 2) WHERE id=$1`,
					id, attempts, msg)
				return err
			}
			if s.NextElse != "" {
				if err := event(ctx, tx, id, sp(s.ID), sp(s.NextElse), "webhook_failed", "", msg, sys); err != nil {
					return err
				}
				return pl.enter(ctx, tx, x, s.NextElse, sys)
			}
			if err := event(ctx, tx, id, sp(s.ID), sp(s.ID), "webhook_failed", "", msg, sys); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET attempts=$2, last_error=$3, wake_at=NULL, updated_at=now() WHERE id=$1`, id, attempts, msg)
			return err
		case StepHTTP, StepCreate, StepProps, StepSubflow, StepParallel, StepAccess, StepMember, StepMove:
			var attempts int
			if err := tx.QueryRow(ctx, `SELECT attempts FROM plugin_workflow_instances WHERE id=$1`, id).Scan(&attempts); err != nil {
				return err
			}
			// a savepoint: a failed step leaves nothing behind in the case
			sp2, err := tx.Begin(ctx)
			if err != nil {
				return err
			}
			res, runErr := pl.runStep(ctx, sp2, x, s)
			if runErr == nil && res.wait {
				if err := sp2.Commit(ctx); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET wake_at=NULL, attempts=0, last_error='' WHERE id=$1`, id)
				return err
			}
			if runErr == nil {
				if err := event(ctx, sp2, id, sp(s.ID), sp(res.next), res.action, res.label, res.note, sys); err != nil {
					runErr = err
				} else if err := pl.enter(ctx, sp2, x, res.next, sys); err != nil {
					runErr = err
				}
			}
			if runErr == nil {
				return sp2.Commit(ctx)
			}
			_ = sp2.Rollback(ctx)
			x, err = pl.loadTx(ctx, tx, id) // as before the step
			if err != nil {
				return err
			}
			attempts++
			msg := runErr.Error()
			pl.host.Log.Warn("workflow step failed", "case", id, "step", s.ID, "attempt", attempts, "err", msg)
			if attempts < WebhookAttempts {
				_, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET attempts=$2, last_error=$3, wake_at=now() + make_interval(mins => $2 * 2) WHERE id=$1`,
					id, attempts, msg)
				return err
			}
			if s.NextElse != "" && s.Type == StepHTTP {
				if err := event(ctx, tx, id, sp(s.ID), sp(s.NextElse), s.Type+"_failed", "", msg, sys); err != nil {
					return err
				}
				return pl.enter(ctx, tx, x, s.NextElse, sys)
			}
			// waits for a decision of a manager (repeat or skip)
			if err := event(ctx, tx, id, sp(s.ID), sp(s.ID), s.Type+"_failed", "", msg, sys); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE plugin_workflow_instances SET attempts=$2, last_error=$3, wake_at=NULL, updated_at=now() WHERE id=$1`, id, attempts, msg)
			return err
		default:
			_, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET wake_at=NULL WHERE id=$1`, id)
			return err
		}
	})
	return done, err
}

// reminders notifies the assignees of overdue tasks and approvals once.
func (pl *Plugin) reminders(ctx context.Context) error {
	rows, err := pl.host.DB.Query(ctx, `SELECT id FROM plugin_workflow_instances
		WHERE status='open' AND reminded_at IS NULL AND step_entered_at < now() - interval '1 day' ORDER BY id LIMIT 500`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		err := pgx.BeginFunc(ctx, pl.host.DB, func(tx pgx.Tx) error {
			x, err := pl.loadTx(ctx, tx, id)
			if err != nil {
				return err
			}
			s := x.Definition.Step(x.Step)
			if x.Status != "open" || s == nil || !s.Manual() || s.DueDays <= 0 {
				_, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET reminded_at=now() WHERE id=$1 AND reminded_at IS NULL`, id)
				return err
			}
			if time.Since(x.StepEntered) < time.Duration(s.DueDays)*24*time.Hour {
				return nil
			}
			if err := pl.notifyAssignees(ctx, tx, x, s, true); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE plugin_workflow_instances SET reminded_at=now() WHERE id=$1`, id)
			return err
		})
		if err != nil {
			return fmt.Errorf("reminder for case %d: %w", id, err)
		}
	}
	return nil
}
