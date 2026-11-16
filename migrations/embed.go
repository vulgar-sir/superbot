package migrations

import "embed"

// FS contains all SQL migrations so the API can apply them at startup.
//
//go:embed *.sql
var FS embed.FS
