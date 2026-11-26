// Package shellcmd is what the shell macro (plugin macro.shell) and the
// workflow step "Shell command" (plugin workflow.process) share: running a
// command safely and the approval of commands.
//
// Security model:
//   - off unless plugins.macros.shell_enabled is set in config.yaml (the web
//     interface cannot change it);
//   - every command must be approved by a superadmin (a user listed in
//     plugins.macros.shell_approvers, or the local command line). The
//     approval is bound to the SHA-256 of the exact command text: any change
//     needs a new approval, the same text keeps its approval;
//   - values of pages or cases never go into the command text, only into
//     environment variables the caller names (EnvName);
//   - commands run with a timeout, a minimal environment, in a temporary
//     directory, as an unprivileged user (plugins.macros.shell_user when the
//     wiki runs as root) and their output is size limited.
//
// The approvals are stored in the table plugin_shell_commands of the plugin
// macro.shell (its migrations), one row per command for all its uses.
package shellcmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// MaxOutput is the maximum number of bytes kept from a command's output.
const MaxOutput = 64 << 10

// MaxCommand is the longest command text.
const MaxCommand = 4000

// Approval states of a command.
const (
	Pending  = "pending"
	Approved = "approved"
	Revoked  = "revoked"
)

// Hash returns the approval hash of a command (after Normalize).
func Hash(command string) string {
	sum := sha256.Sum256([]byte(command))
	return hex.EncodeToString(sum[:])
}

// Normalize removes \r and surrounding blank space so that copying a
// command between systems does not change its hash.
func Normalize(cmd string) string {
	return strings.TrimSpace(strings.ReplaceAll(cmd, "\r\n", "\n"))
}

// CanApprove reports whether p is a superadmin for shell commands: the
// local command line (not --as USER: that carries only the user's rights),
// or an administrator listed in approvers, signed in (not with an API key).
func CanApprove(p *identity.Principal, approvers []string) bool {
	if p == nil {
		return false
	}
	if p.IsCLI() {
		return true
	}
	return (p.Admin() || perm.HasP(p, "macro.shell.manage")) && p.Via != "apikey" && slices.Contains(approvers, p.Handle())
}

// Request records a command as waiting for approval, unless it is known
// already (then nothing changes, whatever its state). source says where it
// is used: "page" (the macro in the page element) or "workflow:<step id>"
// (a step of the workflow element). It reports whether the
// command was new, so the approvers are told only once.
func Request(ctx context.Context, q db.Querier, cmd string, element int64, source string) (bool, error) {
	var el *int64
	if element != 0 {
		el = &element
	}
	var uid *int64
	if id := identity.From(ctx).ID(); id > 0 {
		uid = &id
	}
	tag, err := q.Exec(ctx, `INSERT INTO plugin_shell_commands(hash, command, element_id, requested_by, source) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (hash) DO NOTHING`, Hash(cmd), cmd, el, uid, source)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// Status returns the approval state of a command ("" if it is unknown).
func Status(ctx context.Context, q db.Querier, hash string) (string, error) {
	var status string
	err := q.QueryRow(ctx, `SELECT status FROM plugin_shell_commands WHERE hash=$1`, hash).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return status, err
}

// Ran records the last run of a command (for the list of commands).
func Ran(ctx context.Context, q db.Querier, hash string, exit, ms int) error {
	_, err := q.Exec(ctx, `UPDATE plugin_shell_commands SET last_run_at=now(), last_exit=$2, last_ms=$3 WHERE hash=$1`, hash, exit, ms)
	return err
}

// EnvName is the name of the environment variable that carries a value:
// EWIKI_FIELD_ and the name in capitals, other characters as "_".
func EnvName(field string) string {
	var b strings.Builder
	b.WriteString("EWIKI_FIELD_")
	for _, r := range field {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(unicode.ToUpper(r))
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
