package core_macros

// Wiki blocks of priority B, part 2 (plan of blocks, 3.2): a cloud of tags,
// the latest changes of a page and the people who wrote it. Read with the
// permissions of the reader; people only by their display name.

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

// pageOf: the page a block is about (a chosen one or the page it is on).
func pageOf(mc content.MacroContext) (int64, error) {
	if v := idParam(mc.Params["page"]); v > 0 {
		return v, nil
	}
	if mc.ElementID > 0 {
		return mc.ElementID, nil
	}
	return 0, fmt.Errorf("choose a page (here is no page of its own)")
}

func countParam(v string, def, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return def
	}
	return min(n, max)
}

// ---------------------------------------------------------------- tag cloud

type tagcloud struct{}

func (tagcloud) Name() string              { return "tagcloud" }
func (tagcloud) Labels() map[string]string { return lm("Tag cloud", "Schlagwort-Wolke") }
func (tagcloud) Description() map[string]string {
	return lm("The tags of an area (or of the whole wiki), the most used larger; each one leads to its pages. Only what the reader may see counts.",
		"Die Schlagwörter eines Bereichs (oder des ganzen Wikis), die häufigsten größer; jedes führt zu seinen Seiten. Es zählt nur, was die lesende Person sehen darf.")
}
func (tagcloud) HasBody() bool { return false }
func (tagcloud) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "category", Labels: lm("Area (empty: the whole wiki)", "Bereich (leer: das ganze Wiki)"), Type: "custom:category"},
		{Name: "max", Labels: lm("At most (1–100)", "Höchstens (1–100)"), Type: "number", Default: "40"},
		{Name: "layout", Labels: lm("Display", "Darstellung"), Type: "select", Options: []string{"cloud", "list"}, Default: "cloud",
			OptionLabels: map[string]map[string]string{"cloud": lm("Cloud (sizes by use)", "Wolke (Größe nach Häufigkeit)"), "list": lm("List with counts", "Liste mit Anzahl")}},
		{Name: "label", Labels: lm("Name for screen readers", "Name für Screenreader"), Type: "text"},
	}
}

func (tagcloud) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	root := idParam(mc.Params["category"])
	tags, err := mc.Service.TagCloud(ctx, root, countParam(mc.Params["max"], 40, 100))
	if err != nil {
		return "", fmt.Errorf("the area %d is not there or not visible to you", root)
	}
	if len(tags) == 0 {
		return `<p class="ew-muted">` + esc(tr(mc.Lang, "No tags yet.", "Noch keine Schlagwörter.")) + `</p>`, nil
	}
	label := strings.TrimSpace(mc.Params["label"])
	if label == "" {
		label = tr(mc.Lang, "Tags", "Schlagwörter")
	}
	maxN, minN := tags[0].Count, tags[len(tags)-1].Count
	list := mc.Params["layout"] == "list"
	var b strings.Builder
	cls := "ew-tagcloud"
	if list {
		cls += " ew-tagcloud--list"
	}
	fmt.Fprintf(&b, `<nav class="%s" aria-label="%s"><ul>`, cls, esc(label))
	// alphabetical in the cloud (sizes show the use), by use in the list
	shown := tags
	if !list {
		shown = append([]content.TagCount(nil), tags...)
		sortTags(shown)
	}
	for _, t := range shown {
		q := url.Values{"tags": {t.Tag}}
		if root > 0 {
			q.Set("category", strconv.FormatInt(root, 10))
		}
		step := 1
		if maxN > minN {
			step = 1 + int(math.Round(4*math.Log(float64(t.Count-minN+1))/math.Log(float64(maxN-minN+1))))
		}
		count := fmt.Sprintf(tr(mc.Lang, "%d pages", "%d Seiten"), t.Count)
		if t.Count == 1 {
			count = tr(mc.Lang, "1 page", "1 Seite")
		}
		if list {
			fmt.Fprintf(&b, `<li><a class="ew-tag" href="/search?%s">#%s</a> <span class="ew-muted">%s</span></li>`, esc(q.Encode()), esc(t.Tag), esc(count))
		} else {
			// the size is only for the eye: the count is part of the name of the link
			fmt.Fprintf(&b, `<li><a class="ew-tag ew-tagcloud-%d" href="/search?%s">#%s<span class="sr-only"> (%s)</span></a></li>`, step, esc(q.Encode()), esc(t.Tag), esc(count))
		}
	}
	b.WriteString(`</ul></nav>`)
	return b.String(), nil
}

func sortTags(ts []content.TagCount) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].Tag < ts[j-1].Tag; j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}

// tr: a text of the block in the language of the page (English or German).
func tr(lang, en, de string) string {
	if strings.HasPrefix(lang, "de") {
		return de
	}
	return en
}

// ---------------------------------------------------------------- changes of a page

type pagechanges struct{}

func (pagechanges) Name() string { return "pagechanges" }
func (pagechanges) Labels() map[string]string {
	return lm("Changes of the page", "Änderungen der Seite")
}
func (pagechanges) Description() map[string]string {
	return lm("The latest saved versions of this (or another) page: when, by whom and the note of the change; with a link to the whole history.",
		"Die letzten gespeicherten Fassungen dieser (oder einer anderen) Seite: wann, von wem und der Hinweis zur Änderung; mit Link zum ganzen Verlauf.")
}
func (pagechanges) HasBody() bool { return false }
func (pagechanges) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "page", Labels: lm("Page (empty: this page)", "Seite (leer: diese Seite)"), Type: "custom:page"},
		{Name: "max", Labels: lm("At most (1–50)", "Höchstens (1–50)"), Type: "number", Default: "5"},
	}
}

func (pagechanges) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	id, err := pageOf(mc)
	if err != nil {
		return "", err
	}
	list, err := mc.Service.PageChanges(ctx, id, countParam(mc.Params["max"], 5, content.PageChangesMax))
	if err != nil {
		return "", fmt.Errorf("the page %d is not there or not visible to you", id)
	}
	var b strings.Builder
	b.WriteString(`<div class="ew-pagechanges">`)
	if len(list) == 0 {
		b.WriteString(`<p class="ew-muted">` + esc(tr(mc.Lang, "No saved versions yet.", "Noch keine gespeicherten Fassungen.")) + `</p>`)
	} else {
		b.WriteString(`<ol class="ew-pagechanges-list">`)
		for _, c := range list {
			who := c.Author
			if who == "" {
				who = tr(mc.Lang, "someone", "jemand")
			}
			fmt.Fprintf(&b, `<li><time datetime="%s">%s</time> · %s`, c.At.UTC().Format("2006-01-02T15:04:05Z"), c.At.UTC().Format("2006-01-02"), esc(who))
			if c.Lang != mc.Lang && c.Lang != "" {
				fmt.Fprintf(&b, ` <span class="ew-muted">(%s)</span>`, esc(c.Lang))
			}
			if note := strings.TrimSpace(c.Comment); note != "" {
				b.WriteString(`: ` + esc(note))
			}
			b.WriteString(`</li>`)
		}
		b.WriteString(`</ol>`)
	}
	fmt.Fprintf(&b, `<p><a href="/e/%d/history">%s</a></p></div>`, id, esc(tr(mc.Lang, "The whole history", "Der ganze Verlauf")))
	return b.String(), nil
}

// ---------------------------------------------------------------- contributors

type contributors struct{}

func (contributors) Name() string              { return "contributors" }
func (contributors) Labels() map[string]string { return lm("Contributors", "Mitwirkende") }
func (contributors) Description() map[string]string {
	return lm("The people who wrote this (or another) page, the latest first, with the number of their saved versions.",
		"Die Personen, die an dieser (oder einer anderen) Seite geschrieben haben, die letzten zuerst, mit der Zahl ihrer gespeicherten Fassungen.")
}
func (contributors) HasBody() bool { return false }
func (contributors) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "page", Labels: lm("Page (empty: this page)", "Seite (leer: diese Seite)"), Type: "custom:page"},
		{Name: "max", Labels: lm("At most (1–50)", "Höchstens (1–50)"), Type: "number", Default: "10"},
	}
}

func (contributors) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	id, err := pageOf(mc)
	if err != nil {
		return "", err
	}
	list, err := mc.Service.Contributors(ctx, id, countParam(mc.Params["max"], 10, content.ContributorsMax))
	if err != nil {
		return "", fmt.Errorf("the page %d is not there or not visible to you", id)
	}
	if len(list) == 0 {
		return `<p class="ew-muted">` + esc(tr(mc.Lang, "Nobody yet.", "Noch niemand.")) + `</p>`, nil
	}
	var b strings.Builder
	b.WriteString(`<ul class="ew-contributors">`)
	for _, c := range list {
		n := fmt.Sprintf(tr(mc.Lang, "%d versions", "%d Fassungen"), c.Versions)
		if c.Versions == 1 {
			n = tr(mc.Lang, "1 version", "1 Fassung")
		}
		fmt.Fprintf(&b, `<li><span class="ew-avatar" aria-hidden="true">%s</span> <span class="ew-contributor-name">%s</span> <span class="ew-muted">(%s)</span></li>`, esc(initial(c.Name)), esc(c.Name), esc(n))
	}
	b.WriteString(`</ul>`)
	return b.String(), nil
}

func initial(name string) string {
	for _, r := range strings.TrimSpace(name) {
		return strings.ToUpper(string(r))
	}
	return "?"
}
