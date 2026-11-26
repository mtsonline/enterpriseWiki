package workflow_process

// The step "Shell command" (approved by the project owner on 26.11.2026,
// concept "Workflow-Schritte Shell und Datei"). It shares running and
// approving commands with the shell macro (internal/shellcmd):
//
//   - the command text is fixed and needs the approval of a superadmin,
//     bound to its SHA-256; saving a version with a new command asks for the
//     approval at once, and the same text keeps its approval;
//   - values of the case reach the command only as environment variables
//     (EWIKI_FIELD_<NAME>) of the fields the step names, never as text in the
//     command, so nobody can inject commands through a form;
//   - a case waits on the step until the command is approved (it looks
//     again every minute); a revoked command fails the step;
//   - exit code and output go into fields of the case; an exit code other
//     than 0 leads to the step "otherwise" or, without one, fails the step
//     (retries, then a manager decides);
//   - a test run never runs the command (nothing leaves the wiki).

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/notify"
	"github.com/mtsonline/enterprisewiki/internal/shellcmd"
)

// ShellSpec runs an approved shell command.
type ShellSpec struct {
	Command string `json:"command"`
	// Fields: fields of the case the command gets as environment variables.
	Fields []string `json:"fields,omitempty"`
	// Timeout in seconds (default 30), at most plugins.macros.shell_timeout
	// of config.yaml.
	Timeout int `json:"timeout,omitempty"`
	// Output and Exit: fields that receive the output (at most
	// shellOutputMax characters) and the exit code.
	Output string `json:"output,omitempty"`
	Exit   string `json:"exit,omitempty"`
}

const (
	shellFieldsMax  = 20
	shellOutputMax  = 20000
	shellValueMax   = 10000
	shellTimeoutMax = 300
	// shellRecheck: how often a case asks again whether its command was
	// approved.
	shellRecheck = time.Minute
)

// validateShellStep checks the step and normalizes its command.
func (s *Step) validateShellStep(sh *ShellSpec, fieldType map[string]string) error {
	if sh == nil {
		return apperr.BadRequest("step %q: the command is missing", s.ID)
	}
	v := *sh
	v.Command = shellcmd.Normalize(v.Command)
	if v.Command == "" || len(v.Command) > shellcmd.MaxCommand {
		return apperr.BadRequest("step %q: the command must have 1 to %d characters", s.ID, shellcmd.MaxCommand)
	}
	if len(v.Fields) > shellFieldsMax {
		return apperr.BadRequest("step %q: at most %d fields for the command", s.ID, shellFieldsMax)
	}
	// every field gets its own variable: "a-b" and "a_b" would both be
	// EWIKI_FIELD_A_B, and the command would see only one of them
	vars := map[string]string{}
	for _, f := range v.Fields {
		if _, ok := fieldType[f]; !ok {
			return apperr.BadRequest("step %q: unknown field %q", s.ID, f)
		}
		name := shellcmd.EnvName(f)
		if other, ok := vars[name]; ok {
			return apperr.BadRequest("step %q: the fields %q and %q would have the same variable %s", s.ID, other, f, name)
		}
		vars[name] = f
	}
	for _, f := range []string{v.Output, v.Exit} {
		if f != "" && !idRe.MatchString(f) {
			return apperr.BadRequest("step %q: field %q must consist of a-z, 0-9, _ and -", s.ID, f)
		}
	}
	if v.Timeout == 0 {
		v.Timeout = 30
	}
	if v.Timeout < 1 || v.Timeout > shellTimeoutMax {
		return apperr.BadRequest("step %q: the time limit must be 1 to %d seconds", s.ID, shellTimeoutMax)
	}
	s.Shell = &v
	return nil
}

// requestShell asks for the approval of the commands of a saved version
// right away; the approvers are told about each new command.
func (pl *Plugin) requestShell(ctx context.Context, tx pgx.Tx, elementID int64, d *Definition) error {
	for i := range d.Steps {
		s := &d.Steps[i]
		if s.Type != StepShell || s.Shell == nil {
			continue
		}
		added, err := shellcmd.Request(ctx, tx, s.Shell.Command, elementID, "workflow:"+s.ID)
		if err != nil {
			return err
		}
		if added {
			if err := pl.tellApprovers(ctx, tx, elementID, s); err != nil {
				return err
			}
		}
	}
	return nil
}

// tellApprovers notifies the superadmins of shell commands (config
// plugins.macros.shell_approvers) about a command waiting for approval.
func (pl *Plugin) tellApprovers(ctx context.Context, tx pgx.Tx, elementID int64, s *Step) error {
	handles := pl.host.Config.Get().Plugins.Macros.ShellApprovers
	if len(handles) == 0 {
		return nil
	}
	var ids []int64
	rows, err := tx.Query(ctx, `SELECT id FROM users WHERE NOT disabled AND lower(username || '@' || provider) = ANY($1)`, lower(handles))
	if err != nil {
		return err
	}
	ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil || len(ids) == 0 {
		return err
	}
	step := s.Name
	_, err = pl.host.Notify.SendQ(ctx, tx, ids, notify.Message{Kind: "workflow.process.shell_approval", EntityType: "element", EntityID: elementID,
		Link: "/admin/shell",
		Text: func(lang string) (string, string) {
			params := map[string]string{"step": step}
			return pl.host.T(ctx, lang, "workflow.process.shell_approval_subject", params), pl.host.T(ctx, lang, "workflow.process.shell_approval_body", params)
		}})
	return err
}

func lower(list []string) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = strings.ToLower(v)
	}
	return out
}

// runShell carries out the step: waits for the approval, then runs the
// command with the chosen fields in its environment.
func (pl *Plugin) runShell(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (result, error) {
	cfg := pl.host.Config.Get().Plugins.Macros
	if !cfg.ShellEnabled {
		return result{}, fmt.Errorf("shell commands are switched off on this server (plugins.macros.shell_enabled)")
	}
	sh := s.Shell
	hash := shellcmd.Hash(sh.Command)
	status, err := shellcmd.Status(ctx, tx, hash)
	if err != nil {
		return result{}, err
	}
	switch status {
	case "": // a version saved before the command was known (or deleted since)
		added, err := shellcmd.Request(ctx, tx, sh.Command, x.ElementID, "workflow:"+s.ID)
		if err != nil {
			return result{}, err
		}
		if added {
			if err := pl.tellApprovers(ctx, tx, x.ElementID, s); err != nil {
				return result{}, err
			}
		}
		return result{wait: true, recheck: shellRecheck}, nil
	case shellcmd.Pending:
		return result{wait: true, recheck: shellRecheck}, nil
	case shellcmd.Revoked:
		return result{}, fmt.Errorf("the shell command was revoked by a superadmin")
	}
	var env []string
	for _, f := range sh.Fields {
		v := dataString(x, f)
		if utf8.RuneCountInString(v) > shellValueMax {
			v = string([]rune(v)[:shellValueMax])
		}
		env = append(env, shellcmd.EnvName(f)+"="+strings.ReplaceAll(v, "\x00", ""))
	}
	limit := time.Duration(sh.Timeout) * time.Second
	if d, err := time.ParseDuration(cfg.ShellTimeout); err == nil && d > 0 && d < limit {
		limit = d
	}
	start := time.Now()
	out, exit, runErr := shellcmd.Run(ctx, sh.Command, env, limit, cfg.ShellUser)
	ms := int(time.Since(start).Milliseconds())
	if err := shellcmd.Ran(ctx, tx, hash, exit, ms); err != nil {
		return result{}, err
	}
	// who ran what, without the output (it may hold personal data)
	if err := audit.Log(ctx, tx, "workflow.shell", "workflow_case", x.ID, nil,
		map[string]any{"hash": hash, "step": s.ID, "exit": exit, "ms": ms}); err != nil {
		return result{}, err
	}
	if runErr != nil {
		return result{}, runErr
	}
	if utf8.RuneCountInString(out) > shellOutputMax {
		out = string([]rune(out)[:shellOutputMax])
	}
	if sh.Output != "" || sh.Exit != "" {
		if sh.Output != "" {
			x.Data[sh.Output] = out
		}
		if sh.Exit != "" {
			x.Data[sh.Exit] = strconv.Itoa(exit)
		}
		if err := saveData(ctx, tx, x); err != nil {
			return result{}, err
		}
	}
	label := "exit " + strconv.Itoa(exit)
	if exit != 0 {
		if s.NextElse == "" {
			return result{}, fmt.Errorf("the command ended with exit code %d", exit)
		}
		return result{next: s.NextElse, action: "shell", label: label}, nil
	}
	return result{next: s.Next, action: "shell", label: label}, nil
}
