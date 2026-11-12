package mail

import (
	"context"
	"errors"
	"regexp"
	"sync"

	"github.com/jackc/pgx/v5"
)

// Hidden replaces the secret part of a redacted text.
const Hidden = "[hidden]"

var (
	redactMu   sync.RWMutex
	redactions []*regexp.Regexp
)

// RegisterRedaction adds a pattern for secrets in message texts (e.g. the
// tokens of personal decision links). The first group is kept, the rest of
// the match is replaced by Hidden. Such secrets are sent to the recipient
// but never shown in the outbox and removed from the database once the
// message is finished.
func RegisterRedaction(re *regexp.Regexp) {
	redactMu.Lock()
	defer redactMu.Unlock()
	for _, x := range redactions {
		if x.String() == re.String() {
			return
		}
	}
	redactions = append(redactions, re)
}

// Redact hides the registered secrets of a text.
func Redact(s string) string {
	redactMu.RLock()
	defer redactMu.RUnlock()
	for _, re := range redactions {
		if re.NumSubexp() > 0 {
			s = re.ReplaceAllString(s, "${1}"+Hidden)
		} else {
			s = re.ReplaceAllLiteralString(s, Hidden)
		}
	}
	return s
}

// scrubFinished removes the secrets from the stored text of a message that
// has no pending recipient any more.
func scrubFinished(ctx context.Context, tx pgx.Tx, id int64) error {
	var body string
	err := tx.QueryRow(ctx, `SELECT body_text FROM mail_outbox WHERE id=$1 AND status IN ('sent','failed','partial')`, id).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if r := Redact(body); r != body {
		_, err = tx.Exec(ctx, `UPDATE mail_outbox SET body_text=$2 WHERE id=$1`, id, r)
	}
	return err
}
