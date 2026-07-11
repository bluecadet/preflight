package target

import "context"

// ExecResult is the outcome of a TargetOps.Exec call: the script runs in the
// target's native shell (POSIX sh or PowerShell per TargetInfo.RuntimeKind)
// and returns separated stdout/stderr and the exit code.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// TargetOps is the backend a plugin handle binds to. Every transport
// implements it (Local now; SSH and WinRM follow). The plugin's target effects
// all flow through it — including against the local target — so plugins are
// brought in line with first-party modules. Info returns the same enriched
// TargetInfo delivered to a plugin at initialize.
type TargetOps interface {
	Exec(ctx context.Context, script string) (ExecResult, error)
	PutFile(ctx context.Context, path string, data []byte) error
	GetFile(ctx context.Context, path string) ([]byte, error)
	Info(ctx context.Context) (TargetInfo, error)
}
