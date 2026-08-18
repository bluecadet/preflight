// Package sdk provides the plugin author interface and helpers for implementing
// preflight plugins as standalone executables speaking JSON-RPC over stdin/stdout.
package sdk

import (
	"context"
	"os"
)

// Module is the interface plugin authors implement. Check and Apply receive a
// Handle: ALL target effects flow through it, including against the local
// target. This brings plugins in line with first-party modules.
//
// They also receive a context.Context scoped to that one operation. It is a
// real cancellation signal, not a placeholder: the host sends a protocol-level
// cancel notification when it abandons the call (run timeout, interrupt), and
// the SDK cancels the context this side of the process boundary. Pass it to
// every handle op — an op issued with a background context cannot be
// interrupted, and the plugin will be torn down mid-flight instead of
// unwinding cleanly. See CancelGrace for the window a cancelled call has to
// return.
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
	// (i.e., Apply should be called). Target effects go through h, and ctx
	// must be passed to each of them so they can be interrupted.
	Check(ctx context.Context, args map[string]any, h Handle) (CheckResult, error)
	// Apply brings the system into the desired state, using h for all target
	// effects and ctx for cancellation.
	Apply(ctx context.Context, args map[string]any, h Handle) (ApplyResult, error)
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
