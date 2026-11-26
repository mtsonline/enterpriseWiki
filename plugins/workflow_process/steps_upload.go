package workflow_process

// The step "Upload or confirm a file" (approved by the project owner on
// 26.11.2026, concept "Workflow-Schritte Shell und Datei"): a task of a
// person or group with one fixed purpose.
//
//   - upload: the assignee adds a file to a file field of the case (for
//     example a signed contract) with the button of the step; optionally
//     the file goes into a category of the media library as well, with the
//     rights of the person who saved the workflow (or who started the case);
//   - confirm: the assignee sees a file of the case (from the start form,
//     an earlier upload or a generated document) and confirms or rejects it,
//     a rejection with a reason; optionally with a signature, like other
//     decisions.
//
// Both are tasks in every other respect (assignees, reminders, deadlines,
// hand-overs, "My tasks"): validation builds the actions and the form of the
// step from the spec, and the engine treats the step like a task.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/media"
)

// Modes of the upload step.
const (
	UploadMode  = "upload"
	ConfirmMode = "confirm"
)

// Actions of the upload step.
const (
	ActUpload  = "upload"
	ActConfirm = "confirm"
	ActReject  = "reject"
)

// UploadSpec is the purpose of an upload step.
type UploadSpec struct {
	Mode  string `json:"mode"`  // upload | confirm
	Field string `json:"field"` // a field of the type file
	// Label of the button: upload (default text of the designer), confirm.
	Label string `json:"label"`
	// RejectLabel: the button to reject (confirm only).
	RejectLabel string `json:"reject_label,omitempty"`
	// Media: upload only – also put the file into this category of the
	// media library (0: only in the case).
	Media int64 `json:"media,omitempty"`
}

// validateUploadStep checks the spec and builds the actions and the form
// of the step. assignee checks who handles it (shared with tasks).
func (s *Step) validateUploadStep(fieldType map[string]string) error {
	u := s.Upload
	if u == nil {
		return apperr.BadRequest("step %q: choose upload or confirm and the file field", s.ID)
	}
	if !in(u.Mode, UploadMode, ConfirmMode) {
		return apperr.BadRequest("step %q: the mode must be upload or confirm", s.ID)
	}
	if fieldType[u.Field] != "file" {
		return apperr.BadRequest("step %q: %q is not a file field of the workflow", s.ID, u.Field)
	}
	var err error
	if u.Label, err = clean(u.Label, 100); err != nil || u.Label == "" {
		return apperr.BadRequest("step %q: the label of the button is required", s.ID)
	}
	if s.Next == "" {
		return apperr.BadRequest("step %q: the next step is required", s.ID)
	}
	switch u.Mode {
	case UploadMode:
		if u.Media < 0 {
			return apperr.BadRequest("step %q: invalid category of the media library", s.ID)
		}
		u.RejectLabel, s.NextElse = "", ""
		s.Actions = []Action{{ID: ActUpload, Label: u.Label, Next: s.Next, Note: "optional"}}
		s.Form = []StepField{{Field: u.Field, Mode: "required"}}
	case ConfirmMode:
		u.Media = 0
		if u.RejectLabel, err = clean(u.RejectLabel, 100); err != nil || u.RejectLabel == "" {
			return apperr.BadRequest("step %q: the label of the button to reject is required", s.ID)
		}
		if s.NextElse == "" {
			return apperr.BadRequest("step %q: the step after a rejection is required", s.ID)
		}
		s.Actions = []Action{{ID: ActConfirm, Label: u.Label, Next: s.Next, Note: "optional"},
			{ID: ActReject, Label: u.RejectLabel, Next: s.NextElse, Note: "required"}}
		s.Form = []StepField{{Field: u.Field, Mode: "read"}}
	}
	return nil
}

// storeUpload puts the uploaded file of the step into the media library,
// if the step says so: with the rights of the definer (or starter), inside
// the transaction of the decision, so the decision fails if this fails.
func (pl *Plugin) storeUpload(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) error {
	u := s.Upload
	if u == nil || u.Mode != UploadMode || u.Media == 0 {
		return nil
	}
	m, ok := x.Data[u.Field].(map[string]any)
	hash, _ := m["hash"].(string)
	if !ok || !hashRe.MatchString(hash) {
		return nil
	}
	name, _ := m["name"].(string)
	pctx, err := pl.runAs(ctx, tx, x, s)
	if err != nil {
		return err
	}
	f, err := pl.host.Files.Open(hash)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	// the media library checks the right to upload into the category
	_, err = pl.host.Media.Upload(pctx, media.UploadInput{CategoryID: u.Media, Name: name, Title: name,
		Description: fmt.Sprintf("%s · #%d", x.Title, x.ID)}, f)
	return err
}
