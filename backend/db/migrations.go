// Package migrations embeds the plain-SQL goose migrations so the binary can run
// them at startup without the .sql files present on disk (ADR-0004). Migrations
// stay embedded even though the frontend is served from real files (ADR-0005).
package migrations

import "embed"

// FS holds the embedded migration files under the migrations/ subdirectory.
//
//go:embed migrations/*.sql
var FS embed.FS
