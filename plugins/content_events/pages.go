package content_events

// Dates of pages in calendars (plan "Aufgaben-Bericht", step 2): a
// calendar (or the block of upcoming events) takes categories (with or
// without their subcategories) and pages as sources. Their appointments in
// the text (core.macros.date) – and, if chosen, their tasks with a due date
// (core.html.task) – appear read-only, read with the permissions of the
// reader (content.Service.QueryData). They link to their page.

import (
	"context"
	"fmt"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

// kinds of data read from pages
const (
	pageDateKind = "core.macros.date"
	pageTaskKind = "core.html.task"
)

// pageEvents are the dates (and with tasks the open tasks with a due day)
// of the page sources between from and to, as read-only events; of every
// page those of the language version in lang (content.DataQuery.Prefer).
func (p *Plugin) pageEvents(ctx context.Context, sources []content.Source, tasks bool, from, to time.Time, lang string) []*Event {
	if len(sources) == 0 || p.host.Content == nil {
		return nil
	}
	if lang == "" {
		lang = p.host.Config.Get().I18n.DefaultLanguage
	}
	kinds := []string{pageDateKind}
	if tasks {
		kinds = append(kinds, pageTaskKind)
	}
	var out []*Event
	seen := map[int64]bool{}
	for _, src := range sources {
		for _, kind := range kinds {
			if _, ok := p.host.Content.DataKindOf(kind); !ok {
				continue // its plugin is switched off
			}
			q := content.DataQuery{Kind: kind, From: from.AddDate(0, 0, -1).Format("2006-01-02"), To: to.Format("2006-01-02"), Sort: "when", Limit: content.DataQueryMax, Prefer: lang}
			switch src.Kind {
			case "category":
				q.Category, q.Direct = src.ID, !src.Sub
			case "elements":
				q.Elements = src.IDs
			default:
				continue
			}
			if kind == pageTaskKind {
				q.States = []string{"open"}
			} else {
				q.States = []string{"planned"}
			}
			res, err := p.host.Content.QueryData(ctx, q)
			if err != nil {
				continue // a source that is not there any more shows nothing
			}
			for _, it := range res.Items {
				e := pageEvent(it, kind)
				if e == nil || seen[e.ID] || !e.StartsAt.Before(to) || e.EndsAt.Before(from) {
					continue
				}
				seen[e.ID] = true
				out = append(out, e)
			}
		}
	}
	return out
}

// pageEvent turns an entry of a page into a read-only event (nil: no day).
func pageEvent(it content.DataEntry, kind string) *Event {
	str := func(k string) string { s, _ := it.Data[k].(string); return s }
	day, err := time.Parse("2006-01-02", str("day"))
	if err != nil {
		return nil
	}
	e := &Event{
		ID:    externalID(fmt.Sprintf("page:%d", it.ElementID), fmt.Sprintf("%s/%s/%d", kind, it.Lang, it.Index)),
		Title: str("title"), TZ: "UTC", ExDates: []string{}, Reminders: []int{}, Overrides: map[string]Override{},
		External: true, Source: it.Path, PageID: it.ElementID, PageTitle: it.Title, PageKind: "date",
	}
	e.UID = fmt.Sprintf("page-%d-%s-%d@%s", it.ElementID, it.Lang, it.Index, kind)
	if kind == pageTaskKind {
		e.PageKind = "task"
	}
	if at, err := time.Parse(time.RFC3339, str("at")); err == nil {
		e.StartsAt, e.EndsAt = at, at.Add(time.Hour)
		if end, err := time.Parse(time.RFC3339, str("end")); err == nil && end.After(at) {
			e.EndsAt = end
		}
		return e
	}
	e.StartsAt, e.EndsAt, e.AllDay = day, day.AddDate(0, 0, 1), true
	return e
}
