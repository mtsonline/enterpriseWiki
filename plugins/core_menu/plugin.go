// Package core_menu provides menus. A menu is placed at a position offered
// by the template (header, sidebar, footer, ...) and contains nested items
// that point to a category, an element of any type, a URL or another menu
// (a submenu, for multilevel menus). Category and element items are only
// shown to users who may view them.
package core_menu

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
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

// Plugin is the menu plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { sub, _ := fs.Sub(migrations, "migrations"); return sub }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { sub, _ := fs.Sub(locales, "locales"); return sub }

// Item is a menu entry. Type is the kind of target:
//
//	url       a free link (URL)
//	category  a category (CategoryID), shown only to users who may view it
//	element   an element of any type (ElementID), same permission rule
//	menu      another menu (MenuID): its items become this item's submenu
//	heading   no target, only a label for the children (a group)
//
// The label may be empty for category, element and menu targets: the title
// of the target is shown then (in the user's language).
type Item struct {
	Label      map[string]string `json:"label"`
	Type       string            `json:"type,omitempty"`
	URL        string            `json:"url,omitempty"`
	CategoryID int64             `json:"category_id,omitempty"`
	ElementID  int64             `json:"element_id,omitempty"`
	MenuID     int64             `json:"menu_id,omitempty"`
	NewTab     bool              `json:"new_tab,omitempty"`
	Children   []Item            `json:"children,omitempty"`
	// resolved for display (GET /menus without all=1)
	Href    string `json:"href,omitempty"`
	Submenu bool   `json:"submenu,omitempty"`
}

// Menu is a menu. Menus with an empty location are not placed anywhere;
// they are used as submenus of other menus.
type Menu struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Location  string    `json:"location"`
	Position  int       `json:"position"`
	Items     []Item    `json:"items"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MaxDepth is the deepest level shown (submenus included).
const MaxDepth = 6

var locRe = regexp.MustCompile(`^([a-z][a-z0-9_-]{0,40})?$`)

func hasLabel(l map[string]string) bool {
	for _, v := range l {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// normalize infers the type of older items and checks the target fields.
func normalize(items []Item, depth int) error {
	if depth > 4 {
		return apperr.BadRequest("menus can be nested at most 4 levels (use a submenu for deeper menus)")
	}
	for i := range items {
		it := &items[i]
		if it.Type == "" {
			switch {
			case it.CategoryID != 0:
				it.Type = "category"
			case it.ElementID != 0:
				it.Type = "element"
			case it.MenuID != 0:
				it.Type = "menu"
			case it.URL != "":
				it.Type = "url"
			default:
				it.Type = "heading"
			}
		}
		it.Href, it.Submenu = "", false
		n := i + 1
		switch it.Type {
		case "url":
			u, err := url.Parse(it.URL)
			if it.URL == "" || err != nil || !(u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto" || (u.Scheme == "" && strings.HasPrefix(it.URL, "/") && !strings.HasPrefix(it.URL, "//"))) {
				return apperr.BadRequest("item %d: invalid menu URL %q (allowed: http, https, mailto or /path)", n, it.URL)
			}
			it.CategoryID, it.ElementID, it.MenuID = 0, 0, 0
		case "category":
			if it.CategoryID <= 0 {
				return apperr.BadRequest("item %d: choose a category", n)
			}
			it.URL, it.ElementID, it.MenuID = "", 0, 0
		case "element":
			if it.ElementID <= 0 {
				return apperr.BadRequest("item %d: choose an element", n)
			}
			it.URL, it.CategoryID, it.MenuID = "", 0, 0
		case "menu":
			if it.MenuID <= 0 {
				return apperr.BadRequest("item %d: choose a menu", n)
			}
			it.URL, it.CategoryID, it.ElementID = "", 0, 0
		case "heading":
			it.URL, it.CategoryID, it.ElementID, it.MenuID = "", 0, 0, 0
		default:
			return apperr.BadRequest("item %d: unknown type %q (url, category, element, menu, heading)", n, it.Type)
		}
		if (it.Type == "url" || it.Type == "heading") && !hasLabel(it.Label) {
			return apperr.BadRequest("item %d: a label is required", n)
		}
		if it.Label == nil {
			it.Label = map[string]string{}
		}
		if err := normalize(it.Children, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func menuRefs(items []Item, out map[int64]bool) {
	for _, it := range items {
		if it.Type == "menu" || (it.Type == "" && it.MenuID != 0) {
			out[it.MenuID] = true
		}
		menuRefs(it.Children, out)
	}
}

// checkSubmenus verifies that referenced menus exist and that no cycle
// arises (a menu that contains itself).
func checkSubmenus(ctx context.Context, q querier, id int64, items []Item) error {
	refs := map[int64]bool{}
	menuRefs(items, refs)
	if len(refs) == 0 {
		return nil
	}
	all, err := list(ctx, q, "")
	if err != nil {
		return err
	}
	graph := map[int64]map[int64]bool{}
	for _, m := range all {
		r := map[int64]bool{}
		menuRefs(m.Items, r)
		graph[m.ID] = r
	}
	graph[id] = refs
	for r := range refs {
		if _, ok := graph[r]; !ok {
			return apperr.BadRequest("menu %d does not exist", r)
		}
	}
	// depth-first search from the saved menu
	var visit func(n int64, path map[int64]bool) bool
	visit = func(n int64, path map[int64]bool) bool {
		for c := range graph[n] {
			if c == id || path[c] {
				return true
			}
			path[c] = true
			if visit(c, path) {
				return true
			}
			delete(path, c)
		}
		return false
	}
	if id == 0 {
		id = -1 // a new menu cannot be referenced yet
	}
	if visit(id, map[int64]bool{}) {
		return apperr.BadRequest("a menu cannot contain itself (directly or through submenus)")
	}
	return nil
}

// resolver turns stored items into what the caller may see.
type resolver struct {
	ctx      context.Context
	acl      *acl.Service
	menus    map[int64]Menu
	cats     map[int64]map[string]string
	short    map[string]string // "c12" / "e5" -> short URL
	elements map[int64]struct {
		cat    int64
		titles map[string]string
	}
}

func collect(items []Item, cats, els map[int64]bool) {
	for _, it := range items {
		if it.CategoryID != 0 {
			cats[it.CategoryID] = true
		}
		if it.ElementID != 0 {
			els[it.ElementID] = true
		}
		collect(it.Children, cats, els)
	}
}

func newResolver(ctx context.Context, hs *sdk.Host, all []Menu) (*resolver, error) {
	r := &resolver{ctx: ctx, acl: hs.ACL, menus: map[int64]Menu{}, cats: map[int64]map[string]string{}, short: map[string]string{}, elements: map[int64]struct {
		cat    int64
		titles map[string]string
	}{}}
	cats, els := map[int64]bool{}, map[int64]bool{}
	for _, m := range all {
		r.menus[m.ID] = m
		collect(m.Items, cats, els)
	}
	ids := func(m map[int64]bool) []int64 {
		out := make([]int64, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		return out
	}
	if len(els) > 0 {
		rows, err := hs.DB.Query(ctx, `SELECT id, category_id, titles, COALESCE(short_url, '') FROM elements WHERE id = ANY($1) AND deleted_at IS NULL`, ids(els))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, cat int64
			var t map[string]string
			var short string
			if err := rows.Scan(&id, &cat, &t, &short); err != nil {
				rows.Close()
				return nil, err
			}
			r.elements[id] = struct {
				cat    int64
				titles map[string]string
			}{cat, t}
			if short != "" {
				r.short[fmt.Sprintf("e%d", id)] = short
			}
		}
		rows.Close()
	}
	if len(cats) > 0 {
		rows, err := hs.DB.Query(ctx, `SELECT id, titles, COALESCE(short_url, '') FROM categories WHERE id = ANY($1) AND deleted_at IS NULL`, ids(cats))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var t map[string]string
			var short string
			if err := rows.Scan(&id, &t, &short); err != nil {
				rows.Close()
				return nil, err
			}
			r.cats[id] = t
			if short != "" {
				r.short[fmt.Sprintf("c%d", id)] = short
			}
		}
		rows.Close()
	}
	return r, nil
}

// href is the short URL of a target or /c/ID, /e/ID.
func (r *resolver) href(kind string, id int64) string {
	if s := r.short[fmt.Sprintf("%s%d", kind, id)]; s != "" {
		return "/" + s
	}
	return fmt.Sprintf("/%s/%d", kind, id)
}

func (r *resolver) can(cat int64) bool {
	return r.acl.Can(r.ctx, identity.From(r.ctx), cat, acl.View)
}

// resolve filters items by permission, fills href and missing labels and
// expands submenus (visited prevents loops, depth limits the size).
func (r *resolver) resolve(items []Item, depth int, visited map[int64]bool) []Item {
	out := []Item{}
	if depth > MaxDepth {
		return out
	}
	for _, it := range items {
		var title map[string]string
		children := r.resolve(it.Children, depth+1, visited)
		switch {
		case it.Type == "category" || (it.Type == "" && it.CategoryID != 0):
			t, ok := r.cats[it.CategoryID]
			if !ok || !r.can(it.CategoryID) {
				continue
			}
			it.Href, title = r.href("c", it.CategoryID), t
		case it.Type == "element" || (it.Type == "" && it.ElementID != 0):
			e, ok := r.elements[it.ElementID]
			if !ok || !r.can(e.cat) {
				continue
			}
			it.Href, title = r.href("e", it.ElementID), e.titles
		case it.Type == "menu" || (it.Type == "" && it.MenuID != 0):
			m, ok := r.menus[it.MenuID]
			if !ok || visited[it.MenuID] {
				continue
			}
			visited[it.MenuID] = true
			sub := r.resolve(m.Items, depth+1, visited)
			delete(visited, it.MenuID)
			children = append(sub, children...)
			if len(children) == 0 {
				continue
			}
			it.Submenu = true
			title = map[string]string{"*": m.Name}
		case it.URL != "":
			it.Href = it.URL
		default: // heading
			if len(children) == 0 {
				continue
			}
		}
		if !hasLabel(it.Label) {
			if n, ok := title["*"]; ok {
				it.Label = map[string]string{"*": n}
			} else {
				it.Label = title
			}
		}
		it.Children = children
		if len(it.Children) == 0 {
			it.Children = nil
		}
		out = append(out, it)
	}
	return out
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func list(ctx context.Context, q querier, location string) ([]Menu, error) {
	rows, err := q.Query(ctx, `SELECT id, name, location, position, items, updated_at FROM plugin_menus
		WHERE $1 = '*' OR location = $1 OR ($1 = '' AND location <> '') ORDER BY location, position, id`, location)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Menu, error) {
		var m Menu
		return m, r.Scan(&m.ID, &m.Name, &m.Location, &m.Position, &m.Items, &m.UpdatedAt)
	})
}

// Setup registers the API.
func (p *Plugin) Setup(hs *sdk.Host) error {
	// Public: placed menus (of one location), resolved for the caller:
	// permissions applied, labels and links filled, submenus expanded.
	// all=1 (admins): every menu as stored, including submenu-only menus.
	hs.Handle("GET", "/menus", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		all, err := list(ctx, hs.DB, "*")
		if err != nil {
			return nil, err
		}
		if r.URL.Query().Get("all") == "1" {
			if err := sdk.RequireAdmin(r); err != nil {
				return nil, err
			}
			for i := range all {
				_ = normalize(all[i].Items, 1) // fills the type of older items
			}
			return all, nil
		}
		res, err := newResolver(ctx, hs, all)
		if err != nil {
			return nil, err
		}
		loc := r.URL.Query().Get("location")
		out := []Menu{}
		for _, m := range all {
			if m.Location == "" || (loc != "" && m.Location != loc) {
				continue
			}
			m.Items = res.resolve(m.Items, 1, map[int64]bool{m.ID: true})
			out = append(out, m)
		}
		return out, nil
	})
	hs.Handle("GET", "/menus/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := sdk.RequireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		all, err := list(r.Context(), hs.DB, "*")
		if err != nil {
			return nil, err
		}
		for _, m := range all {
			if m.ID == id {
				_ = normalize(m.Items, 1)
				return m, nil
			}
		}
		return nil, apperr.NotFound("menu")
	})
	save := func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := sdk.RequireAdmin(r); err != nil {
			return nil, err
		}
		var in Menu
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || !locRe.MatchString(in.Location) {
			return nil, apperr.BadRequest("a name and a valid location (e.g. header-nav, sidebar; empty for a submenu) are required")
		}
		if in.Items == nil {
			in.Items = []Item{}
		}
		if err := normalize(in.Items, 1); err != nil {
			return nil, err
		}
		ctx := r.Context()
		var mid int64
		if r.PathValue("id") != "" {
			var err error
			if mid, err = h.PathID(r, "id"); err != nil {
				return nil, err
			}
		}
		if err := checkSubmenus(ctx, hs.DB, mid, in.Items); err != nil {
			return nil, err
		}
		items, _ := json.Marshal(in.Items)
		if mid != 0 {
			tag, err := hs.DB.Exec(ctx, `UPDATE plugin_menus SET name=$2, location=$3, position=$4, items=$5, updated_at=now() WHERE id=$1`,
				mid, in.Name, in.Location, in.Position, items)
			if err != nil {
				return nil, err
			}
			if tag.RowsAffected() == 0 {
				return nil, apperr.NotFound("menu")
			}
			in.ID = mid
		} else if err := hs.DB.QueryRow(ctx, `INSERT INTO plugin_menus(name, location, position, items) VALUES ($1,$2,$3,$4) RETURNING id`,
			in.Name, in.Location, in.Position, items).Scan(&in.ID); err != nil {
			return nil, err
		}
		_ = audit.Log(ctx, hs.DB, "menu.save", "menu", in.ID, nil, in)
		return in, nil
	}
	hs.Handle("POST", "/menus", save)
	hs.Handle("PUT", "/menus/{id}", save)
	hs.Handle("DELETE", "/menus/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := sdk.RequireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		all, err := list(r.Context(), hs.DB, "*")
		if err != nil {
			return nil, err
		}
		var users []string
		for _, m := range all {
			refs := map[int64]bool{}
			menuRefs(m.Items, refs)
			if m.ID != id && refs[id] {
				users = append(users, m.Name)
			}
		}
		if len(users) > 0 {
			return nil, apperr.Conflict("the menu is used as a submenu in: %s", strings.Join(users, ", "))
		}
		tag, err := hs.DB.Exec(r.Context(), `DELETE FROM plugin_menus WHERE id=$1`, id)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, apperr.NotFound("menu")
		}
		_ = audit.Log(r.Context(), hs.DB, "menu.delete", "menu", id, nil, nil)
		return h.NoContent{}, nil
	})
	return nil
}
