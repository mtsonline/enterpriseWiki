// Package core_properties adds structured properties to pages (owner,
// status, review date, …): shown as a table on the page and listed by the
// macro "properties" (page properties report) for the pages of a category.
// Reading needs the View permission of the page, changing the Edit
// permission.
package core_properties

import (
	"context"
	"embed"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// Limits.
const (
	MaxProps    = 50
	MaxKeyLen   = 80
	MaxValueLen = 1000
)

// Types of properties.
var Types = []string{"text", "date", "number", "url", "status", "user"}

// Plugin is the page properties plugin.
type Plugin struct{ host *sdk.Host }

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Property is one property of a page.
type Property struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

func validate(list []Property) ([]Property, error) {
	if len(list) > MaxProps {
		return nil, apperr.BadRequest("at most %d properties", MaxProps)
	}
	seen := map[string]bool{}
	out := make([]Property, 0, len(list))
	for _, x := range list {
		x.Key, x.Value = strings.TrimSpace(x.Key), strings.TrimSpace(x.Value)
		if x.Key == "" {
			continue
		}
		if utf8.RuneCountInString(x.Key) > MaxKeyLen || utf8.RuneCountInString(x.Value) > MaxValueLen {
			return nil, apperr.BadRequest("%s: too long", x.Key)
		}
		if x.Type == "" {
			x.Type = "text"
		}
		ok := false
		for _, t := range Types {
			ok = ok || t == x.Type
		}
		if !ok {
			return nil, apperr.BadRequest("%s: unknown type %q", x.Key, x.Type)
		}
		switch x.Type {
		case "date":
			if x.Value != "" {
				if _, err := time.Parse("2006-01-02", x.Value); err != nil {
					return nil, apperr.BadRequest("%s: date as YYYY-MM-DD", x.Key)
				}
			}
		case "number":
			if x.Value != "" {
				if _, err := strconv.ParseFloat(strings.ReplaceAll(x.Value, ",", "."), 64); err != nil {
					return nil, apperr.BadRequest("%s: not a number", x.Key)
				}
			}
		case "url":
			if x.Value != "" {
				u, err := url.Parse(x.Value)
				if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto" && !strings.HasPrefix(x.Value, "/")) {
					return nil, apperr.BadRequest("%s: use an http(s) address or a path", x.Key)
				}
			}
		}
		k := strings.ToLower(x.Key)
		if seen[k] {
			return nil, apperr.BadRequest("%s: duplicate property", x.Key)
		}
		seen[k] = true
		out = append(out, x)
	}
	return out, nil
}

func (p *Plugin) props(ctx context.Context, ids []int64) (map[int64][]Property, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT element_id, key, value, type FROM plugin_properties WHERE element_id = ANY($1) ORDER BY element_id, position`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Property{}
	for rows.Next() {
		var id int64
		var x Property
		if err := rows.Scan(&id, &x.Key, &x.Value, &x.Type); err != nil {
			return nil, err
		}
		out[id] = append(out[id], x)
	}
	return out, rows.Err()
}

// Setup implements sdk.SetupPlugin.
func (p *Plugin) Setup(host *sdk.Host) error {
	p.host = host
	host.RegisterMacro(report{p})

	// GET /elements/{id} – properties of a page
	host.Handle("GET", "/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		cat, err := host.Content.ElementCategory(ctx, id, acl.View)
		if err != nil {
			return nil, err
		}
		m, err := p.props(ctx, []int64{id})
		if err != nil {
			return nil, err
		}
		items := m[id]
		if items == nil {
			items = []Property{}
		}
		perms, _ := host.ACL.Perms(ctx, identity.From(ctx), cat)
		return map[string]any{"items": items, "can_edit": perms&acl.Edit != 0, "types": Types}, nil
	})
	// PUT /elements/{id} {"items": [{"key": "Owner", "value": "…", "type": "user"}]} – replace all
	host.Handle("PUT", "/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		if _, err := host.Content.ElementCategory(ctx, id, acl.Edit); err != nil {
			return nil, err
		}
		var in struct {
			Items []Property `json:"items"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		items, err := validate(in.Items)
		if err != nil {
			return nil, err
		}
		before, err := p.props(ctx, []int64{id})
		if err != nil {
			return nil, err
		}
		err = pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `DELETE FROM plugin_properties WHERE element_id=$1`, id); err != nil {
				return err
			}
			for i, x := range items {
				if _, err := tx.Exec(ctx, `INSERT INTO plugin_properties(element_id, key, value, type, position) VALUES ($1,$2,$3,$4,$5)`, id, x.Key, x.Value, x.Type, i); err != nil {
					return err
				}
			}
			return audit.Log(ctx, tx, "element.properties", "element", id, map[string]any{"properties": before[id]}, map[string]any{"properties": items})
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items, "can_edit": true, "types": Types}, nil
	})
	// GET /keys?category=ID – keys used in a category and below (suggestions)
	host.Handle("GET", "/keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		cat, _ := strconv.ParseInt(r.URL.Query().Get("category"), 10, 64)
		if cat > 0 {
			if err := host.Content.Require(ctx, cat, acl.View); err != nil {
				return nil, err
			}
		}
		rows, err := host.DB.Query(ctx, `SELECT p.key, min(p.type), count(*) FROM plugin_properties p JOIN elements e ON e.id = p.element_id
			WHERE e.deleted_at IS NULL AND ($1 = 0 OR e.category_id IN (SELECT c.id FROM categories c, categories t WHERE t.id=$1 AND c.path <@ t.path))
			GROUP BY p.key ORDER BY count(*) DESC, p.key LIMIT 100`, cat)
		if err != nil {
			return nil, err
		}
		type key struct {
			Key   string `json:"key"`
			Type  string `json:"type"`
			Count int    `json:"count"`
		}
		return pgx.CollectRows(rows, pgx.RowToStructByPos[key])
	})
	return nil
}

// ---------------------------------------------------------------- report macro

type report struct{ p *Plugin }

func lm(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }

func (report) Name() string { return "properties" }
func (report) Labels() map[string]string {
	return lm("Page properties report", "Seiteneigenschaften-Bericht")
}
func (report) Description() map[string]string {
	return lm("Lists the pages of a category with their properties as a table.", "Listet die Seiten einer Kategorie mit ihren Eigenschaften als Tabelle.")
}
func (report) HasBody() bool { return false }
func (report) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "category", Labels: lm("Category id (empty = this category)", "Kategorie-ID (leer = diese Kategorie)"), Type: "number"},
		{Name: "recursive", Labels: lm("Include subcategories", "Mit Unterkategorien"), Type: "bool", Default: "true"},
		{Name: "columns", Labels: lm("Columns (properties, comma separated; empty = all)", "Spalten (Eigenschaften, mit Komma getrennt; leer = alle)"), Type: "text"},
		{Name: "filter", Labels: lm("Filter, e.g. Status=Active", "Filter, z. B. Status=Aktiv"), Type: "text"},
		{Name: "tags", Labels: lm("Only pages with these tags (comma separated)", "Nur Seiten mit diesen Tags (mit Komma getrennt)"), Type: "text"},
		{Name: "sort", Labels: lm("Sort by (property or title)", "Sortieren nach (Eigenschaft oder Titel)"), Type: "text"},
		{Name: "limit", Labels: lm("Maximum number of pages", "Höchstzahl der Seiten"), Type: "number", Default: "100"},
	}
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func pickLang(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	if v := m["en"]; v != "" {
		return v
	}
	return ""
}

// format renders a value by its type (escaped).
func format(x Property, lang string) string {
	v := html.EscapeString(x.Value)
	switch x.Type {
	case "date":
		if t, err := time.Parse("2006-01-02", x.Value); err == nil {
			f := "2006-01-02"
			if lang == "de" {
				f = "02.01.2006"
			}
			return fmt.Sprintf(`<time datetime="%s">%s</time>`, v, t.Format(f))
		}
	case "url":
		if x.Value != "" {
			return fmt.Sprintf(`<a href="%s" rel="noopener">%s</a>`, v, v)
		}
	case "status":
		if x.Value != "" {
			return fmt.Sprintf(`<span class="ew-badge ewprop-status">%s</span>`, v)
		}
	}
	return v
}

func (r report) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	cat := mc.CategoryID
	if v, err := strconv.ParseInt(mc.Params["category"], 10, 64); err == nil && v > 0 {
		cat = v
	}
	if cat == 0 {
		return "", nil
	}
	limit := 100
	if v, err := strconv.Atoi(mc.Params["limit"]); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	found, _, err := mc.Service.FindElements(ctx, content.FindOptions{Category: cat, Recursive: mc.Params["recursive"] != "false", Lang: mc.Lang, Limit: 500, Sort: "title"})
	if err != nil {
		return "", err
	}
	tags := splitList(strings.ToLower(mc.Params["tags"]))
	var ids []int64
	byID := map[int64]content.FoundElement{}
	for _, e := range found {
		if e.ID == mc.ElementID {
			continue // not the page with the report
		}
		ok := true
		for _, t := range tags {
			has := false
			for _, et := range e.Tags {
				has = has || strings.EqualFold(et, t)
			}
			ok = ok && has
		}
		if ok {
			ids = append(ids, e.ID)
			byID[e.ID] = e
		}
	}
	props, err := r.p.props(ctx, ids)
	if err != nil {
		return "", err
	}
	// filter Key=Value (several separated by ;)
	var filters [][2]string
	for _, f := range strings.Split(mc.Params["filter"], ";") {
		if k, v, ok := strings.Cut(f, "="); ok && strings.TrimSpace(k) != "" {
			filters = append(filters, [2]string{strings.ToLower(strings.TrimSpace(k)), strings.ToLower(strings.TrimSpace(v))})
		}
	}
	value := func(id int64, key string) (Property, bool) {
		for _, x := range props[id] {
			if strings.EqualFold(x.Key, key) {
				return x, true
			}
		}
		return Property{}, false
	}
	var rows []int64
	for _, id := range ids {
		if len(props[id]) == 0 {
			continue // pages without properties are not listed
		}
		ok := true
		for _, f := range filters {
			x, has := value(id, f[0])
			ok = ok && has && strings.ToLower(x.Value) == f[1]
		}
		if ok {
			rows = append(rows, id)
		}
	}
	cols := splitList(mc.Params["columns"])
	if len(cols) == 0 {
		seen := map[string]bool{}
		for _, id := range rows {
			for _, x := range props[id] {
				if k := strings.ToLower(x.Key); !seen[k] {
					seen[k] = true
					cols = append(cols, x.Key)
				}
			}
		}
	}
	if s := strings.TrimSpace(mc.Params["sort"]); s != "" && !strings.EqualFold(s, "title") {
		sort.SliceStable(rows, func(i, j int) bool {
			a, _ := value(rows[i], s)
			b, _ := value(rows[j], s)
			if a.Type == "number" {
				fa, _ := strconv.ParseFloat(strings.ReplaceAll(a.Value, ",", "."), 64)
				fb, _ := strconv.ParseFloat(strings.ReplaceAll(b.Value, ",", "."), 64)
				return fa < fb
			}
			return strings.ToLower(a.Value) < strings.ToLower(b.Value)
		})
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	label := lm("Page properties report", "Seiteneigenschaften-Bericht")
	if len(rows) == 0 {
		return fmt.Sprintf(`<p class="ew-muted">%s</p>`, html.EscapeString(pickLang(lm("No pages with matching properties.", "Keine Seiten mit passenden Eigenschaften."), mc.Lang))), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewprop-report"><table class="ew-table"><caption class="sr-only">%s</caption><thead><tr><th scope="col">%s</th>`,
		html.EscapeString(pickLang(label, mc.Lang)), html.EscapeString(pickLang(lm("Page", "Seite"), mc.Lang)))
	for _, c := range cols {
		fmt.Fprintf(&b, `<th scope="col">%s</th>`, html.EscapeString(c))
	}
	b.WriteString(`</tr></thead><tbody>`)
	for _, id := range rows {
		e := byID[id]
		href := mc.Service.Path(ctx, "element", id, mc.Lang)
		if href == "" {
			href = fmt.Sprintf("/e/%d", id)
		}
		fmt.Fprintf(&b, `<tr><th scope="row"><a href="%s">%s</a></th>`, html.EscapeString(href), html.EscapeString(e.Title))
		for _, c := range cols {
			x, _ := value(id, c)
			fmt.Fprintf(&b, `<td>%s</td>`, format(x, mc.Lang))
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table></div>`)
	return b.String(), nil
}
