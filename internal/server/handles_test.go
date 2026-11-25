package server_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestHandlesOnlyForWhoMayLookUp: next to content (last change, history,
// comments, trash, cases, deputies) a person is named with the display name;
// the handle (user@provider, with OIDC often the e-mail address) only for
// readers who may look the person up – never for guests.
func TestHandlesOnlyForWhoMayLookUp(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	const pw = "Passw0rd!Passw0rd"
	var team idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Handles team"}, &team, 201)
	ids := map[string]int64{}
	for _, u := range []struct {
		name, display string
		groups        []int64
	}{{"carla", "Carla", []int64{team.ID}}, {"mate", "Mate", []int64{team.ID}}, {"out", "Out", []int64{}}} {
		var id idResp
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": u.name, "password": pw, "display_name": u.display, "email": u.name + "@example.com", "groups": u.groups}, &id, 201)
		ids[u.name] = id.ID
	}
	carla, mate, out, guest := e.client(), e.client(), e.client(), e.client()
	carla.login("carla", pw)
	mate.login("mate", pw)
	out.login("out", pw)

	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Public"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 15, "inherit": true}, nil, 201)
	var el elem
	carla.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Notes", "body": htmlBody("<p>x</p>")}, &el, 201)
	carla.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", el.ID), map[string]any{"body": "Hello"}, nil, 201)

	const handle = "carla@local"
	withHandle, plain := "Carla ("+handle+")", "Carla"
	type view struct {
		UpdatedBy string `json:"updated_by"`
	}
	type rev struct {
		Author string `json:"author"`
	}
	check := func(who string, c *client, want string) {
		t.Helper()
		var v view
		c.must("GET", fmt.Sprintf("/api/v1/elements/%d", el.ID), nil, &v, 200)
		if v.UpdatedBy != want {
			t.Errorf("%s: updated_by %q, want %q", who, v.UpdatedBy, want)
		}
		var revs []rev
		c.must("GET", fmt.Sprintf("/api/v1/elements/%d/revisions", el.ID), nil, &revs, 200)
		if len(revs) == 0 || revs[0].Author != want {
			t.Errorf("%s: revisions %+v, want %q", who, revs, want)
		}
		var one rev
		c.must("GET", fmt.Sprintf("/api/v1/elements/%d/revisions/en/1", el.ID), nil, &one, 200)
		if one.Author != want {
			t.Errorf("%s: revision %q, want %q", who, one.Author, want)
		}
		var cm struct {
			Comments []struct {
				Author string `json:"author"`
			} `json:"comments"`
		}
		c.must("GET", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", el.ID), nil, &cm, 200)
		if len(cm.Comments) != 1 || cm.Comments[0].Author != want {
			t.Errorf("%s: comments %+v, want %q", who, cm.Comments, want)
		}
	}
	check("guest", guest, plain)
	check("outside the groups", out, plain)
	check("same group", mate, withHandle)
	check("the person", carla, withHandle)
	check("admin", admin, withHandle)

	// lists of pages never carry the handle
	var list struct {
		Items []view `json:"items"`
	}
	for _, c2 := range []*client{guest, mate} {
		c2.must("GET", fmt.Sprintf("/api/v1/elements?category=%d", c.ID), nil, &list, 200)
		if len(list.Items) != 1 || list.Items[0].UpdatedBy != plain {
			t.Errorf("list: %+v", list.Items)
		}
	}

	// trash: who deleted it
	carla.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", el.ID), nil, nil, 204)
	trash := func(c *client) string {
		t.Helper()
		var items []struct {
			ID        int64  `json:"id"`
			DeletedBy string `json:"deleted_by"`
		}
		c.must("GET", "/api/v1/trash", nil, &items, 200)
		for _, it := range items {
			if it.ID == el.ID {
				return it.DeletedBy
			}
		}
		t.Fatalf("not in the trash: %+v", items)
		return ""
	}
	if got := trash(out); got != plain {
		t.Errorf("trash for out: %q", got)
	}
	if got := trash(mate); got != withHandle {
		t.Errorf("trash for mate: %q", got)
	}

	// cases: who started them, who acted, the deputy
	var w cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Flows"}}, &w, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", w.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	def := map[string]any{"description": "", "steps": []map[string]any{
		{"id": "check", "name": "Check", "assignee": map[string]string{"type": "user", "value": "out"},
			"actions": []map[string]any{{"id": "ok", "label": "Agree", "next": "done"}}},
		{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Flow", "body": def}, &wf, 201)
	var x struct {
		ID        int64  `json:"id"`
		StartedBy string `json:"started_by"`
	}
	carla.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{}}, &x, 201)
	if x.StartedBy != withHandle {
		t.Errorf("own case: %q", x.StartedBy)
	}
	var tasks struct {
		Tasks []struct {
			ID        int64  `json:"id"`
			StartedBy string `json:"started_by"`
		} `json:"tasks"`
	}
	out.must("GET", "/api/v1/x/workflow.process/tasks", nil, &tasks, 200)
	if len(tasks.Tasks) != 1 || tasks.Tasks[0].StartedBy != plain {
		t.Errorf("task of out: %+v", tasks.Tasks)
	}
	var acted struct {
		StartedBy string `json:"started_by"`
		Events    []struct {
			Actor string `json:"actor"`
		} `json:"events"`
	}
	out.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", x.ID), map[string]any{"action": "ok"}, nil, 200)
	carla.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", x.ID), nil, &acted, 200)
	for _, ev := range acted.Events {
		if strings.Contains(ev.Actor, "out@local") {
			t.Errorf("carla sees the handle of out: %+v", acted.Events)
		}
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", x.ID), nil, &acted, 200)
	found := false
	for _, ev := range acted.Events {
		found = found || ev.Actor == "Out (out@local)"
	}
	if !found {
		t.Errorf("admin: actors %+v", acted.Events)
	}

	// deputy: out names carla; out may not look carla up
	out.must("PUT", "/api/v1/x/workflow.process/me/deputy", map[string]any{"deputy": "carla@local", "from": time.Now().Format("2006-01-02"), "until": time.Now().AddDate(0, 0, 30).Format("2006-01-02")}, nil, 200)
	var dep struct {
		Deputy struct {
			Deputy string `json:"deputy"`
		} `json:"deputy"`
	}
	out.must("GET", "/api/v1/x/workflow.process/me/deputy", nil, &dep, 200)
	if dep.Deputy.Deputy != plain {
		t.Errorf("deputy for out: %q", dep.Deputy.Deputy)
	}
	var stand struct {
		StandIn []struct {
			Name string `json:"name"`
		} `json:"stand_in_for"`
	}
	carla.must("GET", "/api/v1/x/workflow.process/me/deputy", nil, &stand, 200)
	if len(stand.StandIn) != 1 || stand.StandIn[0].Name != "Out" {
		t.Errorf("stand-in for carla: %+v", stand.StandIn)
	}

	// an account deleted later: its stored handle is not shown in the history
	admin.must("DELETE", fmt.Sprintf("/api/v1/admin/users/%d", ids["out"]), nil, nil, 204)
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", x.ID), nil, &acted, 200)
	for _, ev := range acted.Events {
		if strings.Contains(ev.Actor, "out@local") {
			t.Errorf("deleted account: %+v", acted.Events)
		}
	}
}
