package stdlib

import "embed"

// FS holds the embedded standard library actions.
//
//go:embed all:actions
var FS embed.FS
