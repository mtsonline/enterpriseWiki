package content_events

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/mail"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

func parseRange(r *http.Request, def int) (time.Time, time.Time, error) {
	q := r.URL.Query()
	from, to := time.Now().Add(-24*time.Hour), time.Time{}
	if v := q.Get("from"); v != "" {
		t, err := parseTime(v)
		if err != nil {
			return from, to, apperr.BadRequest("invalid from")
		}
		from = t
	}
	to = from.AddDate(0, 0, def)
	if v := q.Get("to"); v != "" {
		t, err := parseTime(v)
		if err != nil {
			return from, to, apperr.BadRequest("invalid to")
		}
		to = t
	}
	if !to.After(from) || to.Sub(from) > 400*24*time.Hour {
		return from, to, apperr.BadRequest("choose a range of at most 400 days")
	}
	return from, to, nil
}

func parseTime(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", v)
}

// Setup registers the element type, macro, worker and API.
func (p *Plugin) Setup(hs *sdk.Host) error {
	p.host = hs
	hs.RegisterElementType(Type{})
	hs.RegisterMacro(UpcomingMacro{})
	hs.RegisterWorker("reminders", time.Minute, p.sendReminders)

	// GET /calendars/{id}/events?from=&to=&category=&q=&lang= → events and their occurrences
	hs.Handle("GET", "/calendars/{id}/events", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		q := r.URL.Query()
		cal, err := p.loadCalendar(ctx, id, q.Get("lang"))
		if err != nil {
			return nil, err
		}
		from, to, err := parseRange(r, cal.Config.ListDays)
		if err != nil {
			return nil, err
		}
		events, err := p.eventsOf(ctx, []int64{id}, from, to, q.Get("category"), strings.TrimSpace(q.Get("q")))
		if err != nil {
			return nil, err
		}
		occ, err := p.occurrences(ctx, events, from, to)
		if err != nil {
			return nil, err
		}
		used := map[int64]bool{}
		for _, o := range occ {
			used[o.EventID] = true
		}
		list := []*Event{}
		for _, e := range events {
			if used[e.ID] {
				cal.decorate(ctx, e)
				list = append(list, e)
			}
		}
		return map[string]any{"calendar": map[string]any{"id": cal.ID, "title": cal.Title, "config": cal.Config, "can_create": cal.canCreate(),
			"signed_in": identity.From(ctx).ID() != 0}, "events": list, "occurrences": occ, "from": from, "to": to}, nil
	})

	// POST /calendars/{id}/events creates an event (Create permission).
	hs.Handle("POST", "/calendars/{id}/events", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		cal, err := p.loadCalendar(ctx, id, "")
		if err != nil {
			return nil, err
		}
		if !cal.canCreate() {
			return nil, apperr.ErrForbidden
		}
		var in Input
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := in.check(cal.Config); err != nil {
			return nil, err
		}
		uid := identity.From(ctx).ID()
		host := ""
		if u, err := url.Parse(hs.Config.Get().Server.BaseURL); err == nil {
			host = u.Hostname()
		}
		var eid int64
		err = pgx.BeginFunc(ctx, hs.DB, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `INSERT INTO plugin_events(calendar_id, uid, title, description, location, url, category, starts_at, ends_at, all_day, tz,
				rrule, exdates, registration, capacity, waitlist, reg_deadline, reminders, created_by, updated_by)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::date[],$14,$15,$16,$17,$18,$19,$19) RETURNING id`,
				id, newUID(host), in.Title, in.Description, in.Location, in.URL, in.Category, in.StartsAt, in.EndsAt, in.AllDay, in.TZ,
				in.RRule, in.ExDates, in.Registration, in.Capacity, *in.Waitlist, in.RegDeadline, in.Reminders, nullID(uid)).Scan(&eid); err != nil {
				return err
			}
			return audit.Log(ctx, tx, "event.create", "event", eid, nil, in)
		})
		if err != nil {
			return nil, err
		}
		p.changed(ctx, id)
		e, _, err := p.event(ctx, eid, "")
		if err != nil {
			return nil, err
		}
		return h.Created{V: e}, nil
	})

	// GET /events/{id} → the event with its next occurrences
	hs.Handle("GET", "/events/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		e, _, err := p.event(ctx, id, r.URL.Query().Get("lang"))
		if err != nil {
			return nil, err
		}
		from := time.Now().Add(-24 * time.Hour)
		occ, err := p.occurrences(ctx, []*Event{e}, from, from.AddDate(1, 0, 0))
		if err != nil {
			return nil, err
		}
		if len(occ) > 50 {
			occ = occ[:50]
		}
		return map[string]any{"event": e, "occurrences": occ}, nil
	})

	// PUT /events/{id} changes an event (creator with Create, or Edit).
	hs.Handle("PUT", "/events/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		before, cal, err := p.event(ctx, id, "")
		if err != nil {
			return nil, err
		}
		if !before.CanEdit {
			return nil, apperr.ErrForbidden
		}
		var in Input
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := in.check(cal.Config); err != nil {
			return nil, err
		}
		uid := identity.From(ctx).ID()
		err = pgx.BeginFunc(ctx, hs.DB, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE plugin_events SET title=$2, description=$3, location=$4, url=$5, category=$6, starts_at=$7, ends_at=$8,
				all_day=$9, tz=$10, rrule=$11, exdates=$12::date[], registration=$13, capacity=$14, waitlist=$15, reg_deadline=$16, reminders=$17,
				updated_by=$18, updated_at=now(), sequence=sequence+1 WHERE id=$1`,
				id, in.Title, in.Description, in.Location, in.URL, in.Category, in.StartsAt, in.EndsAt, in.AllDay, in.TZ, in.RRule, in.ExDates,
				in.Registration, in.Capacity, *in.Waitlist, in.RegDeadline, in.Reminders, nullID(uid)); err != nil {
				return err
			}
			// more places: move people up from the waiting list
			if in.Capacity == 0 || in.Capacity > before.Capacity {
				if err := p.promoteAll(ctx, tx, id); err != nil {
					return err
				}
			}
			return audit.Log(ctx, tx, "event.update", "event", id, before, in)
		})
		if err != nil {
			return nil, err
		}
		p.changed(ctx, cal.ID)
		e, _, err := p.event(ctx, id, "")
		return e, err
	})

	// DELETE /events/{id}?notify=1 deletes an event (and tells registered people).
	hs.Handle("DELETE", "/events/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		e, cal, err := p.event(ctx, id, "")
		if err != nil {
			return nil, err
		}
		if !e.CanEdit {
			return nil, apperr.ErrForbidden
		}
		err = pgx.BeginFunc(ctx, hs.DB, func(tx pgx.Tx) error {
			if r.URL.Query().Get("notify") != "0" {
				if err := p.notifyCancelled(ctx, tx, e, cal); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM plugin_events WHERE id=$1`, id); err != nil {
				return err
			}
			return audit.Log(ctx, tx, "event.delete", "event", id, e, nil)
		})
		if err != nil {
			return nil, err
		}
		p.changed(ctx, cal.ID)
		return h.NoContent{}, nil
	})

	// POST /events/{id}/registrations {"occurrence": "YYYY-MM-DD", "note": "", "interested": false}
	// registers the caller (or the waiting list when full); interested=true
	// only asks for reminders.
	hs.Handle("POST", "/events/{id}/registrations", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Occurrence string `json:"occurrence"`
			Note       string `json:"note"`
			Interested bool   `json:"interested"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return p.register(r.Context(), id, in.Occurrence, strings.TrimSpace(in.Note), in.Interested)
	})
	// DELETE /events/{id}/registrations?occurrence= cancels the caller's registration.
	hs.Handle("DELETE", "/events/{id}/registrations", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		uid := identity.From(r.Context()).ID()
		if uid == 0 {
			return nil, apperr.ErrUnauthorized
		}
		return h.NoContent{}, p.cancel(r.Context(), id, r.URL.Query().Get("occurrence"), uid)
	})
	// GET /events/{id}/registrations?occurrence= lists participants (managers).
	hs.Handle("GET", "/events/{id}/registrations", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		e, _, err := p.event(ctx, id, "")
		if err != nil {
			return nil, err
		}
		if !e.CanManage {
			return nil, apperr.ErrForbidden
		}
		rows, err := hs.DB.Query(ctx, `SELECT r.id, r.occurrence::text, r.status, r.note, r.created_at, u.id, u.username,
				COALESCE(NULLIF(u.display_name, ''), u.username), COALESCE(u.email, '')
			FROM plugin_event_registrations r JOIN users u ON u.id = r.user_id
			WHERE r.event_id=$1 AND ($2 = '' OR r.occurrence = $2::date) AND r.status <> 'cancelled'
			ORDER BY r.occurrence, CASE r.status WHEN 'registered' THEN 0 WHEN 'waitlist' THEN 1 ELSE 2 END, r.created_at`, id, r.URL.Query().Get("occurrence"))
		if err != nil {
			return nil, err
		}
		type reg struct {
			ID         int64     `json:"id"`
			Occurrence string    `json:"occurrence"`
			Status     string    `json:"status"`
			Note       string    `json:"note"`
			CreatedAt  time.Time `json:"created_at"`
			UserID     int64     `json:"user_id"`
			Username   string    `json:"username"`
			Name       string    `json:"name"`
			Email      string    `json:"email"`
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (reg, error) {
			var x reg
			return x, r.Scan(&x.ID, &x.Occurrence, &x.Status, &x.Note, &x.CreatedAt, &x.UserID, &x.Username, &x.Name, &x.Email)
		})
	})
	// DELETE /events/{id}/registrations/{rid} removes a participant (managers).
	hs.Handle("DELETE", "/events/{id}/registrations/{rid}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		rid, err := h.PathID(r, "rid")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		e, _, err := p.event(ctx, id, "")
		if err != nil {
			return nil, err
		}
		if !e.CanManage {
			return nil, apperr.ErrForbidden
		}
		var occ string
		var uid int64
		if err := hs.DB.QueryRow(ctx, `SELECT occurrence::text, user_id FROM plugin_event_registrations WHERE id=$1 AND event_id=$2`, rid, id).Scan(&occ, &uid); err != nil {
			return nil, apperr.NotFound("registration")
		}
		return h.NoContent{}, p.cancel(ctx, id, occ, uid)
	})

	// GET /me/registrations → the caller's upcoming registrations
	hs.Handle("GET", "/me/registrations", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		uid := identity.From(ctx).ID()
		if uid == 0 {
			return nil, apperr.ErrUnauthorized
		}
		rows, err := hs.DB.Query(ctx, `SELECT r.event_id, r.occurrence::text, r.status, ev.title, ev.calendar_id FROM plugin_event_registrations r
			JOIN plugin_events ev ON ev.id = r.event_id WHERE r.user_id=$1 AND r.status <> 'cancelled' AND r.occurrence >= current_date - 1
			ORDER BY r.occurrence LIMIT 200`, uid)
		if err != nil {
			return nil, err
		}
		type item struct {
			EventID    int64  `json:"event_id"`
			Occurrence string `json:"occurrence"`
			Status     string `json:"status"`
			Title      string `json:"title"`
			CalendarID int64  `json:"calendar_id"`
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (item, error) {
			var x item
			return x, r.Scan(&x.EventID, &x.Occurrence, &x.Status, &x.Title, &x.CalendarID)
		})
	})

	// GET /upcoming?calendar=&limit=&days= → next events of all visible calendars
	hs.Handle("GET", "/upcoming", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var cals []int64
		if c := h.QueryInt(r, "calendar", 0); c > 0 {
			cals = []int64{int64(c)}
		}
		list, err := p.upcoming(r.Context(), cals, time.Now(), h.QueryInt(r, "days", 60), h.QueryInt(r, "limit", 10))
		if err != nil {
			return nil, err
		}
		type item struct {
			EventID    int64     `json:"event_id"`
			CalendarID int64     `json:"calendar_id"`
			Calendar   string    `json:"calendar"`
			Title      string    `json:"title"`
			Location   string    `json:"location"`
			Start      time.Time `json:"start"`
			End        time.Time `json:"end"`
			AllDay     bool      `json:"all_day"`
			Date       string    `json:"date"` // occurrence key
		}
		out := []item{}
		for _, x := range list {
			out = append(out, item{x.Event.ID, x.Event.CalendarID, x.Cal, x.Event.Title, x.Event.Location, x.Occ, x.End, x.Event.AllDay, x.Date})
		}
		return out, nil
	})

	// PUT /events/{id}/occurrences/{date} changes one occurrence of a series
	// (title, description, location, start and end; empty = as the series).
	// DELETE resets it to the series.
	hs.Handle("PUT", "/events/{id}/occurrences/{date}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var ov Override
		if err := h.Decode(r, &ov); err != nil {
			return nil, err
		}
		return p.setOverride(r.Context(), r, &ov, r.URL.Query().Get("notify") != "0")
	})
	hs.Handle("DELETE", "/events/{id}/occurrences/{date}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if _, err := p.setOverride(r.Context(), r, nil, r.URL.Query().Get("notify") != "0"); err != nil {
			return nil, err
		}
		return h.NoContent{}, nil
	})

	p.icalRoutes(hs)
	return nil
}

// setOverride stores (ov != nil) or removes the change of one occurrence.
func (p *Plugin) setOverride(ctx context.Context, r *http.Request, ov *Override, notify bool) (any, error) {
	id, err := h.PathID(r, "id")
	if err != nil {
		return nil, err
	}
	date := r.PathValue("date")
	before, cal, err := p.event(ctx, id, "")
	if err != nil {
		return nil, err
	}
	if !before.CanEdit {
		return nil, apperr.ErrForbidden
	}
	if before.RRule == "" {
		return nil, apperr.BadRequest("only occurrences of a series can be changed one by one; change the event itself")
	}
	old, ok := before.InstanceAt(date)
	if !ok {
		return nil, apperr.NotFound("occurrence")
	}
	if ov != nil {
		ov.Title, ov.Location = strings.TrimSpace(ov.Title), strings.TrimSpace(ov.Location)
		if utf8.RuneCountInString(ov.Title) > 200 || utf8.RuneCountInString(ov.Location) > 300 || len(ov.Description) > 50000 {
			return nil, apperr.BadRequest("title, location or description too long")
		}
		if (ov.StartsAt == nil) != (ov.EndsAt == nil) {
			return nil, apperr.BadRequest("set start and end together")
		}
		if ov.StartsAt != nil {
			if ov.EndsAt.Before(*ov.StartsAt) {
				return nil, apperr.BadRequest("the end must not be before the start")
			}
			if d := ov.StartsAt.Sub(old.Original); d > MaxShift || d < -MaxShift {
				return nil, apperr.BadRequest("an occurrence can be moved by at most 31 days")
			}
			if ov.StartsAt.Equal(old.Original) && ov.EndsAt.Equal(old.Original.Add(before.EndsAt.Sub(before.StartsAt))) {
				ov.StartsAt, ov.EndsAt = nil, nil // unchanged
			}
		}
		if ov.Title == before.Title {
			ov.Title = ""
		}
		if ov.Location == before.Location {
			ov.Location = ""
		}
		if ov.Description == before.Description {
			ov.Description = ""
		}
		if *ov == (Override{}) {
			ov = nil
		}
	}
	uid := identity.From(ctx).ID()
	err = pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		var q string
		args := []any{id, date, nullID(uid)}
		if ov == nil {
			q = `UPDATE plugin_events SET overrides = overrides - $2, updated_by=$3, updated_at=now(), sequence=sequence+1 WHERE id=$1`
		} else {
			q = `UPDATE plugin_events SET overrides = jsonb_set(overrides, ARRAY[$2::text], $4::jsonb), updated_by=$3, updated_at=now(), sequence=sequence+1 WHERE id=$1`
			args = append(args, ov)
		}
		if _, err := tx.Exec(ctx, q, args...); err != nil {
			return err
		}
		after, err := p.eventTx(ctx, tx, id)
		if err != nil {
			return err
		}
		now, _ := after.InstanceAt(date)
		// people registered for this occurrence learn about a new time or place
		if notify && now.Start.After(time.Now()) && (!now.Start.Equal(old.Start) || !now.End.Equal(old.End) || after.instLocation(now) != before.instLocation(old)) {
			rows, err := tx.Query(ctx, `SELECT user_id FROM plugin_event_registrations WHERE event_id=$1 AND occurrence=$2 AND status IN ('registered', 'waitlist', 'interested')`, id, date)
			if err != nil {
				return err
			}
			users, err := pgx.CollectRows(rows, pgx.RowTo[int64])
			if err != nil {
				return err
			}
			for _, u := range users {
				if err := p.mailUser(ctx, tx, u, after, cal, now.Start, "mail_occurrence_changed"); err != nil {
					return err
				}
			}
		}
		return audit.Log(ctx, tx, "event.occurrence", "event", id, map[string]any{"date": date, "override": old.Override}, map[string]any{"date": date, "override": ov})
	})
	if err != nil {
		return nil, err
	}
	p.changed(ctx, cal.ID)
	e, _, err := p.event(ctx, id, "")
	return e, err
}

// eventTx loads an event inside a transaction (no permission check).
func (p *Plugin) eventTx(ctx context.Context, tx pgx.Tx, id int64) (*Event, error) {
	rows, err := tx.Query(ctx, `SELECT `+eventCols+eventFrom+` WHERE e.id=$1`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectExactlyOneRow(rows, scanEvent)
}

func nullID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// changed tells other parts (watches, caches) that the calendar changed.
func (p *Plugin) changed(ctx context.Context, calendar int64) {
	p.host.Content.Touch(ctx, calendar)
}

// ---------------------------------------------------------------- registrations

func (p *Plugin) register(ctx context.Context, id int64, occurrence, note string, interested bool) (any, error) {
	uid := identity.From(ctx).ID()
	if uid == 0 {
		return nil, apperr.ErrUnauthorized
	}
	if len(note) > 500 {
		return nil, apperr.BadRequest("the note may have at most 500 characters")
	}
	e, cal, err := p.event(ctx, id, "")
	if err != nil {
		return nil, err
	}
	start, ok := e.OccurrenceAt(occurrence)
	if !ok {
		return nil, apperr.BadRequest("the event does not take place on %q", occurrence)
	}
	if !start.After(time.Now()) {
		return nil, apperr.BadRequest("the event has already started")
	}
	if !interested {
		if !e.Registration {
			return nil, apperr.BadRequest("this event has no registration")
		}
		if !e.regOpen(start, time.Now()) {
			return nil, apperr.Conflict("registration is closed")
		}
	}
	status := "interested"
	err = pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		// serialize registrations of an event (capacity)
		if _, err := tx.Exec(ctx, `SELECT 1 FROM plugin_events WHERE id=$1 FOR UPDATE`, id); err != nil {
			return err
		}
		var current string
		_ = tx.QueryRow(ctx, `SELECT status FROM plugin_event_registrations WHERE event_id=$1 AND occurrence=$2 AND user_id=$3`, id, occurrence, uid).Scan(&current)
		if current == "registered" || current == "waitlist" {
			if interested {
				return apperr.Conflict("you are already registered")
			}
			status = current
			_, err := tx.Exec(ctx, `UPDATE plugin_event_registrations SET note=$4, updated_at=now() WHERE event_id=$1 AND occurrence=$2 AND user_id=$3`, id, occurrence, uid, note)
			return err
		}
		if !interested {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM plugin_event_registrations WHERE event_id=$1 AND occurrence=$2 AND status='registered'`, id, occurrence).Scan(&n); err != nil {
				return err
			}
			switch {
			case e.Capacity == 0 || n < e.Capacity:
				status = "registered"
			case e.Waitlist:
				status = "waitlist"
			default:
				return apperr.Conflict("the event is fully booked")
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO plugin_event_registrations(event_id, occurrence, user_id, status, note) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (event_id, occurrence, user_id) DO UPDATE SET status=EXCLUDED.status, note=EXCLUDED.note, created_at=now(), updated_at=now()`,
			id, occurrence, uid, status, note); err != nil {
			return err
		}
		if status != "interested" {
			return p.mailUser(ctx, tx, uid, e, cal, start, "mail_"+status)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]string{"status": status}, nil
}

// cancel removes a registration and moves the first person of the waiting list up.
func (p *Plugin) cancel(ctx context.Context, id int64, occurrence string, uid int64) error {
	e, cal, err := p.event(ctx, id, "")
	if err != nil {
		return err
	}
	if _, err := time.Parse("2006-01-02", occurrence); err != nil {
		return apperr.BadRequest("occurrence (YYYY-MM-DD) is required")
	}
	return pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM plugin_events WHERE id=$1 FOR UPDATE`, id); err != nil {
			return err
		}
		var was string
		if err := tx.QueryRow(ctx, `SELECT status FROM plugin_event_registrations WHERE event_id=$1 AND occurrence=$2 AND user_id=$3
			AND status <> 'cancelled' FOR UPDATE`, id, occurrence, uid).Scan(&was); err != nil {
			if err == pgx.ErrNoRows {
				return apperr.NotFound("registration")
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE plugin_event_registrations SET status='cancelled', updated_at=now()
			WHERE event_id=$1 AND occurrence=$2 AND user_id=$3`, id, occurrence, uid); err != nil {
			return err
		}
		if start, ok := e.OccurrenceAt(occurrence); ok && start.After(time.Now()) && was == "registered" {
			if err := p.mailUser(ctx, tx, uid, e, cal, start, "mail_cancelled"); err != nil {
				return err
			}
			return p.promote(ctx, tx, e, cal, occurrence)
		}
		return nil
	})
}

// promote moves people from the waiting list into free places of an occurrence.
func (p *Plugin) promote(ctx context.Context, tx pgx.Tx, e *Event, cal *calendar, occurrence string) error {
	start, ok := e.OccurrenceAt(occurrence)
	if !ok || !start.After(time.Now()) {
		return nil
	}
	for i := 0; i < 1000; i++ {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM plugin_event_registrations WHERE event_id=$1 AND occurrence=$2 AND status='registered'`, e.ID, occurrence).Scan(&n); err != nil {
			return err
		}
		if e.Capacity > 0 && n >= e.Capacity {
			return nil
		}
		var uid int64
		err := tx.QueryRow(ctx, `UPDATE plugin_event_registrations SET status='registered', updated_at=now() WHERE id = (
			SELECT id FROM plugin_event_registrations WHERE event_id=$1 AND occurrence=$2 AND status='waitlist' ORDER BY created_at LIMIT 1)
			RETURNING user_id`, e.ID, occurrence).Scan(&uid)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if err := p.mailUser(ctx, tx, uid, e, cal, start, "mail_promoted"); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plugin) promoteAll(ctx context.Context, tx pgx.Tx, id int64) error {
	rows, err := tx.Query(ctx, `SELECT `+eventCols+eventFrom+` WHERE e.id=$1`, id)
	if err != nil {
		return err
	}
	e, err := pgx.CollectExactlyOneRow(rows, scanEvent)
	if err != nil {
		return err
	}
	var dates []string
	r2, err := tx.Query(ctx, `SELECT DISTINCT occurrence::text FROM plugin_event_registrations WHERE event_id=$1 AND status='waitlist'`, id)
	if err != nil {
		return err
	}
	for r2.Next() {
		var d string
		if err := r2.Scan(&d); err != nil {
			r2.Close()
			return err
		}
		dates = append(dates, d)
	}
	r2.Close()
	cal, err := p.loadCalendar(ctx, e.CalendarID, "")
	if err != nil {
		return err
	}
	for _, d := range dates {
		if err := p.promote(ctx, tx, e, cal, d); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plugin) notifyCancelled(ctx context.Context, tx pgx.Tx, e *Event, cal *calendar) error {
	rows, err := tx.Query(ctx, `SELECT user_id, occurrence::text FROM plugin_event_registrations
		WHERE event_id=$1 AND status IN ('registered', 'waitlist') AND occurrence >= current_date`, e.ID)
	if err != nil {
		return err
	}
	type reg struct {
		uid int64
		occ string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (reg, error) {
		var x reg
		return x, r.Scan(&x.uid, &x.occ)
	})
	if err != nil {
		return err
	}
	for _, x := range list {
		if start, ok := e.OccurrenceAt(x.occ); ok && start.After(time.Now()) {
			if err := p.mailUser(ctx, tx, x.uid, e, cal, start, "mail_event_cancelled"); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- e-mails

// mailUser queues an e-mail about an occurrence to a user (in the user's language).
func (p *Plugin) mailUser(ctx context.Context, q db.Querier, uid int64, e *Event, cal *calendar, start time.Time, kind string) error {
	if !p.host.Mail.Enabled() {
		return nil
	}
	us, err := p.host.Mail.Users(ctx, q, []int64{uid})
	if err != nil || len(us) == 0 || us[0].Email == "" {
		return err
	}
	u := us[0]
	base := strings.TrimSuffix(p.host.Config.Get().Server.BaseURL, "/")
	in := e.instanceByStart(start)
	when := FormatWhen(in.Start, in.End, e.AllDay, e.Loc(), u.Lang)
	link := fmt.Sprintf("%s/e/%d#event-%d-%s", base, e.CalendarID, e.ID, in.Date)
	location := e.instLocation(in)
	params := map[string]string{"name": u.Name, "title": e.instTitle(in), "when": when, "location": location, "calendar": cal.Title, "link": link}
	if in.Override != nil && in.Override.StartsAt != nil {
		params["was"] = FormatWhen(in.Original, in.Original.Add(e.EndsAt.Sub(e.StartsAt)), e.AllDay, e.Loc(), u.Lang)
	}
	t := func(key string) string { return p.host.T(ctx, u.Lang, "content.events."+key, params) }
	body := t(kind + "_body")
	if params["was"] != "" {
		body += "\n" + t("mail_was")
	}
	if location != "" {
		body += "\n\n" + t("mail_location")
	}
	body += "\n\n" + link + "\n"
	m := mail.Message{To: []string{u.Email}, Subject: t(kind + "_subject"), Text: body, Source: "events", Ref: fmt.Sprintf("event:%d", e.ID), Owner: -1}
	_, err = p.host.Mail.Enqueue(ctx, q, m)
	return err
}

// sendReminders queues due reminders (worker, every minute).
func (p *Plugin) sendReminders(ctx context.Context) error {
	if !p.host.Mail.Enabled() {
		return nil
	}
	ctx = identity.With(ctx, identity.CLI())
	rows, err := p.host.DB.Query(ctx, `SELECT `+eventCols+eventFrom+` WHERE cardinality(e.reminders) > 0
		AND (e.rrule <> '' OR e.starts_at BETWEEN now() AND now() + interval '29 days')`)
	if err != nil {
		return err
	}
	events, err := pgx.CollectRows(rows, scanEvent)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, e := range events {
		max := e.Reminders[0] // sorted descending
		for _, in := range e.Instances(now, now.Add(time.Duration(max+1)*time.Minute)) {
			start, date := in.Start, in.Date
			if !start.After(now) {
				continue
			}
			for _, m := range e.Reminders {
				if now.Before(start.Add(-time.Duration(m) * time.Minute)) {
					continue
				}
				if err := p.remind(ctx, e, start, date, m); err != nil {
					p.host.Log.Error("event reminder failed", "event", e.ID, "err", err)
				}
			}
		}
	}
	return nil
}

func (p *Plugin) remind(ctx context.Context, e *Event, start time.Time, date string, minutes int) error {
	cal, err := p.loadCalendar(ctx, e.CalendarID, "")
	if err != nil {
		return nil // calendar deleted
	}
	return pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `INSERT INTO plugin_event_reminders_sent(event_id, occurrence, user_id, minutes)
			SELECT event_id, occurrence, user_id, $3 FROM plugin_event_registrations
			WHERE event_id=$1 AND occurrence=$2 AND status IN ('registered', 'interested')
			ON CONFLICT DO NOTHING RETURNING user_id`, e.ID, date, minutes)
		if err != nil {
			return err
		}
		var users []int64
		for rows.Next() {
			var u int64
			if err := rows.Scan(&u); err != nil {
				rows.Close()
				return err
			}
			users = append(users, u)
		}
		rows.Close()
		for _, u := range users {
			if err := p.mailUser(ctx, tx, u, e, cal, start, "mail_reminder"); err != nil {
				return err
			}
		}
		return nil
	})
}
