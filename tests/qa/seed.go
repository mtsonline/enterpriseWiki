package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// World describes the test data of a run (written to world.json).
type World struct {
	Base         string             `json:"base"`
	Root         int64              `json:"root"`    // "QA Suite": everything below is test data
	Team         int64              `json:"team"`    // copy of the demo area: editors edit, readers read
	Pages        int64              `json:"pages"`   // pages created by the virtual users
	Collab       int64              `json:"collab"`  // pages for live editing
	Private      int64              `json:"private"` // no rule: only administrators
	Public       int64              `json:"public"`  // guests read
	EditorsGroup int64              `json:"editors_group"`
	ReadersGroup int64              `json:"readers_group"`
	Users        []QAUser           `json:"users"`
	Secret       string             `json:"secret"` // marker text of the private page
	SecretPage   int64              `json:"secret_page"`
	PublicPage   int64              `json:"public_page"`
	SharedPage   int64              `json:"shared_page"` // for the conflict test
	CollabPages  []int64            `json:"collab_pages"`
	Typed        map[string][]int64 `json:"typed"` // element type -> elements of the team area
	TeamCats     []int64            `json:"team_cats"`
	// elements of the QA area the virtual users write to
	BlogCat       int64  `json:"blog_cat"`
	Blog          int64  `json:"blog"`
	FAQCat        int64  `json:"faq_cat"`
	FAQ           int64  `json:"faq"`
	Calendar      int64  `json:"calendar"`
	SharedEvent   int64  `json:"shared_event"` // everybody registers at the same moment
	EventCapacity int    `json:"event_capacity"`
	EventDate     string `json:"event_date"` // occurrence key of the shared event
	Workflow      int64  `json:"workflow"`
	SharedCase    int64  `json:"shared_case"` // every editor approves at the same moment
}

// QAUser is a QA account.
type QAUser struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"` // editor | reader
}

const qaRootTitle = "QA Suite (automatic tests)"

func adminClient(o Options) (*Client, error) {
	c := newClient(o.Base, "admin")
	if o.SpoofIPs {
		c.IP = "10.15.0.1"
	}
	r := c.Login(o.AdminUser, o.AdminPass)
	if r.Err != nil {
		return nil, r.Err
	}
	if r.Status != 200 {
		return nil, fmt.Errorf("administrator sign-in failed: %d %s", r.Status, r.Text(200))
	}
	return c, nil
}

func must(r *Resp, want ...int) error {
	if r.Err != nil {
		return r.Err
	}
	for _, w := range want {
		if r.Status == w {
			return nil
		}
	}
	return fmt.Errorf("unexpected status %d (want %v): %s", r.Status, want, r.Text(300))
}

type idObj struct {
	ID int64 `json:"id"`
}

type catNode struct {
	ID       int64             `json:"id"`
	ParentID *int64            `json:"parent_id"`
	Titles   map[string]string `json:"titles"`
}

func hasTitle(n catNode, prefix string) bool {
	for _, t := range n.Titles {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

// seed creates the QA area, groups and accounts. It removes an older QA
// area first, so every run starts from the same state.
func seed(o Options) (*World, error) {
	adm, err := adminClient(o)
	if err != nil {
		return nil, err
	}
	if err := removeArea(adm); err != nil {
		return nil, err
	}
	w := &World{Base: o.Base, Secret: "QA-SECRET-" + randHex(8), Typed: map[string][]int64{}}

	// demo content (all element types) as the source of the team area
	var tree []catNode
	if err := must(adm.Do("GET", "/api/v1/categories/tree", nil), 200); err != nil {
		return nil, err
	}
	_ = adm.Do("GET", "/api/v1/categories/tree", nil).JSON(&tree)
	demo := findTop(tree, "Demo:")
	if demo == 0 {
		if err := must(adm.Do("POST", "/api/v1/admin/demo", nil), 200, 201); err != nil {
			return nil, fmt.Errorf("create demo content: %w", err)
		}
		_ = adm.Do("GET", "/api/v1/categories/tree", nil).JSON(&tree)
		if demo = findTop(tree, "Demo:"); demo == 0 {
			return nil, errors.New("demo area not found after creating it")
		}
	}

	group := func(name string) (int64, error) {
		var gs []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		_ = adm.Do("GET", "/api/v1/groups", nil).JSON(&gs)
		for _, g := range gs {
			if g.Name == name {
				return g.ID, nil
			}
		}
		var g idObj
		r := adm.Do("POST", "/api/v1/admin/groups", map[string]string{"name": name, "description": "QA suite"})
		if err := must(r, 200, 201); err != nil {
			return 0, err
		}
		_ = r.JSON(&g)
		return g.ID, nil
	}
	if w.EditorsGroup, err = group("qa-editors"); err != nil {
		return nil, err
	}
	if w.ReadersGroup, err = group("qa-readers"); err != nil {
		return nil, err
	}

	// accounts: 4 of 5 are editors
	w.Users = make([]QAUser, o.Users)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	sem := make(chan struct{}, 4)
	for i := 0; i < o.Users; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			u := QAUser{Name: fmt.Sprintf("qa-u%04d", i+1), Role: "editor"}
			gid := w.EditorsGroup
			if i%5 == 4 {
				u.Role, gid = "reader", w.ReadersGroup
			}
			in := map[string]any{"username": u.Name, "password": QAPassword(), "display_name": fmt.Sprintf("QA %s %04d", u.Role, i+1),
				"email": u.Name + "@qa.invalid", "groups": []int64{gid}}
			r := adm.Do("POST", "/api/v1/admin/users", in)
			var x idObj
			switch {
			case r.Status == 201 || r.Status == 200:
				_ = r.JSON(&x)
			case r.Status == 409:
				var us []struct {
					ID       int64  `json:"id"`
					Username string `json:"username"`
				}
				_ = adm.Do("GET", "/api/v1/admin/users?q="+u.Name, nil).JSON(&us)
				for _, e := range us {
					if e.Username == u.Name {
						x.ID = e.ID
					}
				}
				if x.ID != 0 {
					r = adm.Do("PUT", fmt.Sprintf("/api/v1/admin/users/%d", x.ID), map[string]any{"password": QAPassword(), "groups": []int64{gid}, "disabled": false})
				}
			}
			if x.ID == 0 {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("create %s: %d %s", u.Name, r.Status, r.Text(200))
				}
				mu.Unlock()
				return
			}
			u.ID = x.ID
			w.Users[i] = u
		}(i)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	cat := func(parent int64, title string) (int64, error) {
		in := map[string]any{"titles": map[string]string{"de": title, "en": title}}
		if parent != 0 {
			in["parent_id"] = parent
		}
		r := adm.Do("POST", "/api/v1/categories", in)
		if err := must(r, 201, 200); err != nil {
			return 0, fmt.Errorf("category %q: %w", title, err)
		}
		var c idObj
		_ = r.JSON(&c)
		return c.ID, nil
	}
	rule := func(cat int64, in map[string]any) error {
		return must(adm.Do("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat), in), 201, 200)
	}
	page := func(cat int64, title, html string) (int64, error) {
		r := adm.Do("POST", "/api/v1/elements", map[string]any{"category_id": cat, "type": "core.html", "lang": "de", "title": title, "body": htmlBody(html)})
		if err := must(r, 201); err != nil {
			return 0, fmt.Errorf("page %q: %w", title, err)
		}
		var e idObj
		_ = r.JSON(&e)
		return e.ID, nil
	}
	if w.Root, err = cat(0, qaRootTitle); err != nil {
		return nil, err
	}
	// team: a copy of the demo area with every element type
	r := adm.Do("POST", fmt.Sprintf("/api/v1/categories/%d/duplicate", demo), map[string]any{"parent_id": w.Root})
	if err := must(r, 201, 200); err != nil {
		return nil, fmt.Errorf("copy the demo area: %w", err)
	}
	var team idObj
	_ = r.JSON(&team)
	w.Team = team.ID
	for _, x := range []struct {
		dst   *int64
		par   int64
		title string
	}{{&w.Pages, w.Team, "QA pages"}, {&w.Collab, w.Team, "QA live editing"}, {&w.Private, w.Root, "QA private"}, {&w.Public, w.Root, "QA public"}} {
		if *x.dst, err = cat(x.par, x.title); err != nil {
			return nil, err
		}
	}
	for _, x := range []struct {
		cat int64
		in  map[string]any
	}{
		{w.Team, map[string]any{"subject_type": "group", "subject_id": w.EditorsGroup, "preset": "editor", "inherit": true}},
		{w.Team, map[string]any{"subject_type": "group", "subject_id": w.ReadersGroup, "preset": "reader", "inherit": true}},
		{w.Public, map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}},
		{w.Public, map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}},
		{w.Root, map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": false}},
	} {
		if err := rule(x.cat, x.in); err != nil {
			return nil, fmt.Errorf("rule on %d: %w", x.cat, err)
		}
	}
	if w.SecretPage, err = page(w.Private, "QA secret page", "<p>"+w.Secret+"</p>"); err != nil {
		return nil, err
	}
	if w.PublicPage, err = page(w.Public, "QA public page", "<h2>Public</h2><p>Readable by guests. Suchwort qapublicword.</p>"); err != nil {
		return nil, err
	}
	if w.SharedPage, err = page(w.Pages, "QA shared page", "<p>Everybody saves this page at the same moment.</p>"); err != nil {
		return nil, err
	}
	rooms := o.Users/20 + 2
	for i := 0; i < rooms; i++ {
		id, err := page(w.Collab, fmt.Sprintf("QA live page %03d", i+1), "<p>Live editing.</p>")
		if err != nil {
			return nil, err
		}
		w.CollabPages = append(w.CollabPages, id)
	}
	if err := w.discover(adm); err != nil {
		return nil, err
	}
	// own blog, FAQ, calendar and workflow (the copies of the demo still
	// show the categories of the original)
	element := func(cat int64, typ, title string, body any) (int64, error) {
		r := adm.Do("POST", "/api/v1/elements", map[string]any{"category_id": cat, "type": typ, "lang": "de", "title": title, "body": body})
		if err := must(r, 201); err != nil {
			return 0, fmt.Errorf("%s %q: %w", typ, title, err)
		}
		var e idObj
		_ = r.JSON(&e)
		return e.ID, nil
	}
	if w.BlogCat, err = cat(w.Team, "QA blog"); err != nil {
		return nil, err
	}
	if w.FAQCat, err = cat(w.Team, "QA questions"); err != nil {
		return nil, err
	}
	if w.Blog, err = element(w.BlogCat, "content.blog", "QA blog", map[string]any{"category_id": w.BlogCat, "per_page": 10}); err != nil {
		return nil, err
	}
	if w.FAQ, err = element(w.FAQCat, "content.faq", "QA questions", map[string]any{"category_id": w.FAQCat, "recursive": true}); err != nil {
		return nil, err
	}
	if w.Calendar, err = element(w.Pages, "content.events", "QA calendar", map[string]any{"view": "list"}); err != nil {
		return nil, err
	}
	w.EventCapacity = o.Users / 2
	start := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Hour)
	r = adm.Do("POST", fmt.Sprintf("/api/v1/x/content.events/calendars/%d/events", w.Calendar), map[string]any{"title": "QA event with limited seats",
		"starts_at": start, "ends_at": start.Add(time.Hour), "tz": "UTC", "registration": true, "capacity": w.EventCapacity, "waitlist": true})
	w.EventDate = start.Format("2006-01-02")
	if err := must(r, 201, 200); err != nil {
		return nil, fmt.Errorf("shared event: %w", err)
	}
	var ev idObj
	_ = r.JSON(&ev)
	w.SharedEvent = ev.ID
	w.Workflow, err = element(w.Pages, "workflow.process", "QA approval", map[string]any{"description": "QA", "fields": []map[string]any{{"id": "subject", "label": "Subject", "type": "text"}},
		"steps": []map[string]any{
			{"id": "check", "name": "Check", "assignee": map[string]string{"type": "group", "value": "qa-editors"}, "actions": []map[string]any{{"id": "ok", "label": "Approve", "next": "done"}}},
			{"id": "done", "name": "Done", "actions": []any{}, "outcome": "success"}}})
	if err != nil {
		return nil, err
	}
	r = adm.Do("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", w.Workflow), map[string]any{"data": map[string]any{"subject": "shared"}})
	if err := must(r, 201, 200); err != nil {
		return nil, fmt.Errorf("shared case: %w", err)
	}
	var cs idObj
	_ = r.JSON(&cs)
	w.SharedCase = cs.ID
	return w, nil
}

// discover lists the elements and categories of the team area.
func (w *World) discover(adm *Client) error {
	var list struct {
		Items []struct {
			ID         int64  `json:"id"`
			Type       string `json:"type"`
			CategoryID int64  `json:"category_id"`
		} `json:"items"`
	}
	r := adm.Do("GET", fmt.Sprintf("/api/v1/elements?category=%d&recursive=1&limit=500", w.Team), nil)
	if err := must(r, 200); err != nil {
		return err
	}
	_ = r.JSON(&list)
	w.Typed = map[string][]int64{}
	skip := map[int64]bool{w.SharedPage: true}
	for _, id := range w.CollabPages {
		skip[id] = true
	}
	seen := map[int64]bool{}
	for _, e := range list.Items {
		if !skip[e.ID] {
			w.Typed[e.Type] = append(w.Typed[e.Type], e.ID)
		}
		if !seen[e.CategoryID] {
			seen[e.CategoryID] = true
			w.TeamCats = append(w.TeamCats, e.CategoryID)
		}
	}
	return nil
}

func findTop(tree []catNode, prefix string) int64 {
	for _, n := range tree {
		if n.ParentID == nil && hasTitle(n, prefix) {
			return n.ID
		}
	}
	return 0
}

// removeArea deletes the QA area (trash and purge).
func removeArea(adm *Client) error {
	var tree []catNode
	_ = adm.Do("GET", "/api/v1/categories/tree", nil).JSON(&tree)
	for _, n := range tree {
		if n.ParentID == nil && hasTitle(n, "QA Suite") {
			if err := must(adm.Do("DELETE", fmt.Sprintf("/api/v1/categories/%d", n.ID), nil), 200, 204); err != nil {
				return fmt.Errorf("remove old QA area: %w", err)
			}
			_ = adm.Do("DELETE", fmt.Sprintf("/api/v1/trash/category/%d", n.ID), nil)
		}
	}
	return nil
}

// cleanup removes the QA area, accounts and groups.
func cleanup(o Options) error {
	adm, err := adminClient(o)
	if err != nil {
		return err
	}
	if err := removeArea(adm); err != nil {
		return err
	}
	for off := 0; ; {
		var us []struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		}
		r := adm.Do("GET", fmt.Sprintf("/api/v1/admin/users?q=qa-u&limit=200&offset=%d", off), nil)
		if r.JSON(&us) != nil || len(us) == 0 {
			break
		}
		n := 0
		for _, u := range us {
			if strings.HasPrefix(u.Username, "qa-u") || strings.HasPrefix(u.Username, "qa-pt-") {
				if adm.Do("DELETE", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), nil).Status < 300 {
					n++
				}
			}
		}
		if n == 0 {
			off += len(us)
		}
	}
	var gs []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	_ = adm.Do("GET", "/api/v1/groups", nil).JSON(&gs)
	for _, g := range gs {
		if g.Name == "qa-editors" || g.Name == "qa-readers" {
			_ = adm.Do("DELETE", fmt.Sprintf("/api/v1/admin/groups/%d", g.ID), nil)
		}
	}
	return nil
}
