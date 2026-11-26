package server_test

import (
	"context"
	"fmt"
	"testing"
)

// TestWorkflowUploadStep: the step "Upload or confirm a file" (decision of
// the project owner of 26.11.2026). Uploading needs the file and puts it
// into the case and, if the step says so, into the media library with the
// rights of the definer; confirming shows the file, a rejection needs a
// reason and leads to the step after a rejection. Only the assignees act;
// file fields stay closed in the forms of other steps.
func TestWorkflowUploadStep(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.client()
	admin.login("admin", adminPW)
	var w, archive cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Contracts"}}, &w, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Signed contracts"}}, &archive, 201)
	fields := []map[string]any{{"id": "title", "label": "Title", "type": "text"}, {"id": "contract", "label": "Contract", "type": "file"}}
	steps := []map[string]any{
		{"id": "sign", "name": "Upload the signed contract", "type": "upload", "assignee": map[string]any{"type": "starter"}, "next": "check",
			"upload": map[string]any{"mode": "upload", "field": "contract", "label": "Upload", "media": archive.ID}},
		{"id": "check", "name": "Check it", "type": "upload", "assignee": map[string]any{"type": "starter"}, "next": "done", "next_else": "sign",
			"upload": map[string]any{"mode": "confirm", "field": "contract", "label": "Looks good", "reject_label": "Not signed"}},
		{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}
	// a file field in the form of an ordinary task stays closed; the field of an upload step must be a file field
	bad := []map[string]any{
		{"id": "t", "name": "T", "type": "task", "assignee": map[string]any{"type": "starter"}, "form": []map[string]any{{"field": "contract", "mode": "required"}},
			"actions": []map[string]any{{"id": "ok", "label": "OK", "next": "done"}}},
		{"id": "done", "name": "Done", "type": "end"}}
	if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Bad",
		"body": map[string]any{"fields": fields, "steps": bad}}, nil); code != 400 {
		t.Fatalf("file field in a task form: %d", code)
	}
	wrongField := []map[string]any{{"id": "sign", "name": "S", "type": "upload", "assignee": map[string]any{"type": "starter"}, "next": "done",
		"upload": map[string]any{"mode": "upload", "field": "title", "label": "Upload"}}, {"id": "done", "name": "Done", "type": "end"}}
	if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Bad",
		"body": map[string]any{"fields": fields, "steps": wrongField}}, nil); code != 400 {
		t.Fatalf("upload into a text field: %d", code)
	}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Contracts",
		"body": map[string]any{"fields": fields, "steps": steps}}, &wf, 201)
	var c wfCase
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{"title": "Lease"}}, &c, 201)
	act := func(body map[string]any, want int) wfCase {
		t.Helper()
		var x wfCase
		admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", c.ID), body, &x, want)
		return x
	}
	get := func() wfCase {
		t.Helper()
		var x wfCase
		admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", c.ID), nil, &x, 200)
		return x
	}
	if x := get(); x.Step != "sign" || len(x.Actions) == 0 || x.Actions[0].ID != "upload" {
		t.Fatalf("upload step: %+v", x)
	}
	// without the file: refused
	act(map[string]any{"action": "upload"}, 400)
	code, f := admin.upload(t, "lease.pdf", MinimalPDF("signed lease"))
	if code != 201 && code != 200 {
		t.Fatalf("upload: %d", code)
	}
	file := map[string]any{"hash": f.Hash, "name": "lease.pdf", "mime": f.Mime, "size": f.Size}
	act(map[string]any{"action": "upload", "data": map[string]any{"contract": file}}, 200)
	x := get()
	stored, _ := x.Data["contract"].(map[string]any)
	if x.Step != "check" || stored["hash"] != f.Hash {
		t.Fatalf("after the upload: %+v", x)
	}
	var inMedia int
	if err := e.app.DB.QueryRow(ctx, `SELECT count(*) FROM media WHERE category_id=$1 AND name='lease.pdf'`, archive.ID).Scan(&inMedia); err != nil || inMedia != 1 {
		t.Fatalf("not in the media library: %d %v", inMedia, err)
	}
	// confirm: the file is shown (read only), a rejection needs a reason
	if len(x.Actions) < 2 || x.Actions[0].ID != "confirm" || x.Actions[1].ID != "reject" {
		t.Fatalf("confirm step: %+v", x)
	}
	act(map[string]any{"action": "reject"}, 400)
	act(map[string]any{"action": "reject", "note": "The signature is missing."}, 200)
	if x := get(); x.Step != "sign" {
		t.Fatalf("after a rejection: %+v", x)
	}
	act(map[string]any{"action": "upload", "data": map[string]any{"contract": file}}, 200)
	act(map[string]any{"action": "confirm"}, 200)
	if x := get(); x.Status != "done" || x.Outcome != "success" {
		t.Fatalf("after confirming: %+v", x)
	}
	// somebody who is not assigned cannot act
	const pw = "Passw0rd!Passw0rd"
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "olaf", "password": pw, "display_name": "Olaf"}, nil, 201)
	olaf := e.client()
	olaf.login("olaf", pw)
	var c2 wfCase
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{"title": "Second"}}, &c2, 201)
	if code := olaf.do("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", c2.ID), map[string]any{"action": "upload", "data": map[string]any{"contract": file}}, nil); code != 403 && code != 404 {
		t.Fatalf("not assigned: %d", code)
	}
}
