// Package migrations embeds the core database migrations.
package migrations

import "embed"

// FS contains the core SQL migrations (NNNN_name.sql).
//
//go:embed *.sql
var FS embed.FS
