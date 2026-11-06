package workflow_process

// Permissions of workflows (declared in plugin.json):
//
//   - workflow.process.create: create workflows and change or delete the
//     own ones (with their steps and assignees);
//   - workflow.process.admin: change and delete all workflows of the
//     category – every change is logged in detail and the creator of the
//     workflow is notified;
//   - workflow.process.cases: see all cases, cancel them and decide about
//     failed steps.
//
// Who may carry out a step is defined only by the assignee of the step.

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/mail"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

const (
	permCreate = "workflow.process.create"
	permAdmin  = "workflow.process.admin"
	permCases  = "workflow.process.cases"
)

// guardHosts are the hosts of the sites, set in Setup (the element type is
// registered once per process, the plugin once per site).
var guardHosts sync.Map // site name -> *sdk.Host

// guardHost returns the host of the site of ctx (nil before Setup).
func guardHost(ctx context.Context) *sdk.Host {
	if h, ok := guardHosts.Load(sdk.SiteOf(ctx)); ok {
		return h.(*sdk.Host)
	}
	return nil
}

// CheckCreate implements content.Guard.
func (Type) CheckCreate(ctx context.Context, _ *content.Service, categoryID int64) error {
	gh := guardHost(ctx)
	if gh == nil {
		return nil
	}
	if gh.Can(ctx, categoryID, permCreate) || gh.Can(ctx, categoryID, permAdmin) {
		return nil
	}
	return apperr.Forbidden("creating workflows needs the permission %q in this category", "Create workflows")
}

// CheckChange implements content.Guard: the creator (with permCreate) or a
// workflow administrator (permAdmin; logged in detail, creator notified).
func (Type) CheckChange(ctx context.Context, _ *content.Service, el *content.Element, oldBody, newBody json.RawMessage) error {
	gh := guardHost(ctx)
	if gh == nil {
		return nil
	}
	p := identity.From(ctx)
	own := p != nil && el.CreatedByID != nil && *el.CreatedByID == p.ID()
	if own && (gh.Can(ctx, el.CategoryID, permCreate) || gh.Can(ctx, el.CategoryID, permAdmin)) {
		return nil
	}
	if !gh.Can(ctx, el.CategoryID, permAdmin) {
		return apperr.Forbidden("only the creator of the workflow or a workflow administrator may change it")
	}
	// workflow administrator changes a workflow of someone else
	changes := diffDefinitions(oldBody, newBody)
	action := "workflow.admin_change"
	if newBody == nil {
		action, changes = "workflow.admin_delete", []string{"deleted or moved"}
	}
	if err := audit.Log(ctx, gh.DB, action, "element", el.ID, map[string]any{"creator": el.CreatedByID}, map[string]any{"changes": changes}); err != nil {
		return err
	}
	if el.CreatedByID != nil && len(changes) > 0 {
		notifyCreator(ctx, el, p, changes)
	}
	return nil
}

// diffDefinitions describes the differences of two workflow definitions
// (steps added, removed or changed, with assignees).
func diffDefinitions(oldBody, newBody json.RawMessage) []string {
	var a, b Definition
	_ = json.Unmarshal(oldBody, &a)
	_ = json.Unmarshal(newBody, &b)
	out := []string{}
	byID := func(d Definition) map[string]Step {
		m := map[string]Step{}
		for _, s := range d.Steps {
			m[s.ID] = s
		}
		return m
	}
	oa, ob := byID(a), byID(b)
	for _, s := range b.Steps {
		old, ok := oa[s.ID]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("step %q (%s) added, assignee %s", s.Name, s.ID, assigneeText(s.Assignee)))
		case !reflect.DeepEqual(old.Assignee, s.Assignee):
			out = append(out, fmt.Sprintf("step %q (%s): assignee %s → %s", s.Name, s.ID, assigneeText(old.Assignee), assigneeText(s.Assignee)))
		case !reflect.DeepEqual(old, s):
			out = append(out, fmt.Sprintf("step %q (%s) changed", s.Name, s.ID))
		}
	}
	for _, s := range a.Steps {
		if _, ok := ob[s.ID]; !ok {
			out = append(out, fmt.Sprintf("step %q (%s) removed", s.Name, s.ID))
		}
	}
	if !reflect.DeepEqual(a.Fields, b.Fields) {
		out = append(out, "form fields changed")
	}
	if a.Description != b.Description {
		out = append(out, "description changed")
	}
	return out
}

func assigneeText(a Assignee) string {
	if a.Value == "" {
		return a.Type
	}
	return a.Type + " " + a.Value
}

// notifyCreator tells the creator of a workflow that an administrator changed it.
func notifyCreator(ctx context.Context, el *content.Element, by *identity.Principal, changes []string) {
	h := guardHost(ctx)
	if h == nil || h.Mail == nil || !h.Mail.Enabled() {
		return
	}
	us, err := h.Mail.Users(ctx, h.DB, []int64{*el.CreatedByID})
	if err != nil || len(us) == 0 || us[0].Email == "" {
		return
	}
	u := us[0]
	title := el.Titles[el.OriginalLang]
	link := fmt.Sprintf("%s/e/%d", strings.TrimSuffix(h.Config.Get().Server.BaseURL, "/"), el.ID)
	who := "–"
	if by != nil {
		who = by.DisplayName
		if who == "" {
			who = by.Handle()
		}
	}
	params := map[string]string{"name": u.Name, "title": title, "by": who, "changes": "- " + strings.Join(changes, "\n- "), "link": link}
	m := mail.Message{To: []string{u.Email}, Subject: h.T(ctx, u.Lang, "workflow.mail_admin_change_subject", params),
		Text: h.T(ctx, u.Lang, "workflow.mail_admin_change_body", params) + "\n\n" + link + "\n", Source: "workflow", Ref: fmt.Sprintf("element:%d", el.ID), Owner: -1}
	if _, err := h.Mail.Enqueue(ctx, h.DB, m); err != nil {
		h.Log.Error("notify workflow creator", "err", err)
	}
}
