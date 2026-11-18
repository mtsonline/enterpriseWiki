package server_test

import (
	"fmt"
	"testing"

	"github.com/mtsonline/enterprisewiki/plugins/content_forms"
)

// TestDashboardHomeAndStatus covers the option "dashboard as the start page"
// and the state of the wiki for administrators (GET admin/status).
func TestDashboardHomeAndStatus(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)

	// the option: off by default, kept by the settings form, shown in the site info
	var site struct {
		DashboardHome bool `json:"dashboard_home"`
	}
	admin.must("GET", "/api/v1/site", nil, &site, 200)
	if site.DashboardHome {
		t.Fatal("dashboard_home is on by default")
	}
	var s struct {
		Config struct {
			UI map[string]any `json:"ui"`
		} `json:"config"`
	}
	admin.must("GET", "/api/v1/admin/settings", nil, &s, 200)
	s.Config.UI["dashboard_home"] = true
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": s.Config.UI}, nil, 200)
	admin.must("GET", "/api/v1/site", nil, &site, 200)
	if !site.DashboardHome {
		t.Fatal("dashboard_home not saved")
	}
	// another setting keeps it
	admin.must("GET", "/api/v1/admin/settings", nil, &s, 200)
	s.Config.UI["site_name"] = "Renamed"
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": s.Config.UI}, nil, 200)
	admin.must("GET", "/api/v1/site", nil, &site, 200)
	if !site.DashboardHome {
		t.Fatal("dashboard_home lost when saving another setting")
	}

	// the state of the wiki: only for administrators
	if code := e.client().do("GET", "/api/v1/admin/status", nil, nil); code != 401 {
		t.Fatalf("guest: %d", code)
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "mia", "password": "Mia-Passw0rd!x"}, nil, 201)
	mia := e.client()
	mia.login("mia", "Mia-Passw0rd!x")
	if code := mia.do("GET", "/api/v1/admin/status", nil, nil); code != 403 {
		t.Fatalf("member: %d", code)
	}
	type status struct {
		Version         string   `json:"version"`
		LoginFailed     int      `json:"login_failed_24h"`
		MailFailed      *int     `json:"mail_failed"`
		QueuePending    *int     `json:"queue_pending"`
		PluginsInactive []string `json:"plugins_inactive"`
		LicenseLevel    string   `json:"license_level"`
		DatabaseLevel   string   `json:"database_level"`
	}
	var before, after status
	admin.must("GET", "/api/v1/admin/status", nil, &before, 200)
	if before.Version == "" || before.MailFailed == nil || before.QueuePending == nil || before.PluginsInactive == nil || before.LicenseLevel == "" || before.DatabaseLevel == "" {
		t.Fatalf("status incomplete: %+v", before)
	}
	// a failed sign-in is counted
	if code := e.client().do("POST", "/api/v1/auth/login", map[string]any{"username": "mia", "password": "wrong"}, nil); code != 401 {
		t.Fatalf("wrong password: %d", code)
	}
	admin.must("GET", "/api/v1/admin/status", nil, &after, 200)
	if after.LoginFailed != before.LoginFailed+1 {
		t.Fatalf("failed sign-ins: %d → %d", before.LoginFailed, after.LoginFailed)
	}
}

// TestRecentSubmissions covers the forms with the latest submissions (tile of
// the dashboard): only forms whose submissions the caller sees, as for the
// list of one form.
func TestRecentSubmissions(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()
	old := content_forms.MinAge
	content_forms.MinAge = 0
	t.Cleanup(func() { content_forms.MinAge = old })

	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Forms"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rita", "password": "Rita-Passw0rd!x"}, &u, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 1 | 2 | 4, "actions": []string{"content.forms.create"}, "inherit": true}, nil, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "carl", "password": "Carl-Passw0rd!x"}, nil, 201)
	rita, carl := e.client(), e.client()
	rita.login("rita", "Rita-Passw0rd!x")
	carl.login("carl", "Carl-Passw0rd!x")

	def := map[string]any{"fields": []map[string]any{{"id": "name", "type": "text", "label": "Name"}}, "targets": map[string]any{"store": true}}
	var adminForm, ritaForm elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.forms", "title": "Admin form", "body": def}, &adminForm, 201)
	rita.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.forms", "title": "Rita form", "body": def}, &ritaForm, 201)
	submit := func(id int64) {
		t.Helper()
		var st formStart
		guest.must("GET", fmt.Sprintf("/api/v1/x/content.forms/elements/%d/start", id), nil, &st, 200)
		if code, body := guest.submitForm(t, id, map[string]any{"token": st.Token, "values": map[string]any{"name": "Ann"}}, nil); code != 201 && code != 200 {
			t.Fatalf("submit: %d %s", code, body)
		}
	}
	submit(adminForm.ID)
	submit(adminForm.ID)
	submit(ritaForm.ID)

	type item struct {
		ElementID int64             `json:"element_id"`
		Titles    map[string]string `json:"titles"`
		Unseen    int               `json:"unseen"`
	}
	recent := func(cl *client) map[int64]int {
		t.Helper()
		var list []item
		cl.must("GET", "/api/v1/x/content.forms/submissions/recent", nil, &list, 200)
		out := map[int64]int{}
		for _, x := range list {
			out[x.ElementID] = x.Unseen
		}
		return out
	}
	if got := recent(admin); got[adminForm.ID] != 2 || got[ritaForm.ID] != 1 {
		t.Fatalf("admin: %v", got)
	}
	// the creator sees her form only
	if got := recent(rita); len(got) != 1 || got[ritaForm.ID] != 1 {
		t.Fatalf("creator: %v", got)
	}
	// viewing the forms is not enough
	if got := recent(carl); len(got) != 0 {
		t.Fatalf("viewer: %v", got)
	}
	if code := guest.do("GET", "/api/v1/x/content.forms/submissions/recent", nil, nil); code != 401 {
		t.Fatalf("guest: %d", code)
	}
	// with the permission to see submissions
	var me struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	carl.must("GET", "/api/v1/auth/me", nil, &me, 200)
	var rule struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID),
		map[string]any{"subject_type": "user", "subject_id": me.User.ID, "permissions": 1, "actions": []string{"content.forms.submissions"}, "inherit": true}, &rule, 201)
	if got := recent(carl); len(got) != 2 {
		t.Fatalf("with the permission: %v", got)
	}
	// opening a submission marks it as seen
	var subs struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/content.forms/elements/%d/submissions", adminForm.ID), nil, &subs, 200)
	admin.must("GET", fmt.Sprintf("/api/v1/x/content.forms/submissions/%d", subs.Items[0].ID), nil, nil, 200)
	if got := recent(admin); got[adminForm.ID] != 1 {
		t.Fatalf("after opening one: %v", got)
	}
	// a form rita may no longer see disappears, though she created it
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d/acl", ritaForm.ID), map[string]any{"own_rights_only": true}, nil, 200)
	if got := recent(rita); len(got) != 0 {
		t.Fatalf("creator without the right to view: %v", got)
	}
	// deleted forms are not listed
	admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", adminForm.ID), nil, nil, 204)
	if got := recent(admin); got[adminForm.ID] != 0 || len(got) != 1 {
		t.Fatalf("after deleting: %v", got)
	}
}
