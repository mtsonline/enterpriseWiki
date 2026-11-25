package server_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

type personResp struct {
	ID          int64             `json:"id"`
	DisplayName string            `json:"display_name"`
	Handle      string            `json:"handle"`
	Card        map[string]string `json:"card"`
	Email       *string           `json:"email"`
	Self        bool              `json:"self"`
}

// TestPeople: person pages and the ids next to names follow
// core.people.view and the rules for finding people; e-mail addresses
// follow people.email_visibility and the person's own choice; guests get
// nothing; a hidden address is never returned, also not in people blocks.
func TestPeople(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	const pw = "Passw0rd!Passw0rd"
	var team, c idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "People team"}, &team, 201)
	ids := map[string]int64{}
	clients := map[string]*client{}
	for _, u := range []struct {
		name   string
		groups []int64
	}{{"carla", []int64{team.ID}}, {"mate", []int64{team.ID}}, {"out", []int64{}}} {
		var id idResp
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": u.name, "password": pw, "display_name": strings.ToUpper(u.name[:1]) + u.name[1:], "email": u.name + "@example.com", "groups": u.groups}, &id, 201)
		ids[u.name] = id.ID
		clients[u.name] = e.client()
		clients[u.name].login(u.name, pw)
	}
	carla, mate, out, guest := clients["carla"], clients["mate"], clients["out"], e.client()
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d/profile", ids["carla"]), map[string]string{"job_title": "Head of Lynx", "phone": "+43 1 234"}, nil, 200)

	get := func(cl *client, id int64, want int) personResp {
		t.Helper()
		var p personResp
		cl.must("GET", fmt.Sprintf("/api/v1/people/%d", id), nil, &p, want)
		return p
	}
	email := func(p personResp) string {
		if p.Email == nil {
			return ""
		}
		return *p.Email
	}

	// who may open carla
	p := get(mate, ids["carla"], 200)
	if p.DisplayName != "Carla" || p.Handle != "carla@local" || p.Card["job_title"] != "Head of Lynx" || email(p) != "carla@example.com" || p.Self {
		t.Fatalf("mate sees carla: %+v", p)
	}
	get(guest, ids["carla"], 401)
	get(out, ids["carla"], 404) // not in a shared group: like a person that does not exist
	get(out, 999999999, 404)    // the same answer
	if p := get(carla, ids["carla"], 200); !p.Self || email(p) != "carla@example.com" {
		t.Fatalf("carla herself: %+v", p)
	}
	get(admin, ids["out"], 200)

	// the own address hidden: nobody else sees it, administrators neither
	carla.must("PUT", "/api/v1/me/profile", map[string]string{"email_hidden": "true"}, nil, 200)
	if p := get(mate, ids["carla"], 200); p.Email != nil {
		t.Fatalf("hidden address for mate: %+v", p)
	}
	if p := get(admin, ids["carla"], 200); p.Email != nil {
		t.Fatalf("hidden address for the administrator: %+v", p)
	}
	if p := get(carla, ids["carla"], 200); email(p) != "carla@example.com" {
		t.Fatalf("own hidden address: %+v", p)
	}
	carla.must("PUT", "/api/v1/me/profile", map[string]string{"email_hidden": "yes"}, nil, 400)

	// people blocks follow the rule too (team pages do not)
	var cat idResp
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "People pages"}}, &cat, 201)
	c = cat
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 15, "inherit": true}, nil, 201)
	var blocks elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Contacts",
		"body": map[string]any{"format": "markdown", "source": fmt.Sprintf("::profilecard{user=%d}\n\n::people{group=%d}\n", ids["carla"], team.ID)}}, &blocks, 201)
	html := func(cl *client, id int64) string {
		t.Helper()
		var v struct {
			HTML string `json:"html"`
		}
		cl.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", id), nil, &v, 200)
		return v.HTML
	}
	if h := html(mate, blocks.ID); strings.Contains(h, "carla@example.com") || !strings.Contains(h, "mate@example.com") || !strings.Contains(h, "Head of Lynx") {
		t.Fatalf("people blocks with a hidden address: %s", h)
	}

	// hiding not allowed: the choice no longer counts, and cannot be set
	settings := func(vis string, hide bool) {
		t.Helper()
		admin.must("PUT", "/api/v1/admin/settings", map[string]any{"people": map[string]any{"email_visibility": vis, "email_hide_allowed": hide}}, nil, 200)
	}
	settings("users", false)
	if p := get(mate, ids["carla"], 200); email(p) != "carla@example.com" {
		t.Fatalf("hiding not allowed: %+v", p)
	}
	mate.must("PUT", "/api/v1/me/profile", map[string]string{"email_hidden": "true"}, nil, 400)
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"people": map[string]any{"email_visibility": "everybody"}}, nil, 400)

	// only with the permission
	settings("permission", true)
	carla.must("PUT", "/api/v1/me/profile", map[string]string{"email_hidden": ""}, nil, 200)
	if p := get(mate, ids["carla"], 200); p.Email != nil {
		t.Fatalf("without core.people.email: %+v", p)
	}
	if h := html(mate, blocks.ID); strings.Contains(h, "carla@example.com") {
		t.Fatalf("people blocks without core.people.email: %s", h)
	}
	var role idResp
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Address book", "permissions": []string{"core.people.email"}}, &role, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", ids["mate"]), map[string]any{"display_name": "Mate", "email": "mate@example.com", "groups": []int64{team.ID}, "roles": []int64{role.ID}}, nil, 200)
	mate.login("mate", pw)
	if p := get(mate, ids["carla"], 200); email(p) != "carla@example.com" {
		t.Fatalf("with core.people.email: %+v", p)
	}
	settings("nobody", true)
	if p := get(mate, ids["carla"], 200); p.Email != nil {
		t.Fatalf("nobody: %+v", p)
	}
	if p := get(admin, ids["carla"], 200); p.Email != nil {
		t.Fatalf("nobody, administrator: %+v", p)
	}
	settings("users", true)

	// ids next to names: only for who may open the person
	var el elem
	carla.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Notes", "body": htmlBody("<p>x</p>")}, &el, 201)
	carla.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", el.ID), map[string]any{"body": "Hello"}, nil, 201)
	type refs struct {
		Element, Revision, Comment *int64
	}
	refsOf := func(cl *client) refs {
		t.Helper()
		var v struct {
			UpdatedByID *int64 `json:"updated_by_id"`
		}
		cl.must("GET", fmt.Sprintf("/api/v1/elements/%d", el.ID), nil, &v, 200)
		var revs []struct {
			AuthorID *int64 `json:"author_id"`
		}
		cl.must("GET", fmt.Sprintf("/api/v1/elements/%d/revisions", el.ID), nil, &revs, 200)
		var cm struct {
			Comments []struct {
				AuthorID *int64 `json:"author_id"`
			} `json:"comments"`
		}
		cl.must("GET", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", el.ID), nil, &cm, 200)
		return refs{v.UpdatedByID, revs[0].AuthorID, cm.Comments[0].AuthorID}
	}
	is := func(p *int64, id int64) bool { return p != nil && *p == id }
	if r := refsOf(mate); !is(r.Element, ids["carla"]) || !is(r.Revision, ids["carla"]) || !is(r.Comment, ids["carla"]) {
		t.Fatalf("ids for mate: %+v", r)
	}
	for who, cl := range map[string]*client{"guest": guest, "out": out} {
		if r := refsOf(cl); r.Element != nil || r.Revision != nil || r.Comment != nil {
			t.Fatalf("ids for %s: %+v", who, r)
		}
	}

	// team page: ids and marks only for who may open the person
	var tp elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.team", "title": "Team",
		"body": map[string]any{"groups": []int64{team.ID}, "fields": []string{"job_title", "email"}, "layout": "cards"}}, &tp, 201)
	members := func(cl *client) []*int64 {
		t.Helper()
		var m struct {
			Sections []struct {
				Members []struct {
					ID *int64 `json:"id"`
				} `json:"members"`
			} `json:"sections"`
		}
		cl.must("GET", fmt.Sprintf("/api/v1/x/content.team/elements/%d/members", tp.ID), nil, &m, 200)
		var out []*int64
		for _, s := range m.Sections {
			for _, x := range s.Members {
				out = append(out, x.ID)
			}
		}
		return out
	}
	if got := members(mate); len(got) != 2 || got[0] == nil || got[1] == nil {
		t.Fatalf("team ids for mate: %v", got)
	}
	for who, cl := range map[string]*client{"guest": guest, "out": out} {
		if got := members(cl); len(got) != 2 || slices.ContainsFunc(got, func(p *int64) bool { return p != nil }) {
			t.Fatalf("team ids for %s: %v", who, got)
		}
		if h := html(cl, tp.ID); strings.Contains(h, "data-person") {
			t.Fatalf("team marks for %s: %s", who, h)
		}
	}
	if h := html(mate, tp.ID); !strings.Contains(h, fmt.Sprintf(`data-person="%d"`, ids["carla"])) {
		t.Fatalf("team marks for mate: %s", h)
	}

	// without core.people.view: nobody opens people, no ids
	var defaults struct {
		Authenticated []string `json:"authenticated"`
	}
	admin.must("GET", "/api/v1/admin/permissions", nil, &defaults, 200)
	without := slices.DeleteFunc(slices.Clone(defaults.Authenticated), func(k string) bool { return k == "core.people.view" })
	admin.must("PUT", "/api/v1/admin/permissions/authenticated", map[string]any{"permissions": without}, nil, 204)
	mate.login("mate", pw)
	get(mate, ids["carla"], 403)
	if r := refsOf(mate); r.Element != nil || r.Revision != nil || r.Comment != nil {
		t.Fatalf("ids without core.people.view: %+v", r)
	}
	admin.must("PUT", "/api/v1/admin/permissions/authenticated", map[string]any{"permissions": defaults.Authenticated}, nil, 204)

	// no bulk collecting: at most core.people.views_per_hour people per hour
	admin.must("PUT", "/api/v1/admin/limits", map[string]any{"values": map[string]any{"core.people.views_per_hour": 10}}, nil, 200)
	var lim idResp
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "lim", "password": pw, "display_name": "Lim"}, &lim, 201)
	limited := e.client()
	limited.login("lim", pw)
	for i := 0; i < 10; i++ {
		get(limited, lim.ID, 200)
	}
	get(limited, lim.ID, 429)
	get(limited, 999999999, 429) // also for people that do not exist
	get(admin, ids["out"], 200)  // administrators without limit
}

// TestPeopleCLIAs: "ewiki person show" goes through the API; --as carries
// exactly the rights of that person, never more.
func TestPeopleCLIAs(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var g, carla idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "CLI people"}, &g, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "carla", "password": "Passw0rd!Passw0rd", "display_name": "Carla", "email": "carla@example.com", "groups": []int64{g.ID}}, &carla, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "mate", "password": "Passw0rd!Passw0rd", "display_name": "Mate", "groups": []int64{g.ID}}, nil, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "out", "password": "Passw0rd!Passw0rd", "display_name": "Out"}, nil, 201)
	id := fmt.Sprint(carla.ID)
	if code, out := runCLI(t, e.cfg, "person", "show", id); code != 0 || !strings.Contains(out, "carla@example.com") {
		t.Fatalf("person show: exit %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "person", "show", id, "--as", "mate"); code != 0 || !strings.Contains(out, "Carla") {
		t.Fatalf("person show --as mate: exit %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "person", "show", id, "--as", "out"); code == 0 || strings.Contains(out, "carla@example.com") {
		t.Fatalf("person show --as out: exit %d %s", code, out)
	}
}

// TestPeopleInLists: drafts and events name people with the display name
// only and carry their id only for who may open them; the raw id of the
// organiser of an event is never sent.
func TestPeopleInLists(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	const pw = "Passw0rd!Passw0rd"
	var g, carla, c idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "List people"}, &g, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "carla", "password": pw, "display_name": "Carla", "groups": []int64{g.ID}}, &carla, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "mate", "password": pw, "display_name": "Mate", "groups": []int64{g.ID}}, nil, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "out", "password": pw, "display_name": "Out"}, nil, 201)
	cl := map[string]*client{}
	for _, n := range []string{"carla", "mate", "out"} {
		cl[n] = e.client()
		cl[n].login(n, pw)
	}
	guest := e.client()
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Lists"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 15, "inherit": true}, nil, 201)

	// drafts
	cl["carla"].must("POST", "/api/v1/drafts", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Draft", "body": map[string]any{"format": "html", "source": "<p>d</p>"}}, nil, 201)
	drafts := func(who string) (string, *int64) {
		t.Helper()
		var ds []struct {
			UpdatedBy   string `json:"updated_by"`
			UpdatedByID *int64 `json:"updated_by_id"`
		}
		cl[who].must("GET", fmt.Sprintf("/api/v1/drafts?category=%d", c.ID), nil, &ds, 200)
		if len(ds) != 1 {
			t.Fatalf("drafts for %s: %+v", who, ds)
		}
		return ds[0].UpdatedBy, ds[0].UpdatedByID
	}
	if n, id := drafts("mate"); n != "Carla" || id == nil || *id != carla.ID {
		t.Fatalf("draft for mate: %q %v", n, id)
	}
	if n, id := drafts("out"); n != "Carla" || id != nil {
		t.Fatalf("draft for out: %q %v", n, id)
	}

	// events: the organiser
	var cal elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.events", "title": "Dates", "body": map[string]any{"view": "list"}}, &cal, 201)
	start := time.Now().Add(72 * time.Hour).Truncate(time.Hour)
	var ev idResp
	cl["carla"].must("POST", fmt.Sprintf("/api/v1/x/content.events/calendars/%d/events", cal.ID), map[string]any{"title": "Yoga", "starts_at": start, "ends_at": start.Add(time.Hour), "tz": "UTC"}, &ev, 201)
	event := func(who string, c *client) map[string]any {
		t.Helper()
		var v struct {
			Event map[string]any `json:"event"`
		}
		c.must("GET", fmt.Sprintf("/api/v1/x/content.events/events/%d", ev.ID), nil, &v, 200)
		if _, ok := v.Event["created_by"]; ok {
			t.Errorf("%s gets the raw id of the organiser: %v", who, v.Event)
		}
		if v.Event["author"] != "Carla" {
			t.Errorf("%s: organiser %v", who, v.Event["author"])
		}
		return v.Event
	}
	if v := event("mate", cl["mate"]); v["author_id"] != float64(carla.ID) {
		t.Fatalf("event for mate: %v", v["author_id"])
	}
	for who, c := range map[string]*client{"out": cl["out"], "guest": guest} {
		if v := event(who, c); v["author_id"] != nil {
			t.Fatalf("event for %s: %v", who, v["author_id"])
		}
	}
}
