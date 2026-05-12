package target

import "context"

// OutputFunc is a callback invoked with each line of output emitted by a module during execution.
type OutputFunc func(line string)

// Status represents the outcome of a task execution.
type Status string

const (
	StatusOK      Status = "ok"
	StatusChanged Status = "changed"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

// Module is the interface implemented by all built-in modules.
type Module interface {
	Check(ctx context.Context, params map[string]any) (needed bool, err error)
	Apply(ctx context.Context, params map[string]any) error
}

// PluggableModule is implemented by modules that delegate to an out-of-process
// adapter (typically a preflight plugin executable). Targets clone these
// per-instance so each target gets its own adapter client state, and transports
// that cannot delegate to an external process consult this interface for
// clearer "not supported on this transport" diagnostics.
type PluggableModule interface {
	Module
	// PluginPath is the path to the backing plugin executable.
	PluginPath() string
	// CloneModule returns a fresh adapter that owns no shared client state.
	CloneModule() Module
}

// CheckStreamingModule is an optional extension of Module for implementations
// that can emit output line-by-line during Check.
type CheckStreamingModule interface {
	Module
	CheckWithOutput(ctx context.Context, params map[string]any, onOutput OutputFunc) (needed bool, err error)
}

// StreamingModule is an optional extension of Module for implementations
// that can emit output line-by-line during Apply.
type StreamingModule interface {
	Module
	ApplyWithOutput(ctx context.Context, params map[string]any, onOutput OutputFunc) error
}

// Result holds the outcome of a single task execution.
type Result struct {
	TaskID  string
	Status  Status
	Message string
	Output  []string
	Error   error
}

// Transport identifies how the controller reaches a target.
type Transport string

const (
	TransportLocal Transport = "local"
	TransportSSH   Transport = "ssh"
	TransportWinRM Transport = "winrm"
)

// OSFamily is a normalized operating-system family used for behavior checks.
type OSFamily string

const (
	OSFamilyUnknown OSFamily = "unknown"
	OSFamilyWindows OSFamily = "windows"
	OSFamilyLinux   OSFamily = "linux"
	OSFamilyDarwin  OSFamily = "darwin"
)

// TargetInfo holds basic facts about a target machine.
type TargetInfo struct {
	Hostname  string
	OSVersion string
	OSBuild   string
	Arch      string
	OSFamily  OSFamily
	Transport Transport
}

// IsLocal reports whether the target is the controller machine.
func (i TargetInfo) IsLocal() bool {
	return i.Transport == TransportLocal
}

// IsWindows reports whether the target belongs to the Windows OS family.
func (i TargetInfo) IsWindows() bool {
	return i.OSFamily == OSFamilyWindows
}

// Target is the central abstraction for all operations against a machine.
// The runner is always injected with a Target and never assumes local execution.
type Target interface {
	// Execute runs a named module with the given params against the target.
	// If dryRun is true, only Check() is called — no changes are made.
	Execute(ctx context.Context, taskID string, module string, params map[string]any, opts ExecutionOptions, dryRun bool, onOutput OutputFunc) (Result, error)

	// Info returns basic facts about the target machine.
	Info(ctx context.Context) (TargetInfo, error)

	// Transport returns the connection type used to reach the target.
	Transport() Transport

	// RunPowerShell executes an inline PowerShell script against the target.
	RunPowerShell(ctx context.Context, script string) (string, error)
}
