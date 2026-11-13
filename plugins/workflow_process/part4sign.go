// Workflow 2, part 4: decisions with a signature. A step (or a single
// action) can require a signature – any method ("advanced"), a qualified
// one ("qualified") or a certain method (its plugin id). The record that is
// signed is built here from the case, never by the browser: workflow and
// version, case, step and when it was entered, the decision with note and
// the values of the step form, and all data of the case after it.
// Completing rebuilds the record from the current case and the submitted
// decision – if anything differs, the signature does not count.
package workflow_process

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/signature"
)

// SignPurpose is the purpose of decision signatures.
const SignPurpose = "workflow.decision"

var signMethodRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(\.[a-z0-9][a-z0-9_-]*)+$`)

func validSign(v string) bool {
	return v == "" || v == signature.LevelAdvanced || v == signature.LevelQualified || signMethodRe.MatchString(v)
}

// validateSign checks the signature settings of steps and actions.
func (d *Definition) validateSign() error {
	for i := range d.Steps {
		s := &d.Steps[i]
		if !validSign(s.Sign) {
			return apperr.BadRequest("step %q: signature must be advanced, qualified or a signature plugin", s.ID)
		}
		if s.Sign != "" && s.Type != StepTask && s.Type != StepApproval {
			return apperr.BadRequest("step %q: only decisions (tasks and approvals) can be signed", s.ID)
		}
		for j := range s.Actions {
			a := &s.Actions[j]
			if a.Sign != "" && a.Sign != "none" && !validSign(a.Sign) {
				return apperr.BadRequest("step %q, action %q: signature must be none, advanced, qualified or a signature plugin", s.ID, a.ID)
			}
		}
	}
	return nil
}

// signReq is the signature an action of a step needs ("" none).
func signReq(s *Step, a Action) string {
	if s == nil || isHandover(a.ID) || a.ID == ActRetry || a.ID == ActSkip {
		return ""
	}
	if s.Type == StepTask {
		for _, da := range s.Actions {
			if da.ID == a.ID {
				switch da.Sign {
				case "none":
					return ""
				case "":
					return s.Sign
				default:
					return da.Sign
				}
			}
		}
		return ""
	}
	if s.Type == StepApproval && (a.ID == "approve" || a.ID == "reject") {
		return s.Sign
	}
	return ""
}

// markSign sets the needed signature on the actions offered to a caller.
func markSign(s *Step, list []Action) {
	for i := range list {
		list[i].Sign = signReq(s, list[i])
	}
}

func caseRef(id int64) string { return "workflow_case:" + strconv.FormatInt(id, 10) }

// decisionRecord is what is signed for a decision.
func decisionRecord(x *Instance, s *Step, a Action, note, to string, form map[string]any, signer int64) map[string]any {
	data := maps.Clone(x.Data)
	if data == nil {
		data = map[string]any{}
	}
	maps.Copy(data, form)
	if form == nil {
		form = map[string]any{}
	}
	return map[string]any{
		"type":     SignPurpose,
		"workflow": map[string]any{"id": x.ElementID, "version": x.Rev, "title": pickTitle(x.Element, x.Lang)},
		"case":     map[string]any{"id": x.ID, "title": x.Title, "started_at": x.StartedAt.UTC().Format(time.RFC3339Nano)},
		"step":     map[string]any{"id": s.ID, "name": s.Name, "entered_at": x.StepEntered.UTC().Format(time.RFC3339Nano)},
		"decision": map[string]any{"action": a.ID, "label": a.Label, "note": note, "to": to},
		"form":     form,
		"data":     data,
		"signer":   signer,
	}
}

// signRoutes: start a signature for a decision.
func (pl *Plugin) signRoutes(host interface {
	Handle(method, path string, fn h.Handler)
}) {
	// POST /instances/{id}/sign {"action", "note", "to", "data", "method", "lang"}
	// → {"token", "challenge"}: sign the decision, then send it with
	// POST /instances/{id}/actions and {"signature": {"token", "answer"}}.
	host.Handle("POST", "/instances/{id}/sign", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Action string         `json:"action"`
			Note   string         `json:"note"`
			To     string         `json:"to"`
			Data   map[string]any `json:"data"`
			Method string         `json:"method"`
			Lang   string         `json:"lang"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		ctx := r.Context()
		p := identity.From(ctx)
		if p == nil || p.ID() == 0 {
			return nil, apperr.ErrUnauthorized
		}
		x, _, _, err := pl.load(ctx, id)
		if err != nil {
			return nil, err
		}
		if x.Status != "open" {
			return nil, apperr.Conflict("the case is already closed")
		}
		s := x.Definition.Step(x.Step)
		var a *Action
		for i := range x.Actions {
			if x.Actions[i].ID == in.Action {
				a = &x.Actions[i]
			}
		}
		if a == nil {
			return nil, apperr.BadRequest("you cannot take the action %q now", in.Action)
		}
		req := signReq(s, *a)
		if req == "" {
			return nil, apperr.BadRequest("this decision needs no signature")
		}
		note, err := clean(in.Note, 5000)
		if err != nil {
			return nil, err
		}
		if a.Note == "required" && note == "" {
			return nil, apperr.BadRequest("a note is required")
		}
		var form map[string]any
		if in.Action != "reject" {
			if form, err = stepData(&x.Definition, s, in.Data); err != nil {
				return nil, err
			}
		}
		token, ch, err := pl.host.Signatures.Begin(ctx, signature.Start{Purpose: SignPurpose, Ref: caseRef(x.ID), Method: in.Method, Require: req, Lang: in.Lang,
			Record: decisionRecord(x, s, *a, note, in.To, form, p.ID())})
		if err != nil {
			return nil, err
		}
		return map[string]any{"token": token, "challenge": ch, "require": req}, nil
	})
}

// signIn is the signature sent with a decision.
type signIn struct {
	Token  string          `json:"token"`
	Answer json.RawMessage `json:"answer"`
}

// completeSign checks the signature of a decision inside its transaction
// and returns the id of the stored evidence.
func (pl *Plugin) completeSign(ctx context.Context, tx pgx.Tx, cur *Instance, s *Step, a Action, note, to string, form map[string]any, sg *signIn, lang string) (int64, error) {
	if sg == nil || sg.Token == "" {
		return 0, &apperr.Error{Status: signature.ErrStatus, Code: "signature_required", Msg: "this decision needs a signature"}
	}
	p := identity.From(ctx)
	_, digest, err := signature.Canonical(decisionRecord(cur, s, a, note, to, form, p.ID()))
	if err != nil {
		return 0, err
	}
	id, _, err := pl.host.Signatures.Complete(ctx, tx, sg.Token, lang, sg.Answer, func(pd signature.Pending) error {
		if pd.Purpose != SignPurpose || pd.Ref != caseRef(cur.ID) {
			return apperr.BadRequest("the signature belongs to another decision")
		}
		if !bytes.Equal(pd.Digest, digest) {
			return apperr.Conflict("the decision or the case changed after signing – please sign again")
		}
		return nil
	})
	return id, err
}

// signedEvent links the last event of a case with its signature.
func signedEvent(ctx context.Context, tx pgx.Tx, id, sig int64) error {
	_, err := tx.Exec(ctx, `UPDATE plugin_workflow_events SET signature_id=$2
		WHERE id = (SELECT max(id) FROM plugin_workflow_events WHERE instance_id=$1)`, id, sig)
	return err
}

// signViewer: who may see a case may see the signatures of its decisions.
func (pl *Plugin) signViewer(ctx context.Context, ref string) bool {
	n, ok := strings.CutPrefix(ref, "workflow_case:")
	if !ok {
		return false
	}
	id, err := strconv.ParseInt(n, 10, 64)
	if err != nil {
		return false
	}
	_, _, _, err = pl.load(ctx, id)
	return err == nil
}

// deleteSignatures removes the signatures of deleted or anonymized cases.
func deleteSignatures(ctx context.Context, tx pgx.Tx, ids []int64) error {
	refs := make([]string, len(ids))
	for i, id := range ids {
		refs[i] = caseRef(id)
	}
	_, err := tx.Exec(ctx, `DELETE FROM signatures WHERE ref = ANY($1)`, refs)
	return err
}
