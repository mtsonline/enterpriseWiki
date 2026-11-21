package core_macros

// Wiki blocks of priority A (plan of blocks and options, 3.2): include a
// page (or one excerpt of it), mark an excerpt, an anchor in the text, a
// page tree with a filter, a search box for one category and a button for a
// new page from a template. Everything is read with the permissions of the
// reader: what they may not see is not there for them.

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

var (
	anchorNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
)

func idParam(v string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(v, "#")), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// ---------------------------------------------------------------- excerpt

type excerpt struct{}

func (excerpt) Name() string              { return "excerpt" }
func (excerpt) Labels() map[string]string { return lm("Excerpt", "Auszug") }
func (excerpt) Description() map[string]string {
	return lm("Marks a part of this page that other pages can include (block \"Include page\").",
		"Markiert einen Teil dieser Seite, den andere Seiten einbinden können (Baustein „Seite einbinden“).")
}
func (excerpt) HasBody() bool { return true }
func (excerpt) Params() []content.MacroParam {
	return []content.MacroParam{{Name: "name", Labels: lm("Name (empty: the excerpt of the page)", "Name (leer: der Auszug der Seite)"), Type: "text"}}
}
func (excerpt) Render(_ context.Context, mc content.MacroContext) (string, error) {
	name := strings.TrimSpace(mc.Params["name"])
	if name != "" && !anchorNameRe.MatchString(name) {
		return "", apperr.BadRequest("the name: lowercase letters, digits and hyphens, starting with a letter")
	}
	return `<div class="ew-excerpt" data-excerpt="` + esc(name) + `">` + mc.Body + `</div>`, nil
}

// excerptOf finds the excerpt of a rendered page (by name; "" is the first
// one without a name). The HTML is rendered by the wiki itself.
func excerptOf(page, name string) (string, bool) {
	root, err := nethtml.Parse(strings.NewReader("<div>" + page + "</div>"))
	if err != nil {
		return "", false
	}
	var found *nethtml.Node
	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		if found != nil {
			return
		}
		if n.Type == nethtml.ElementNode && n.DataAtom == atom.Div {
			cls, ex, has := "", "", false
			for _, a := range n.Attr {
				switch a.Key {
				case "class":
					cls = a.Val
				case "data-excerpt":
					ex, has = a.Val, true
				}
			}
			if has && slices.Contains(strings.Fields(cls), "ew-excerpt") && ex == name {
				found = n
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if found == nil {
		return "", false
	}
	var b strings.Builder
	for c := found.FirstChild; c != nil; c = c.NextSibling {
		_ = nethtml.Render(&b, c)
	}
	return b.String(), true
}

// ---------------------------------------------------------------- include

type include struct{}

func (include) Name() string              { return "include" }
func (include) Labels() map[string]string { return lm("Include page", "Seite einbinden") }
func (include) Description() map[string]string {
	return lm("Shows another page (or one excerpt of it) here, always in its current state and with the permissions of the reader.",
		"Zeigt eine andere Seite (oder einen Auszug davon) hier, immer im aktuellen Stand und mit den Rechten der lesenden Person.")
}
func (include) HasBody() bool { return false }
func (include) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "page", Labels: lm("Page", "Seite"), Type: "custom:page", Required: true},
		{Name: "excerpt", Labels: lm("Only the excerpt", "Nur den Auszug"), Type: "bool"},
		{Name: "name", Labels: lm("Name of the excerpt (empty: the excerpt without a name)", "Name des Auszugs (leer: der Auszug ohne Namen)"), Type: "text"},
		{Name: "title", Labels: lm("Show the title with a link", "Titel mit Link zeigen"), Type: "bool", Default: "true"},
	}
}

func (include) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	id := idParam(mc.Params["page"])
	if id == 0 {
		return "", apperr.BadRequest("choose the page to include (its number)")
	}
	if id == mc.ElementID {
		return "", apperr.BadRequest("a page cannot include itself")
	}
	if !content.SpendInclude(ctx) {
		return "", apperr.BadRequest("this page includes too many pages; the others are not shown")
	}
	// with the permissions of the reader; a page they may not see is not
	// there for them (the same answer as for a page that does not exist).
	// Pages that include pages stop at the depth of the content service.
	notThere := apperr.BadRequest("the page %d is not there or not visible to you", id)
	cat, err := mc.Service.ElementCategory(ctx, id, acl.View)
	if err != nil {
		return "", notThere
	}
	// content never leaves its classification: what is classified stricter
	// than this page (or its category) is not shown in it, not even its
	// title. Otherwise it would travel on in e-mails, prints and exports of
	// this page as if it were as open as the page.
	if mc.Service.Classification(ctx, id, cat) > mc.Service.Classification(ctx, mc.ElementID, mc.CategoryID) {
		return fmt.Sprintf(`<div class="ew-include" data-element="%d"><p class="ew-notice ew-notice--info">%s <a href="/e/%d">%s</a></p></div>`, id,
			esc(pick(lm("Included content: it is classified stricter than this page, so it is not shown here.", "Eingebundener Inhalt: Er ist strenger eingestuft als diese Seite und wird deshalb hier nicht gezeigt."), mc.Lang)),
			id, esc(pick(lm("Open it", "Öffnen"), mc.Lang))), nil
	}
	v, err := mc.Service.GetElement(ctx, id, mc.Lang, false)
	if err != nil {
		return "", notThere
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ew-include" data-element="%d">`, id)
	if mc.Params["title"] != "false" {
		fmt.Fprintf(&b, `<p class="ew-include-title"><a href="/e/%d">%s</a></p>`, id, esc(v.Title))
	}
	body := v.HTML
	if mc.Params["excerpt"] == "true" {
		name := strings.TrimSpace(mc.Params["name"])
		ex, ok := excerptOf(v.HTML, name)
		if !ok {
			return "", apperr.BadRequest("the page %q has no excerpt %q", v.Title, name)
		}
		body = ex
	}
	b.WriteString(`<div class="ew-include-body">` + body + `</div></div>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- anchor

type anchor struct{}

func (anchor) Name() string              { return "anchor" }
func (anchor) Labels() map[string]string { return lm("Anchor", "Anker") }
func (anchor) Description() map[string]string {
	return lm("A point in the text that links can jump to (#name).", "Eine Stelle im Text, zu der Links springen können (#name).")
}
func (anchor) HasBody() bool { return false }
func (anchor) Inline() bool  { return true }
func (anchor) Params() []content.MacroParam {
	return []content.MacroParam{{Name: "name", Labels: lm("Name", "Name"), Type: "text", Required: true}}
}
func (anchor) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	name := strings.TrimSpace(mc.Params["name"])
	if err := content.CheckAnchor(name); err != nil {
		return "", apperr.BadRequest("the name of the anchor: %v", err)
	}
	if !content.ClaimID(ctx, name) {
		return "", apperr.BadRequest("the anchor %q is on this page twice; give each anchor its own name", name)
	}
	return `<span class="ew-anchor" id="` + esc(name) + `"></span>`, nil
}

// ---------------------------------------------------------------- page tree

type pagetree struct{}

const (
	treeMaxCategories = 300
	treeMaxPages      = 200
)

func (pagetree) Name() string              { return "pagetree" }
func (pagetree) Labels() map[string]string { return lm("Page tree", "Seitenbaum") }
func (pagetree) Description() map[string]string {
	return lm("The subcategories and pages of a category as a tree to open, with a filter.",
		"Die Unterkategorien und Seiten einer Kategorie als Baum zum Aufklappen, mit Filter.")
}
func (pagetree) HasBody() bool { return false }
func (pagetree) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "category", Labels: lm("Category (empty: this one)", "Kategorie (leer: diese)"), Type: "custom:category"},
		{Name: "depth", Labels: lm("Levels", "Ebenen"), Type: "select", Options: []string{"1", "2", "3", "4"}, Default: "3"},
		{Name: "pages", Labels: lm("Show pages", "Seiten zeigen"), Type: "bool", Default: "true"},
		{Name: "open", Labels: lm("All open", "Alles aufgeklappt"), Type: "bool"},
		{Name: "search", Labels: lm("Filter field", "Filterfeld"), Type: "bool", Default: "true"},
	}
}

func (pagetree) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	root := mc.CategoryID
	if v := idParam(mc.Params["category"]); v > 0 {
		root = v
	}
	if root == 0 {
		return "", apperr.BadRequest("choose a category")
	}
	depth := 3
	if n, err := strconv.Atoi(mc.Params["depth"]); err == nil && n >= 1 && n <= 4 {
		depth = n
	}
	// a few queries for the whole tree (never one per category): the
	// categories the reader may see, then all their pages at once
	scope, err := mc.Service.Scope(ctx, root, true)
	if err != nil {
		return "", apperr.BadRequest("the category %d is not there or not visible to you", root)
	}
	cats, err := mc.Service.CategoriesByID(ctx, scope)
	if err != nil {
		return "", err
	}
	byParent := map[int64][]content.Category{}
	level := map[int64]int{root: 0}
	shown := []int64{root}
	more := false
	for _, c := range cats { // in tree order (by path)
		if c.ID == root || c.ParentID == nil {
			continue
		}
		l, ok := level[*c.ParentID]
		if !ok || l+1 > depth-1 || c.Hidden {
			continue
		}
		if len(shown) >= treeMaxCategories {
			more = true
			break
		}
		level[c.ID] = l + 1
		byParent[*c.ParentID] = append(byParent[*c.ParentID], c)
		shown = append(shown, c.ID)
	}
	for k := range byParent {
		slices.SortStableFunc(byParent[k], func(a, b content.Category) int { return a.Position - b.Position })
	}
	pages := map[int64][]content.ListedElement{}
	if mc.Params["pages"] != "false" {
		els, total, err := mc.Service.ListIn(ctx, content.ListOptions{Categories: shown, Lang: mc.Lang, Order: "position", Limit: treeMaxPages, SummaryLen: 1, Exclude: mc.ElementID})
		if err == nil {
			for _, e := range els {
				pages[e.CategoryID] = append(pages[e.CategoryID], e)
			}
			more = more || total > len(els)
		}
	}
	open := mc.Params["open"] == "true"
	var b strings.Builder
	// its own name: the navigation of the wiki is called page tree too
	label := pick(lm("Pages of %s", "Seiten von %s"), mc.Lang)
	for _, c := range cats {
		if c.ID == root {
			label = fmt.Sprintf(label, pick(c.Titles, mc.Lang))
		}
	}
	fmt.Fprintf(&b, `<nav class="ew-pagetree" aria-label="%s">`, esc(label))
	if mc.Params["search"] != "false" {
		// filled by the browser (a filter needs script; without it the tree is complete)
		fmt.Fprintf(&b, `<div class="ew-pagetree-filter" data-ew-widget="core.macros.pagetree"></div>`)
	}
	var list func(id int64)
	list = func(id int64) {
		subs, ps := byParent[id], pages[id]
		if len(subs) == 0 && len(ps) == 0 {
			return
		}
		b.WriteString(`<ul>`)
		for _, c := range subs {
			o := ""
			if open {
				o = " open"
			}
			title := esc(pick(c.Titles, mc.Lang))
			fmt.Fprintf(&b, `<li class="ew-pagetree-cat"><a href="/c/%d">%s</a>`, c.ID, title)
			// a link inside the summary would be a control inside a control:
			// the link opens the category, the summary its contents
			if n := len(byParent[c.ID]) + len(pages[c.ID]); n > 0 {
				fmt.Fprintf(&b, `<details%s><summary>%s<span class="sr-only">: %s</span></summary>`, o, esc(fmt.Sprintf(pick(lm("Contents (%d)", "Inhalt (%d)"), mc.Lang), n)), title)
				list(c.ID)
				b.WriteString(`</details>`)
			}
			b.WriteString(`</li>`)
		}
		for _, e := range ps {
			fmt.Fprintf(&b, `<li class="ew-pagetree-page"><a href="/e/%d">%s</a></li>`, e.ID, esc(e.Title))
		}
		b.WriteString(`</ul>`)
	}
	list(root)
	if more {
		fmt.Fprintf(&b, `<p class="ew-help"><a href="/c/%d">%s</a></p>`, root, esc(pick(lm("Not everything is shown here: open the category", "Hier ist nicht alles zu sehen: die Kategorie öffnen"), mc.Lang)))
	}
	b.WriteString(`</nav>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- search box

type searchbox struct{}

func (searchbox) Name() string              { return "searchbox" }
func (searchbox) Labels() map[string]string { return lm("Search box", "Suchfeld") }
func (searchbox) Description() map[string]string {
	return lm("A search field, in the whole wiki or in one category and its subcategories.",
		"Ein Suchfeld, im ganzen Wiki oder in einer Kategorie und ihren Unterkategorien.")
}
func (searchbox) HasBody() bool { return false }
func (searchbox) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "scope", Labels: lm("Search in", "Suchen in"), Type: "select", Options: []string{"category", "all"}, Default: "category",
			OptionLabels: map[string]map[string]string{"category": lm("This category (or the one below)", "Dieser Kategorie (oder der unten genannten)"), "all": lm("The whole wiki", "Dem ganzen Wiki")}},
		{Name: "category", Labels: lm("Category (empty: this one)", "Kategorie (leer: diese)"), Type: "custom:category"},
		{Name: "label", Labels: lm("Label", "Beschriftung"), Type: "text"},
		{Name: "placeholder", Labels: lm("Example in the field", "Beispiel im Feld"), Type: "text"},
		{Name: "live", Labels: lm("Show the first results while typing", "Erste Treffer schon beim Tippen zeigen"), Type: "bool", Default: "true"},
	}
}

func (searchbox) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	cat := int64(0)
	if mc.Params["scope"] != "all" {
		cat = mc.CategoryID
		if v := idParam(mc.Params["category"]); v > 0 {
			cat = v
		}
		if cat > 0 && mc.Service != nil {
			if _, err := mc.Service.Scope(ctx, cat, false); err != nil {
				return "", apperr.BadRequest("the category %d is not there or not visible to you", cat)
			}
		}
	}
	label := strings.TrimSpace(mc.Params["label"])
	if label == "" {
		label = pick(lm("Search", "Suchen"), mc.Lang)
	}
	// unique on the page, also when a page with a search box is included twice
	id := fmt.Sprintf("ew-searchbox-%d-%d", mc.ElementID, mc.Index)
	for n, base := 2, id; !content.ClaimID(ctx, id) && n < 100; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	var b strings.Builder
	// a plain form: works without script, the search page shows the results
	live := ""
	if mc.Params["live"] != "false" {
		// the browser shows the first results under the field (without script: the search page)
		live = ` data-ew-widget="core.macros.searchlive"`
	}
	fmt.Fprintf(&b, `<form class="ew-searchbox" role="search" action="/search" method="get"%s><label for="%s">%s</label><span class="ew-searchbox-row"><input id="%s" class="ew-input" type="search" name="q" required placeholder="%s">`,
		live, id, esc(label), id, esc(mc.Params["placeholder"]))
	if cat > 0 {
		fmt.Fprintf(&b, `<input type="hidden" name="category" value="%d">`, cat)
	}
	fmt.Fprintf(&b, `<button class="ew-btn ew-btn--primary" type="submit">%s</button></span></form>`, esc(pick(lm("Search", "Suchen"), mc.Lang)))
	return b.String(), nil
}

// ---------------------------------------------------------------- new page from a template

type newpage struct{}

func (newpage) Name() string { return "newpage" }
func (newpage) Labels() map[string]string {
	return lm("New page from a template", "Seite aus Vorlage anlegen")
}
func (newpage) Description() map[string]string {
	return lm("A button that opens a new page in a category, filled from a page template. Only people who may create pages there see it.",
		"Ein Knopf, der eine neue Seite in einer Kategorie öffnet, gefüllt aus einer Seitenvorlage. Nur wer dort Seiten anlegen darf, sieht ihn.")
}
func (newpage) HasBody() bool { return false }
func (newpage) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "template", Labels: lm("Page template", "Seitenvorlage"), Type: "custom:template", Required: true},
		{Name: "category", Labels: lm("Category (empty: this one)", "Kategorie (leer: diese)"), Type: "custom:category"},
		{Name: "text", Labels: lm("Text of the button", "Text des Knopfs"), Type: "text"},
	}
}

func (newpage) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	cat := mc.CategoryID
	if v := idParam(mc.Params["category"]); v > 0 {
		cat = v
	}
	tid := idParam(mc.Params["template"])
	if cat == 0 || tid == 0 {
		return "", apperr.BadRequest("choose a category and a page template")
	}
	// only for people who may create pages there and may use the template
	c, err := mc.Service.GetCategory(ctx, cat)
	if err != nil || !slices.Contains(c.Permissions, "create") {
		return "", nil
	}
	tpls, err := mc.Service.TemplatesOfKind(ctx, "page", "", tid, cat)
	if err != nil || len(tpls) == 0 {
		return "", nil
	}
	text := strings.TrimSpace(mc.Params["text"])
	if text == "" {
		text = fmt.Sprintf(pick(lm("New page: %s", "Neue Seite: %s"), mc.Lang), tpls[0].Name)
	}
	href := html.EscapeString(fmt.Sprintf("/c/%d/new?type=%s&template=%d", cat, url.QueryEscape(tpls[0].Type), tid))
	return `<p class="ewl-cta-wrap"><a class="ewl-cta ewl-cta--primary ew-btn ew-btn--primary" href="` + href + `">` + esc(text) + `</a></p>`, nil
}
