package stdlib

import "embed"

// FS holds the embedded standard library actions.
//
//go:embed actions/preflight/*/action.yml
var FS embed.FS
