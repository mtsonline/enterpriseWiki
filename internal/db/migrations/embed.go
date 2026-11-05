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
