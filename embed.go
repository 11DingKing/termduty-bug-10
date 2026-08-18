// Package termduty is the module root. It embeds the SQL migration scripts so
// the store layer can apply them without depending on files at a fixed path.
package termduty

import "embed"

// Migrations holds the committed schema migration scripts.
//
//go:embed migrations/*.sql
var Migrations embed.FS
