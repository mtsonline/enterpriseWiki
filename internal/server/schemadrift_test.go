package server_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/db"
)

// TestSchemaDrift: a database of an older development state (before the
// first release new columns go into the 0001 files without migrations)
// is found at start: the wiki refuses to run on it instead of failing
// later with a 500. A database without differences is accepted, also
// when it does not know the content of its files yet (older versions).
func TestSchemaDrift(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	// an older version did not record the files: the check runs once and
	// accepts the schema
	if _, err := e.app.DB.Exec(ctx, `DELETE FROM schema_files`); err != nil {
		t.Fatal(err)
	}
	if err := e.app.MigrateDB(ctx); err != nil {
		t.Fatalf("an unchanged schema was refused: %v", err)
	}
	var n int
	if err := e.app.DB.QueryRow(ctx, `SELECT count(*) FROM schema_files`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("files not recorded: %d %v", n, err)
	}
	// recorded and unchanged: no check, nothing to say
	if err := e.app.MigrateDB(ctx); err != nil {
		t.Fatal(err)
	}
	// a column and a table that an older 0001 did not have
	if _, err := e.app.DB.Exec(ctx, `ALTER TABLE demo_objects DROP COLUMN created_at; DROP TABLE feed_cache CASCADE;
		UPDATE schema_files SET sha256 = 'older' WHERE source = 'core'`); err != nil {
		t.Fatal(err)
	}
	err := e.app.MigrateDB(ctx)
	var drift *db.SchemaDriftError
	if !errors.As(err, &drift) {
		t.Fatalf("an older schema was not found: %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "column demo_objects.created_at") || !strings.Contains(msg, "table feed_cache") || !strings.Contains(msg, "Refusing to start") {
		t.Fatalf("message: %s", msg)
	}
	// the check leaves nothing behind
	if err := e.app.DB.QueryRow(ctx, `SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'ewiki_check_%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("temporary schemas left: %d %v", n, err)
	}
}
