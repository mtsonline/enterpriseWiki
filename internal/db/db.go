// Package db provides the PostgreSQL connection pool and the migration runner
// used by the core and by plugins.
package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SharedSchema holds the tables used by all sites (users, groups, roles,
// sessions, sites).
const SharedSchema = "public"

var siteNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)

// ValidSiteName reports whether name can be used as a site name.
func ValidSiteName(name string) bool { return siteNameRe.MatchString(name) }

// SiteSchema returns the schema of a site.
func SiteSchema(site string) string { return "site_" + site }

// Open creates a connection pool for the shared schema and verifies
// connectivity.
func Open(ctx context.Context, url string, maxConns int) (*pgxpool.Pool, error) {
	return OpenSchema(ctx, url, maxConns, "")
}

// OpenSite creates a connection pool for a site: its schema comes first in
// the search path, the shared schema second. The schema is created if needed.
func OpenSite(ctx context.Context, url string, maxConns int, site string) (*pgxpool.Pool, error) {
	if !ValidSiteName(site) {
		return nil, fmt.Errorf("invalid site name %q", site)
	}
	return OpenSchema(ctx, url, maxConns, SiteSchema(site))
}

// OpenSchema creates a connection pool whose search path starts with schema
// (empty: shared schema only).
func OpenSchema(ctx context.Context, url string, maxConns int, schema string) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, errors.New("database url is empty (set database.url or EWIKI_DATABASE_URL)")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = int32(min(maxConns, 10000)) // #nosec G115 -- bounded
	}
	cfg.MaxConnIdleTime = 5 * time.Minute
	if schema != "" {
		cfg.ConnConfig.RuntimeParams["search_path"] = schema + ", " + SharedSchema
	} else {
		cfg.ConnConfig.RuntimeParams["search_path"] = SharedSchema
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(cctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if schema != "" {
		if _, err := pool.Exec(cctx, `CREATE SCHEMA IF NOT EXISTS `+pgx.Identifier{schema}.Sanitize()); err != nil {
			pool.Close()
			return nil, fmt.Errorf("create schema %s: %w", schema, err)
		}
	}
	return pool, nil
}

// DropSite removes the schema of a site with all its data.
func DropSite(ctx context.Context, q Querier, site string) error {
	if !ValidSiteName(site) {
		return fmt.Errorf("invalid site name %q", site)
	}
	_, err := q.Exec(ctx, `DROP SCHEMA IF EXISTS `+pgx.Identifier{SiteSchema(site)}.Sanitize()+` CASCADE`)
	return err
}

// Schema returns the first schema of the connection's search path.
func Schema(ctx context.Context, q Querier) (string, error) {
	var s string
	err := q.QueryRow(ctx, `SELECT current_schema()`).Scan(&s)
	return s, err
}

// ResetForTests drops the shared schema and all site schemas (tests only).
func ResetForTests(ctx context.Context, q Querier) error {
	rows, err := q.Query(ctx, `SELECT nspname FROM pg_namespace WHERE nspname LIKE 'site\_%'`)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		names = append(names, n)
	}
	rows.Close()
	for _, n := range names {
		if _, err := q.Exec(ctx, `DROP SCHEMA `+pgx.Identifier{n}.Sanitize()+` CASCADE`); err != nil {
			return err
		}
	}
	_, err = q.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`)
	return err
}

// Querier is implemented by *pgxpool.Pool, *pgx.Conn and pgx.Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Tx runs fn inside a transaction; it commits on nil error and rolls back otherwise.
func Tx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after commit
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsUniqueViolation reports whether err is a unique constraint violation.
func IsUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

// IsForeignKeyViolation reports whether err is a foreign key violation.
func IsForeignKeyViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23503"
}

// MigrationSource is a named set of migrations (core or a plugin).
// Files must be named NNNN_description.sql and are applied in order, each in
// its own transaction. Applied migrations are never re-run or modified.
type MigrationSource struct {
	Name  string
	Files fs.FS
}

var migName = regexp.MustCompile(`^(\d{4,})_[a-z0-9_]+\.sql$`)

const migrationLock = 7300417 // arbitrary advisory lock id

// Migrate applies all pending migrations of the given sources.
func Migrate(ctx context.Context, pool *pgxpool.Pool, sources []MigrationSource) ([]string, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLock); err != nil {
		return nil, err
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLock) }() // also released with the connection
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		source text NOT NULL,
		version int NOT NULL,
		name text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (source, version))`); err != nil {
		return nil, err
	}
	var applied []string
	for _, src := range sources {
		if src.Files == nil {
			continue
		}
		entries, err := fs.ReadDir(src.Files, ".")
		if err != nil {
			return applied, fmt.Errorf("migrations %s: %w", src.Name, err)
		}
		type mig struct {
			v    int
			name string
		}
		var migs []mig
		for _, e := range entries {
			m := migName.FindStringSubmatch(e.Name())
			if e.IsDir() || m == nil {
				continue
			}
			v, _ := strconv.Atoi(m[1])
			migs = append(migs, mig{v, e.Name()})
		}
		sort.Slice(migs, func(i, j int) bool { return migs[i].v < migs[j].v })
		for _, m := range migs {
			var exists bool
			if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE source=$1 AND version=$2)`, src.Name, m.v).Scan(&exists); err != nil {
				return applied, err
			}
			if exists {
				continue
			}
			sqlb, err := fs.ReadFile(src.Files, path.Clean(m.name))
			if err != nil {
				return applied, err
			}
			tx, err := conn.Begin(ctx)
			if err != nil {
				return applied, err
			}
			if _, err := tx.Exec(ctx, string(sqlb)); err != nil {
				_ = tx.Rollback(ctx)
				return applied, fmt.Errorf("migration %s/%s: %w", src.Name, m.name, err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(source, version, name) VALUES ($1,$2,$3)`, src.Name, m.v, m.name); err != nil {
				_ = tx.Rollback(ctx)
				return applied, err
			}
			if err := tx.Commit(ctx); err != nil {
				return applied, err
			}
			applied = append(applied, src.Name+"/"+m.name)
		}
	}
	return applied, nil
}

// CountActiveAccounts counts the enabled accounts of the whole instance: the
// shared accounts plus the own accounts of sites with separate accounts.
func CountActiveAccounts(ctx context.Context, q Querier) (int, error) {
	rows, err := q.Query(ctx, `SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relname = 'users' AND c.relkind = 'r' AND (n.nspname = 'public' OR n.nspname LIKE 'site\_%')`)
	if err != nil {
		return 0, err
	}
	var schemas []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return 0, err
		}
		schemas = append(schemas, s)
	}
	rows.Close()
	total := 0
	for _, s := range schemas {
		var n int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{s, "users"}.Sanitize()+` WHERE NOT disabled`).Scan(&n); err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}
