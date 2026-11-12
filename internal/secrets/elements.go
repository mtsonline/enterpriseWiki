package secrets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
)

// Secrets of element bodies (signing keys of webhooks in workflows and
// forms) are kept outside of the body: whoever may edit or export a page
// must not read them. The body holds a reference ("sec_…"); the value is
// stored (sealed) in element_secrets. A reference belongs to the element it
// was first saved with: a copied body (duplicate, template) does not carry
// the secret along – it has to be entered again.

func init() { Register(Column{Table: "element_secrets", Key: "ref", Value: "value"}) }

var refRe = regexp.MustCompile(`^sec_[0-9a-f]{32}$`)

// ValidRef reports whether ref looks like a reference.
func ValidRef(ref string) bool { return refRe.MatchString(ref) }

// ErrMissing: the element has no secret under that reference.
var ErrMissing = errors.New("the secret is missing (enter it again, e.g. after copying)")

// PutRef stores a new secret of the person uid (not yet bound to an
// element) and returns its reference.
func PutRef(ctx context.Context, q DB, b *Box, uid int64, value string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	ref := "sec_" + hex.EncodeToString(raw)
	v, err := b.Seal([]byte(value), AAD("element_secrets", ref))
	if err != nil {
		return "", err
	}
	var by *int64
	if uid > 0 {
		by = &uid
	}
	_, err = q.Exec(ctx, `INSERT INTO element_secrets(ref, value, created_by) VALUES ($1,$2,$3)`, ref, v, by)
	return ref, err
}

// BindRefs binds the new references of a body to element (only those the
// person uid created and that belong to no element yet).
func BindRefs(ctx context.Context, q DB, element, uid int64, refs []string) error {
	if len(refs) == 0 {
		return nil
	}
	var by *int64
	if uid > 0 {
		by = &uid
	}
	_, err := q.Exec(ctx, `UPDATE element_secrets SET element_id=$1 WHERE ref = ANY($2) AND element_id IS NULL AND created_by IS NOT DISTINCT FROM $3`,
		element, refs, by)
	return err
}

// GetRef returns the secret ref of element.
func GetRef(ctx context.Context, q DB, b *Box, element int64, ref string) (string, error) {
	if !ValidRef(ref) {
		return "", ErrMissing
	}
	var v []byte
	err := q.QueryRow(ctx, `SELECT value FROM element_secrets WHERE ref=$1 AND element_id=$2`, ref, element).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrMissing
	}
	if err != nil {
		return "", err
	}
	out, err := b.Open(v, AAD("element_secrets", ref))
	return string(out), err
}

// Cleanup removes secrets that were never saved with an element.
func Cleanup(ctx context.Context, q DB) error {
	_, err := q.Exec(ctx, `DELETE FROM element_secrets WHERE element_id IS NULL AND created_at < now() - interval '1 day'`)
	return err
}
