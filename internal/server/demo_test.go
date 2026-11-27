package server_test

import (
	"context"
	"io/fs"
	"maps"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/db"
)

// dbSnapshot counts the rows of every table of the site and shared schema.
func dbSnapshot(t *testing.T, e *env, skip ...string) map[string]int {
	t.Helper()
	ctx := context.Background()
	rows, err := e.app.DB.Query(ctx, `SELECT schemaname, tablename FROM pg_tables WHERE schemaname IN (current_schema(), 'public')`)
	if err != nil {
		t.Fatal(err)
	}
	type tbl struct{ s, n string }
	var list []tbl
	for rows.Next() {
		var x tbl
		if err := rows.Scan(&x.s, &x.n); err != nil {
			t.Fatal(err)
		}
		list = append(list, x)
	}
	rows.Close()
	out := map[string]int{}
	for _, x := range list {
		name := x.s + "." + x.n
		skipIt := false
		for _, s := range skip {
			skipIt = skipIt || x.n == s
		}
		if skipIt {
			continue
		}
		var n int
		if err := e.app.DB.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{x.s, x.n}.Sanitize()).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[name] = n
	}
	return out
}

func storedFiles(t *testing.T, e *env) []string {
	t.Helper()
	var out []string
	root := filepath.Join(e.app.Config.Get().Storage.Path, "files")
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, strings.TrimPrefix(p, root))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func diff(a, b map[string]int) []string {
	var out []string
	for _, k := range slicesSorted(a, b) {
		if a[k] != b[k] {
			out = append(out, k+": "+strconv.Itoa(a[k])+" → "+strconv.Itoa(b[k]))
		}
	}
	return out
}

func slicesSorted(a, b map[string]int) []string {
	m := maps.Clone(a)
	maps.Copy(m, b)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type demoState struct {
	Installed bool           `json:"installed"`
	Objects   map[string]int `json:"objects"`
	Skipped   []string       `json:"skipped"`
}

func TestDemo(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	// something of our own that must survive
	var own struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Own"}}, &own, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": own.ID, "type": "core.html", "title": "Mine", "body": htmlBody("<p>mine</p>")}, nil, 201)
	time.Sleep(10 * time.Millisecond)

	seats0, _ := db.CountActiveAccounts(context.Background(), e.app.Instance.Shared)
	before := dbSnapshot(t, e, "audit_log", "worker_jobs", "worker_runs")
	filesBefore := storedFiles(t, e)

	u := e.client()
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "eve", "password": "Eve-Passw0rd!x", "display_name": "Eve"}, nil, 201)
	before = dbSnapshot(t, e, "audit_log", "worker_jobs", "worker_runs")
	u.login("eve", "Eve-Passw0rd!x")
	if c := u.do("POST", "/api/v1/admin/demo", nil, nil); c != 403 {
		t.Fatalf("non-admin created the demo: %d", c)
	}
	// search_changes: technical change log (element ids only, kept 30 days)
	// that the search indexer may still append to after the removal
	// identity_changes: change log of accounts (kept a day) – the demo
	// accounts come and go
	skip := []string{"audit_log", "worker_jobs", "worker_runs", "sessions", "search_changes", "identity_changes"}
	before = dbSnapshot(t, e, skip...)

	home0 := e.app.Config.Get().UI
	for round := 0; round < 2; round++ {
		var st demoState
		admin.must("POST", "/api/v1/admin/demo", nil, &st, 200)
		// the start page: what the wiki can do, under a short address, the
		// way into the tour; without path, title and page details
		home := e.app.Config.Get().UI
		if home.HomeElement == 0 || home.HomeElement == home0.HomeElement || home.SiteName != home0.SiteName {
			t.Fatalf("demo start page: %+v (before %+v)", home, home0)
		}
		code, body, _ := admin.fetch(t, "/api/v1/resolve?path=/welcome")
		if code != 200 || !strings.Contains(string(body), strconv.FormatInt(home.HomeElement, 10)) {
			t.Fatalf("short URL /welcome: %d %s", code, body)
		}
		var land struct {
			HTML string `json:"html"`
		}
		admin.must("GET", "/api/v1/elements/"+strconv.FormatInt(home.HomeElement, 10), nil, &land, 200)
		if strings.Contains(land.HTML, "ew-macro-error") || strings.Count(land.HTML, `class="ewl-iconbox `) != 12 {
			t.Errorf("demo start page: %s", land.HTML)
		}
		var hidden int
		_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM placements WHERE level='instance' AND target=$1 AND position=''
			AND block IN ('core.element.path','core.element.title','core.element.info')`, strconv.FormatInt(home.HomeElement, 10)).Scan(&hidden)
		if hidden != 3 {
			t.Errorf("demo start page: %d of path, title and details hidden", hidden)
		}
		_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM placements WHERE level='instance' AND target=$1 AND position=''`,
			strconv.FormatInt(home.HomeElement, 10)).Scan(&hidden)
		if hidden != 8 {
			t.Errorf("demo start page: %d parts hidden, want 8", hidden)
		}
		if !st.Installed || st.Objects["element"] < 15 || st.Objects["user"] != 4 || st.Objects["medium"] < 5 {
			t.Fatalf("demo objects: %+v", st)
		}
		// with every plugin on and an administrator, no part is left out
		// (a part with a mistake would otherwise go unnoticed)
		if len(st.Skipped) != 0 {
			t.Fatalf("demo parts left out: %v", st.Skipped)
		}
		if c := admin.do("POST", "/api/v1/admin/demo", nil, nil); c != 409 {
			t.Fatalf("second demo: %d", c)
		}
		// the tour page renders through its short URL with macros
		code, body, _ = admin.fetch(t, "/api/v1/resolve?path=/demo")
		if code != 200 {
			t.Fatalf("short URL /demo: %d %s", code, body)
		}
		// demo accounts cannot sign in and use no license seats
		if c := e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "demo-anna", "password": ""}, nil); c == 200 {
			t.Fatal("demo account signed in")
		}
		if seats, _ := db.CountActiveAccounts(context.Background(), e.app.Instance.Shared); seats != seats0+1 {
			t.Fatalf("license seats %d, want %d", seats, seats0+1)
		}
		if round == 0 {
			checkDemoRules(t, e, admin)
		}
		// the job application answers with a pop-up
		var popupSteps int
		_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM elements e JOIN element_revisions r ON r.element_id = e.id
			WHERE e.titles->>'en' = 'Internal job application' AND r.body::text LIKE '%"popup": true%'`).Scan(&popupSteps)
		if popupSteps == 0 {
			t.Error("demo: the job application does not answer with a pop-up")
		}
		// the stations of the tour show their blocks, without errors
		station := func(en string) string {
			t.Helper()
			var id int64
			if err := e.app.DB.QueryRow(context.Background(), `SELECT id FROM elements WHERE titles->>'en' LIKE '%. ' || $1 AND deleted_at IS NULL`, en).Scan(&id); err != nil {
				t.Fatalf("station %q: %v", en, err)
			}
			var v struct {
				HTML string `json:"html"`
			}
			admin.must("GET", "/api/v1/elements/"+strconv.FormatInt(id, 10), nil, &v, 200)
			return v.HTML
		}
		for name, wants := range map[string][]string{
			"Wiki building blocks":              {"the last one out", `id="tree"`, `class="ew-searchbox"`, `class="ew-pagetree"`, "House rules", "New meeting minutes"},
			"Files and the media library":       {`class="ew-attachments"`, "/download"},
			"Figures and charts":                {`<svg class="ewl-chart-svg"`, "Faults"},
			"Pictures and galleries":            {`class="ewl-hotspot"`, `data-ew-widget="ewl.compare"`},
			"Timeline, slider, dialog and more": {"ewl-plan--featured", `data-ew-widget="ewl.countdown"`},
			// what is placed where outside the pages
			"Menus, placement and carousel": {"Menu “Demo”", "Block “Reception”", "Carousel “Demo”", "Handbook", "content-top", "Where to change it:"},
		} {
			html := station(name)
			for _, want := range wants {
				if !strings.Contains(html, want) {
					t.Errorf("station %q: %q missing", name, want)
				}
			}
			if strings.Contains(html, "ew-macro-error") {
				t.Errorf("station %q with an error: %s", name, html)
			}
		}
		// the carousel has no fixed position: it is placed for the category of
		// the company only (its start page, not the subcategories)
		var carPos string
		if err := e.app.DB.QueryRow(context.Background(), `SELECT position FROM plugin_carousels WHERE name='Demo'`).Scan(&carPos); err != nil || carPos != "" {
			t.Errorf("demo carousel position %q, %v", carPos, err)
		}
		var placed int
		_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM placements p JOIN categories c ON p.level='category' AND p.target=c.id::text
			WHERE c.titles->>'en'='Enterprise IT Services' AND p.block LIKE 'core.carousel/%' AND p.position='content-top' AND p.no_subcategories`).Scan(&placed)
		if placed != 1 {
			t.Errorf("demo carousel placed %d times for the company", placed)
		}
		// a page somebody added inside the demo area goes with it
		var demoCat int64
		if err := e.app.DB.QueryRow(context.Background(), `SELECT min(ref::bigint) FROM demo_objects WHERE kind='category'`).Scan(&demoCat); err != nil {
			t.Fatal(err)
		}
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": demoCat, "type": "core.html", "title": "Added", "body": htmlBody("<p>x</p>")}, nil, 201)
		admin.must("DELETE", "/api/v1/admin/demo", nil, nil, 204)
		if h := e.app.Config.Get().UI; h.HomeElement != home0.HomeElement || h.HomeCategory != home0.HomeCategory {
			t.Fatalf("start page after removing the demo: %+v, want %+v", h, home0)
		}
		admin.must("GET", "/api/v1/admin/demo", nil, &st, 200)
		if st.Installed {
			t.Fatalf("still installed: %+v", st)
		}
		after := dbSnapshot(t, e, skip...)
		if d := diff(before, after); len(d) > 0 {
			t.Fatalf("round %d: the demo left rows behind:\n%s", round, strings.Join(d, "\n"))
		}
		if f := storedFiles(t, e); strings.Join(f, ",") != strings.Join(filesBefore, ",") {
			t.Fatalf("round %d: files left behind: %v (before %v)", round, f, filesBefore)
		}
		var traces int
		_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action LIKE 'demo.create'`).Scan(&traces)
		if traces != 0 {
			t.Fatalf("audit traces of the demo: %d", traces)
		}
	}
	// own content is untouched
	admin.must("GET", "/api/v1/categories/"+strconv.Itoa(int(own.ID)), nil, nil, 200)
}

func TestSiteReset(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var own struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Own"}}, &own, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "eve", "password": "Eve-Passw0rd!x", "display_name": "Eve"}, nil, 201)
	admin.must("POST", "/api/v1/admin/demo", nil, nil, 200)
	if err := e.app.Config.Update(func(c *config.Config) error { c.Retention.TrashDays = 3; return nil }); err != nil {
		t.Fatal(err)
	}
	name := e.app.Config.Get().UI.SiteName
	if c := admin.do("POST", "/api/v1/admin/reset", map[string]string{"confirm": "wrong", "password": adminPW}, nil); c != 400 {
		t.Fatalf("wrong confirmation: %d", c)
	}
	if c := admin.do("POST", "/api/v1/admin/reset", map[string]string{"confirm": name, "password": "nope"}, nil); c != 403 {
		t.Fatalf("wrong password: %d", c)
	}
	eve := e.client()
	eve.login("eve", "Eve-Passw0rd!x")
	if c := eve.do("POST", "/api/v1/admin/reset", map[string]string{"confirm": name, "password": "Eve-Passw0rd!x"}, nil); c != 403 {
		t.Fatalf("non-admin reset: %d", c)
	}
	var key struct {
		Token string `json:"token"`
	}
	admin.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "k", "permissions": 15, "allow_admin": true, "expires_at": time.Now().Add(time.Hour)}, &key, 201)
	kc := e.client()
	kc.token = key.Token
	if c := kc.do("POST", "/api/v1/admin/reset", map[string]string{"confirm": name, "password": adminPW}, nil); c != 403 {
		t.Fatalf("reset with an API key: %d", c)
	}
	admin.must("POST", "/api/v1/admin/reset", map[string]string{"confirm": name, "password": adminPW}, nil, 200)

	// like the first start: one administrator, the welcome space, default settings
	ctx := context.Background()
	var users, cats, elems, demo, audit int
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users)
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM categories`).Scan(&cats)
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM elements`).Scan(&elems)
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM demo_objects`).Scan(&demo)
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action <> 'site.reset'`).Scan(&audit)
	if users != 1 || cats != 1 || elems != 1 || demo != 0 {
		t.Fatalf("after reset: users=%d categories=%d elements=%d demo=%d", users, cats, elems, demo)
	}
	var old int // entries of before the reset (the welcome space logs its own)
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_type NOT IN ('category','element','acl','settings','config','site')`).Scan(&old)
	if old > 0 || audit > 8 {
		rows, _ := e.app.DB.Query(ctx, `SELECT action || ' ' || entity_type FROM audit_log`)
		list, _ := pgx.CollectRows(rows, pgx.RowTo[string])
		t.Fatalf("audit entries survived: %v", list)
	}
	if f := storedFiles(t, e); len(f) != 0 {
		t.Fatalf("files survived: %v", f)
	}
	if e.app.Config.Get().Retention.TrashDays == 3 {
		t.Fatal("settings survived")
	}
	// the session is gone; the administrator signs in again
	if c := admin.do("GET", "/api/v1/admin/demo", nil, nil); c != 401 && c != 403 {
		t.Fatalf("old session still valid: %d", c)
	}
	again := e.client()
	again.login("admin", adminPW)
	var st demoState
	again.must("GET", "/api/v1/admin/demo", nil, &st, 200)
	if eveC := e.client(); eveC.do("POST", "/api/v1/auth/login", map[string]string{"username": "eve", "password": "Eve-Passw0rd!x"}, nil) == 200 {
		t.Fatal("deleted account signed in")
	}
}

// Resetting one site of several with shared accounts keeps the accounts and
// never touches the other sites.
func TestSiteResetSharedAccounts(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var mainCat struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Main stuff"}}, &mainCat, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "eve", "password": "Eve-Passw0rd!x", "display_name": "Eve"}, nil, 201)
	admin.must("POST", "/api/v1/instance/sites", map[string]any{"name": "docs", "title": "Docs", "hostnames": []string{"docs.example.test"}}, nil, 201)
	sa := e.client()
	sa.host = "docs.example.test"
	sa.login("admin", adminPW)
	sa.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Docs stuff"}}, nil, 201)
	sa.must("POST", "/api/v1/admin/demo", nil, nil, 200)
	var st struct {
		SiteName string `json:"site_name"`
	}
	sa.must("GET", "/api/v1/admin/demo", nil, &st, 200)
	sa.must("POST", "/api/v1/admin/reset", map[string]string{"confirm": st.SiteName, "password": adminPW}, nil, 200)

	// the main site and the shared accounts are untouched
	admin.must("GET", "/api/v1/categories/"+strconv.FormatInt(mainCat.ID, 10), nil, nil, 200)
	if c := e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "eve", "password": "Eve-Passw0rd!x"}, nil); c != 200 {
		t.Fatalf("shared account gone after resetting another site: %d", c)
	}
	var demoUsers int
	_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE provider='demo'`).Scan(&demoUsers)
	if demoUsers != 0 {
		t.Fatalf("demo accounts of the reset site survived: %d", demoUsers)
	}
	again := e.client()
	again.host = "docs.example.test"
	again.login("admin", adminPW)
	var cats []map[string]any
	again.must("GET", "/api/v1/categories", nil, &cats, 200)
	if len(cats) != 1 {
		t.Fatalf("docs site after reset: %d top categories", len(cats))
	}
}

// TestDemoCycle: the demo is created, removed, created again, the site is
// reset, and the demo is created and removed once more – every step must
// work (an owner saw a failure when creating it again and a 500 on reset).
func TestDemoCycle(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var st demoState
	admin.must("POST", "/api/v1/admin/demo", nil, &st, 200)
	admin.must("DELETE", "/api/v1/admin/demo", nil, nil, 204)
	admin.must("POST", "/api/v1/admin/demo", nil, &st, 200)
	name := e.app.Config.Get().UI.SiteName
	admin.must("POST", "/api/v1/admin/reset", map[string]string{"confirm": name, "password": adminPW}, nil, 200)
	again := e.client()
	again.login("admin", adminPW)
	again.must("POST", "/api/v1/admin/demo", nil, &st, 200)
	again.must("DELETE", "/api/v1/admin/demo", nil, nil, 204)
	again.must("GET", "/api/v1/admin/demo", nil, &st, 200)
}
