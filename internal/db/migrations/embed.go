// Package migrations embeds the core database migrations.
package migrations

import (
	"embed"
	"io/fs"
)

// FS contains the core SQL migrations of a site schema (NNNN_name.sql).
//
//go:embed *.sql
var FS embed.FS

//go:embed shared/*.sql
var shared embed.FS

// Shared contains the core SQL migrations of the shared schema.
var Shared, _ = fs.Sub(shared, "shared")

//go:embed identity/*.sql
var identity embed.FS

// Identity contains the account tables (users, groups, roles, SSH keys):
// in the shared schema and in the schema of sites with separate accounts.
var Identity, _ = fs.Sub(identity, "identity")
