package target

import "context"

// OutputFunc is a callback invoked with each line of output emitted by a module during Apply.
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

// TargetInfo holds basic facts about a target machine.
type TargetInfo struct {
	Hostname  string
	OSVersion string
	OSBuild   string
	Arch      string
}

// Target is the central abstraction for all operations against a machine.
// The runner is always injected with a Target and never assumes local execution.
type Target interface {
	// Execute runs a named module with the given params against the target.
	// If dryRun is true, only Check() is called — no changes are made.
	Execute(ctx context.Context, taskID string, module string, params map[string]any, dryRun bool, onOutput OutputFunc) (Result, error)

	// CopyFile copies a local file to dst on the target.
	CopyFile(ctx context.Context, src, dst string) error

	// ReadFile reads a file from the target and returns its contents.
	ReadFile(ctx context.Context, path string) ([]byte, error)

	// Reachable reports whether the target can be contacted.
	Reachable(ctx context.Context) (bool, error)

	// Info returns basic facts about the target machine.
	Info(ctx context.Context) (TargetInfo, error)
}
