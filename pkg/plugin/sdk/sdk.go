// Package sdk provides the plugin author interface and helpers for implementing
// preflight plugins as standalone executables speaking JSON-RPC over stdin/stdout.
package sdk

// CheckResult is returned by a module's Check method.
type CheckResult struct {
	// NeedsChange must be true if the system is NOT yet in the desired state
	// (i.e., Apply should be called). Return false when the system is already
	// in the desired state and no action is required.
	NeedsChange bool           `json:"needs_change"`
	State       map[string]any `json:"state"`
	Error       string         `json:"error,omitempty"`
}

// ApplyResult is returned by a module's Apply method.
type ApplyResult struct {
	State map[string]any `json:"state"`
	Error string         `json:"error,omitempty"`
}

// Module is the interface plugin authors implement.
type Module interface {
	// Name returns the module's canonical name (e.g. "my-module").
	Name() string
	// Version returns the module's semantic version.
	Version() string
	// Check reports whether the system is already in the desired state.
	// NeedsChange must be true if the system is NOT yet in the desired state
	// (i.e., Apply should be called). Return false when no change is required.
	Check(args map[string]any) (CheckResult, error)
	// Apply brings the system into the desired state.
	Apply(args map[string]any) (ApplyResult, error)
}

// Serve runs the JSON-RPC loop for the given module.
// Call this from your plugin's main().
func Serve(m Module) {
	serve(m)
}
