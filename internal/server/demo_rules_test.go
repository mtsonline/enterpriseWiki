package server_test

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// The minimum rules of the demo (wish of the owner, 14.11.2026), checked on
// the content it creates:
//   - every page exists in German and English, with its own text in each,
//     and shows without a block error;
//   - every link inside the wiki leads to a page or category that exists;
//   - e-mail addresses are examples (@example.com);
//   - the tour is a handbook: every station follows the same structure
//     (intro, how it is done, what it looks like, what it is good for), the
//     stations are in a row without gaps (previous and next) and the first
//     one lists them all;
//   - the start page leads into both areas.
//
// The accessibility of every page (axe, light and dark) is checked by the
// browser tests (demo crawl in web/tests/e2e.test.mjs).

var (
	demoLinkRe  = regexp.MustCompile(`href="/(e|c)/(\d+)[#"?]`)
	demoMailRe  = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	stationHead = map[string][]string{
		"en": {"How it is done", "What it looks like", "What it is good for"},
		"de": {"So geht&#39;s", "So sieht es aus", "Wofür Sie es nutzen"},
	}
)

// Short, clear sentences (CLAUDE.md, section 9; the same rule as
// internal/i18n/readability_test.go for the interface): at most 20 words a
// sentence, at most 22 letters a word (a hyphen splits a word).
var (
	demoTagRe      = regexp.MustCompile(`<[^>]*>|\{[^{}]*\}|&[a-z]+;|&#\d+;`)
	demoSentenceRe = regexp.MustCompile(`[.!?;:]\**(\s+|$)|\n`)
	demoCodeRe     = regexp.MustCompile("`[^`]*`|<code>.*?</code>|<pre>(?s:.*?)</pre>|https?://\\S+|mailto:[\\w.+-]+@[\\w.-]+|[\\w.+-]+@[\\w.-]+|/[a-z]\\S*")
)

func demoLongSentences(src string) []string {
	src = demoCodeRe.ReplaceAllString(src, " x ")
	text := demoTagRe.ReplaceAllString(src, "\n")
	var out []string
	for _, s := range demoSentenceRe.Split(text, -1) {
		words := strings.Fields(strings.Trim(s, " -–*#|>"))
		long := len(words) > 20
		for _, w := range words {
			for _, part := range strings.Split(w, "-") {
				if utf8.RuneCountInString(strings.Trim(part, ".,;:!?()„“”\"'*_")) > 22 {
					long = true
				}
			}
		}
		if long {
			out = append(out, strings.Join(words, " "))
		}
	}
	return out
}

type demoView struct {
	ID         int64  `json:"id"`
	CategoryID int64  `json:"category_id"`
	Type       string `json:"type"`
	Position   int    `json:"position"`
	Title      string `json:"title"`
	ShownLang  string `json:"shown_lang"`
	Fallback   bool   `json:"fallback"`
	HTML       string `json:"html"`
	ShortURL   string `json:"short_url"`
	Body       struct {
		Source string `json:"source"`
	} `json:"body"`
}

func checkDemoRules(t *testing.T, e *env, admin *client) {
	t.Helper()
	ctx := context.Background()
	// everything in the demo area: the pages of its categories
	rows, err := e.app.DB.Query(ctx, `WITH RECURSIVE t AS (SELECT id FROM categories WHERE id = (SELECT min(ref::bigint) FROM demo_objects WHERE kind='category')
		UNION SELECT c.id FROM categories c JOIN t ON c.parent_id = t.id)
		SELECT e.id FROM elements e WHERE e.category_id IN (SELECT id FROM t) AND e.deleted_at IS NULL ORDER BY e.id`)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) < 60 {
		t.Fatalf("demo: only %d elements", len(ids))
	}
	views := map[string]map[int64]demoView{"en": {}, "de": {}}
	for _, id := range ids {
		for _, l := range []string{"en", "de"} {
			var v demoView
			admin.must("GET", "/api/v1/elements/"+strconv.FormatInt(id, 10)+"?edit=1&lang="+l, nil, &v, 200)
			views[l][id] = v
		}
	}
	exists := func(kind string, id int64) bool {
		table := map[string]string{"e": "elements", "c": "categories"}[kind]
		var ok bool
		_ = e.app.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id=$1 AND deleted_at IS NULL)`, id).Scan(&ok)
		return ok
	}
	checked := map[string]bool{}
	for _, id := range ids {
		en, de := views["en"][id], views["de"][id]
		name := strconv.FormatInt(id, 10) + " " + en.Title
		for _, v := range []demoView{en, de} {
			if v.Fallback || strings.TrimSpace(v.Title) == "" {
				t.Errorf("demo page %s: no %s version", name, v.ShownLang)
			}
			if strings.Contains(v.HTML, "ew-macro-error") {
				t.Errorf("demo page %s (%s): a block with an error: %s", name, v.ShownLang, v.HTML)
			}
			for _, m := range demoLinkRe.FindAllStringSubmatch(v.HTML, -1) {
				n, _ := strconv.ParseInt(m[2], 10, 64)
				if key := m[1] + m[2]; !checked[key] {
					checked[key] = true
					if !exists(m[1], n) {
						t.Errorf("demo page %s: dead link /%s/%d", name, m[1], n)
					}
				}
			}
			for _, mail := range demoMailRe.FindAllString(v.HTML, -1) {
				if !strings.HasSuffix(mail, "@example.com") {
					t.Errorf("demo page %s: e-mail address %q is no example", name, mail)
				}
			}
		}
		// a page with text has its own text in each language, in short sentences
		if en.Type == "core.html" && en.HTML == de.HTML {
			t.Errorf("demo page %s: the same text in German and English", name)
		}
		for _, v := range []demoView{en, de} {
			if v.Type != "core.html" {
				continue
			}
			for _, s := range demoLongSentences(v.Body.Source) {
				t.Errorf("demo page %s (%s): sentence too long: %q", name, v.ShownLang, s)
			}
		}
	}

	// the tour: a handbook of stations
	var tour int64
	if err := e.app.DB.QueryRow(ctx, `SELECT id FROM categories WHERE titles->>'en' = 'Tour of the features'`).Scan(&tour); err != nil {
		t.Fatal(err)
	}
	var set struct {
		Own struct {
			Display struct {
				View string `json:"view"`
			} `json:"display"`
		} `json:"own"`
	}
	admin.must("GET", "/api/v1/categories/"+strconv.FormatInt(tour, 10)+"/settings", nil, &set, 200)
	if set.Own.Display.View != "handbook" {
		t.Errorf("the tour is shown as %q, want handbook (previous and next page)", set.Own.Display.View)
	}
	var stations []demoView
	for _, id := range ids {
		if v := views["en"][id]; v.CategoryID == tour {
			stations = append(stations, v)
		}
	}
	slices.SortFunc(stations, func(a, b demoView) int { return a.Position - b.Position })
	if len(stations) < 20 {
		t.Fatalf("tour: %d stations", len(stations))
	}
	for i, s := range stations {
		if s.Position != i || s.Type != "core.html" {
			t.Errorf("station %q: position %d (want %d, no gaps), type %s", s.Title, s.Position, i, s.Type)
		}
		for _, l := range []string{"en", "de"} {
			v := views[l][s.ID]
			if !strings.HasPrefix(strings.TrimSpace(v.HTML), "<p>") {
				t.Errorf("station %q (%s) does not start with its introduction", v.Title, l)
			}
			at := 0
			for _, hd := range stationHead[l] {
				i := strings.Index(v.HTML[at:], ">"+hd+"</h2>")
				if i < 0 {
					t.Errorf("station %q (%s): heading %q missing or out of order", v.Title, l, hd)
					break
				}
				at += i
			}
		}
		if i > 0 && !strings.Contains(views["en"][stations[0].ID].HTML, `href="/e/`+strconv.FormatInt(s.ID, 10)+`"`) {
			t.Errorf("the first station does not list %q", s.Title)
		}
	}

	// the start page of the site leads into both areas
	home := e.app.Config.Get().UI.HomeElement
	var company int64
	if err := e.app.DB.QueryRow(ctx, `SELECT id FROM categories WHERE titles->>'en' = 'Enterprise IT Services'`).Scan(&company); err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{"en", "de"} {
		v := views[l][home]
		for _, want := range []string{`href="/c/` + strconv.FormatInt(company, 10) + `"`, `href="/c/` + strconv.FormatInt(tour, 10) + `"`, `src="/brand/logo.webp"`} {
			if !strings.Contains(v.HTML, want) {
				t.Errorf("start page (%s): %s missing", l, want)
			}
		}
	}

	// the next events on the intranet (the block without a calendar shows
	// all calendars the reader may see – it once showed none)
	var intranet int64
	if err := e.app.DB.QueryRow(ctx, `SELECT id FROM elements WHERE titles->>'en' = 'Intranet' AND category_id=$1`, company).Scan(&intranet); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(views["en"][intranet].HTML, `class="ewe-upcoming"`) {
		t.Errorf("intranet: the next events are missing: %s", views["en"][intranet].HTML)
	}
}
