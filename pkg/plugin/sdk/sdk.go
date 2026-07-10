// Package sdk provides the plugin author interface and helpers for implementing
// preflight plugins as standalone executables speaking JSON-RPC over stdin/stdout.
package sdk

import (
	"os"
)

// Module is the interface plugin authors implement. Check and Apply receive a
// Handle: ALL target effects flow through it, including against the local
// target. This brings plugins in line with first-party modules.
//
// One target op is in flight per session. For high-latency transports, batch
// work into a single script-shaped RunCommand instead of many round trips.
type Module interface {
	// Name returns the module's canonical name (e.g. "my-module").
	Name() string
	// Version returns the module's semantic version.
	Version() string
	// Check reports whether the system is already in the desired state.
	// NeedsChange must be true if the system is NOT yet in the desired state
	// (i.e., Apply should be called). Target effects go through h.
	Check(args map[string]any, h Handle) (CheckResult, error)
	// Apply brings the system into the desired state, using h for all target
	// effects.
	Apply(args map[string]any, h Handle) (ApplyResult, error)
}

// Serve runs the JSON-RPC loop for the given module, reading requests from
// stdin and writing responses to stdout. Call this from your plugin's main().
//
// The host delivers TargetInfo at initialize; the Handle given to Check/Apply
// exposes it (plus RunCommand/PutFile/GetFile/Output) by calling back over the
// same stdio channel — both sides act as JSON-RPC client and server.
func Serve(m Module) {
	serveIO(m, os.Stdin, os.Stdout)
}
