package core_properties

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/mail"
)

// Subscriptions of reports: the rows of a report of a page by e-mail,
// daily or weekly, always with the current rights of the subscriber.
// Confidential rows are sent with title and link only, strictly
// confidential ones not at all.

// Subscription is a subscribed report.
type Subscription struct {
	ID        int64             `json:"id"`
	ElementID int64             `json:"element_id"`
	Title     string            `json:"title"`
	Params    map[string]string `json:"params"`
	Frequency string            `json:"frequency"`
	NextAt    time.Time         `json:"next_at"`
}

const (
	maxSubsPerUser = 50
	maxMailRows    = 50
)

func subRoutes(p *Plugin) {
	host := p.host
	// GET /subscriptions?element=ID – own subscriptions
	host.Handle("GET", "/subscriptions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		me := identity.From(r.Context())
		if me == nil || me.ID() <= 0 {
			return nil, apperr.ErrUnauthorized
		}
		el := h.QueryInt(r, "element", 0)
		rows, err := host.DB.Query(r.Context(), `SELECT id, element_id, title, params, frequency, next_at FROM plugin_prop_subs
			WHERE user_id=$1 AND ($2 = 0 OR element_id = $2) ORDER BY created_at`, me.ID(), el)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Subscription, error) {
			var s Subscription
			var raw []byte
			err := r.Scan(&s.ID, &s.ElementID, &s.Title, &raw, &s.Frequency, &s.NextAt)
			_ = json.Unmarshal(raw, &s.Params)
			return s, err
		})
	})
	// POST /subscriptions {"element_id": 5, "category_id": 3, "params": {...}, "title": "…", "frequency": "weekly"}
	host.Handle("POST", "/subscriptions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		me := identity.From(ctx)
		if me == nil || me.ID() <= 0 {
			return nil, apperr.ErrUnauthorized
		}
		var in struct {
			ElementID  int64             `json:"element_id"`
			CategoryID int64             `json:"category_id"`
			Params     map[string]string `json:"params"`
			Title      string            `json:"title"`
			Frequency  string            `json:"frequency"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if in.Frequency != "daily" && in.Frequency != "weekly" {
			return nil, apperr.BadRequest("frequency: daily or weekly")
		}
		cat, err := host.Content.ElementCategory(ctx, in.ElementID, acl.View)
		if err != nil {
			return nil, err
		}
		ri := reportInput{Params: in.Params, CategoryID: cat, ElementID: in.ElementID}
		if _, err := ri.query(); err != nil {
			return nil, err
		}
		var n int
		if err := host.DB.QueryRow(ctx, `SELECT count(*) FROM plugin_prop_subs WHERE user_id=$1`, me.ID()).Scan(&n); err != nil {
			return nil, err
		}
		if n >= maxSubsPerUser {
			return nil, apperr.BadRequest("at most %d subscriptions", maxSubsPerUser)
		}
		title := strings.TrimSpace(in.Title)
		if len([]rune(title)) > 200 {
			title = string([]rune(title)[:200])
		}
		params, _ := json.Marshal(in.Params)
		next := nextRun(time.Now(), in.Frequency)
		s := Subscription{ElementID: in.ElementID, Title: title, Params: in.Params, Frequency: in.Frequency, NextAt: next}
		if err := host.DB.QueryRow(ctx, `INSERT INTO plugin_prop_subs(user_id, element_id, category_id, params, title, frequency, next_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, me.ID(), in.ElementID, cat, params, title, in.Frequency, next).Scan(&s.ID); err != nil {
			return nil, err
		}
		return h.Created{V: s}, nil
	})
	// DELETE /subscriptions/{id} – own subscriptions
	host.Handle("DELETE", "/subscriptions/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		tag, err := host.DB.Exec(r.Context(), `DELETE FROM plugin_prop_subs WHERE id=$1 AND user_id=$2`, id, identity.From(r.Context()).ID())
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, apperr.NotFound("subscription")
		}
		return h.NoContent{}, nil
	})
	host.RegisterWorker("subscriptions", 15*time.Minute, p.SendSubscriptions)
}

// nextRun is the next sending time (07:00 server time; weekly on Mondays).
func nextRun(from time.Time, freq string) time.Time {
	t := time.Date(from.Year(), from.Month(), from.Day(), 7, 0, 0, 0, from.Location())
	for !t.After(from) || (freq == "weekly" && t.Weekday() != time.Monday) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

// SendSubscriptions sends the due reports (worker).
func (p *Plugin) SendSubscriptions(ctx context.Context) error {
	rows, err := p.host.DB.Query(ctx, `SELECT id, user_id, element_id, category_id, params, title, frequency FROM plugin_prop_subs WHERE next_at <= now() ORDER BY next_at LIMIT 200`)
	if err != nil {
		return err
	}
	type due struct {
		id, user, el, cat int64
		params            map[string]string
		title, freq       string
	}
	var list []due
	for rows.Next() {
		var d due
		var raw []byte
		if err := rows.Scan(&d.id, &d.user, &d.el, &d.cat, &raw, &d.title, &d.freq); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal(raw, &d.params)
		list = append(list, d)
	}
	rows.Close()
	for _, d := range list {
		if _, err := p.host.DB.Exec(ctx, `UPDATE plugin_prop_subs SET next_at=$2 WHERE id=$1`, d.id, nextRun(time.Now(), d.freq)); err != nil {
			return err
		}
		if err := p.sendOne(ctx, d.user, d.el, d.params, d.title); err != nil {
			p.host.Log.Info("report subscription", "id", d.id, "err", err)
		}
	}
	return nil
}

func (p *Plugin) sendOne(ctx context.Context, uid, el int64, params map[string]string, title string) error {
	pr, err := p.host.Auth.LoadPrincipal(ctx, uid)
	if err != nil {
		return err
	}
	uctx := identity.With(ctx, pr)
	cat, err := p.host.Content.ElementCategory(uctx, el, acl.View)
	if err != nil {
		return nil // no longer visible: nothing is sent
	}
	us, err := p.host.Mail.Users(ctx, p.host.DB, []int64{uid})
	if err != nil || len(us) == 0 || us[0].Email == "" {
		return err
	}
	u := us[0]
	ri := reportInput{Params: params, CategoryID: cat, ElementID: el, Lang: u.Lang}
	q, err := ri.query()
	if err != nil {
		return err
	}
	q.Group, q.Group2, q.Limit, q.Offset = "", "", maxMailRows, 0
	res, err := p.Run(uctx, q)
	if err != nil {
		return err
	}
	base := strings.TrimSuffix(p.host.Config.Get().Server.BaseURL, "/")
	de := u.Lang == "de"
	tr := func(en, ger string) string {
		if de {
			return ger
		}
		return en
	}
	if title == "" {
		title = tr("Report", "Bericht")
	}
	cols := res.Columns
	if len(cols) > 4 {
		cols = cols[:4]
	}
	var text, body strings.Builder
	fmt.Fprintf(&text, "%s\n%s/e/%d\n\n", title, base, el)
	body.WriteString(`<!DOCTYPE html><html><body style="font-family:Arial,Helvetica,sans-serif;font-size:15px;line-height:1.5;color:#172b4d">`)
	fmt.Fprintf(&body, `<h1 style="font-size:20px"><a href="%s/e/%d">%s</a></h1><table style="border-collapse:collapse" cellpadding="6"><tr>`, html.EscapeString(base), el, html.EscapeString(title))
	body.WriteString(`<th align="left" style="border-bottom:2px solid #c1c7d0">` + html.EscapeString(tr("Element", "Element")) + `</th>`)
	for _, c := range cols {
		if c.Key != "title" {
			body.WriteString(`<th align="left" style="border-bottom:2px solid #c1c7d0">` + html.EscapeString(c.Label) + `</th>`)
		}
	}
	body.WriteString(`</tr>`)
	left := 0
	for _, row := range res.Rows {
		level := p.host.ACL.Classification(uctx, row.CategoryID)
		if level >= acl.StrictConfidential {
			left++
			continue
		}
		link := base + row.Link
		fmt.Fprintf(&text, "- %s – %s\n", row.Title, link)
		fmt.Fprintf(&body, `<tr><td style="border-bottom:1px solid #dfe1e6"><a href="%s">%s</a></td>`, html.EscapeString(link), html.EscapeString(row.Title))
		for _, c := range cols {
			if c.Key == "title" {
				continue
			}
			v := ""
			if level < acl.Confidential { // confidential: title and link only
				v = plain(builtinValue(row, c.Key), c.Type, res.Refs)
			}
			body.WriteString(`<td style="border-bottom:1px solid #dfe1e6">` + html.EscapeString(v) + `</td>`)
		}
		body.WriteString(`</tr>`)
	}
	body.WriteString(`</table>`)
	if left > 0 {
		note := fmt.Sprintf(tr("%d strictly confidential entries are only shown in the wiki.", "%d streng vertrauliche Einträge sind nur im Wiki sichtbar."), left)
		text.WriteString("\n" + note + "\n")
		body.WriteString(`<p>` + html.EscapeString(note) + `</p>`)
	}
	foot := tr("You subscribed to this report. To end the subscription, open the page and choose “End subscription”.",
		"Sie haben diesen Bericht abonniert. Zum Beenden öffnen Sie die Seite und wählen „Abo beenden“.")
	text.WriteString("\n" + foot + "\n")
	body.WriteString(`<p style="color:#5e6c84;font-size:13px;margin-top:24px">` + html.EscapeString(foot) + `</p></body></html>`)
	subject := fmt.Sprintf(tr("Report: %s (%d entries)", "Bericht: %s (%d Einträge)"), title, len(res.Rows)-left)
	_, err = p.host.Mail.Enqueue(ctx, p.host.DB, mail.Message{To: []string{u.Email}, Subject: subject, Text: text.String(), HTML: body.String(),
		Source: "core.properties", Ref: fmt.Sprintf("report:%d", el), Owner: -1})
	return err
}
