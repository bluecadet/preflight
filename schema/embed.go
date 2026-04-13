package schema

import "embed"

// FS exposes the repository's JSON Schema files for runtime validation.
//
//go:embed *.json
var FS embed.FS
