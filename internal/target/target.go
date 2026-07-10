package target

import "context"

// OutputFunc is a callback invoked with each line of output emitted by a module during execution.
type OutputFunc func(line string)

// NoOutput is a nil OutputFunc that can be passed to modules which do not
// produce streaming output. Passing nil directly is equivalent; this named
// form makes the intent explicit at call sites and in tests.
var NoOutput OutputFunc

// Status represents the outcome of a task execution.
type Status string

const (
	StatusOK      Status = "ok"
	StatusChanged Status = "changed"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

// Module is the interface implemented by all built-in modules, plugin
// adapters, and remote-execution closures. A single contract replaces what
// used to be three parallel ones (in-process Module, target.remoteModule,
// optional CheckStreamingModule / StreamingModule upgrades).
//
// Output streaming is part of the main signature: implementations call out(line)
// for each line they want to surface; callers pass nil when they do not care.
// Modules that do not stream simply ignore the OutputFunc.
type Module interface {
	Check(ctx context.Context, params map[string]any, out OutputFunc) (CheckResult, error)
	Apply(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error)
}

// CheckResult is the outcome of a module's Check method.
type CheckResult struct {
	// NeedsChange reports whether Apply must run to bring the system to the
	// desired state.
	NeedsChange bool
	// Message is an optional human-readable summary. When non-empty it
	// overrides the runner's default status message ("already in desired
	// state" or "would apply change (dry-run)").
	Message string
}

// ApplyResult is the outcome of a module's Apply method.
type ApplyResult struct {
	// Message is an optional human-readable summary. When non-empty it
	// overrides the runner's default "change applied" message.
	Message string
}

// EnsureModule is an optional capability for modules that can combine Check
// and Apply into a single round trip. Worthwhile on high-latency transports
// (e.g. WinRM) where two separate invocations double overhead. Implementations
// may return ErrEnsureNotHandled to fall back to the standard Check+Apply
// path (e.g. when params don't support the fast path).
type EnsureModule interface {
	Module
	Ensure(ctx context.Context, params map[string]any, dryRun bool, out OutputFunc) (EnsureResult, error)
}

// EnsureResult is the outcome of an ensure single-round-trip operation.
type EnsureResult struct {
	// Changed reports whether the module made (or, in dry-run, would have
	// made) a change.
	Changed bool
	// Message is an optional human-readable summary.
	Message string
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
//
// POSIX fields (OSName, PackageManager, Init) are populated by the cached
// runtime detection probe and are empty on Windows. Plugins receive this
// struct so they branch on enriched OS facts without re-detecting.
type TargetInfo struct {
	Hostname       string
	OSVersion      string
	OSBuild        string
	OSName         string // os-release ID on POSIX; friendly name on Windows
	Arch           string
	OSFamily       OSFamily
	PackageManager string // apt | dnf | "" (POSIX only)
	Init           string // systemd | "" (POSIX only)
	Transport      Transport
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
}

// PowerShellRunner is implemented by targets that can execute an inline
// PowerShell script. Callers that need PowerShell (e.g. Windows fact
// gathering) consult this capability rather than assuming every target
// supports it. Non-Windows transports that genuinely cannot reach a
// PowerShell host need not implement it.
type PowerShellRunner interface {
	RunPowerShell(ctx context.Context, script string) (string, error)
}

// RoundTripCounter is optionally implemented by targets that can report the
// number of transport round-trips made during execution. The count is surfaced
// in the run log and under -v as a performance-tuning observable.
type RoundTripCounter interface {
	RoundTripCount() int64
}
