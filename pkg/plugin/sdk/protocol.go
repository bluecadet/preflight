package sdk

import "context"

// ProtocolVersion is the wire-protocol version this host and SDK speak.
// Plugins must echo it back in their initialize response; a mismatch or
// absence is a plugin_protocol error (pre-v1 plugins are rejected).
const ProtocolVersion = "1"

// TargetInfo is the enriched target context delivered to a plugin at
// initialize. Absent signals are empty strings, never missing keys, so plugin
// code can branch on them with simple equality. RuntimeKind tells the plugin
// which shell RunCommand speaks (posix-sh or windows-powershell).
type TargetInfo struct {
	Family         string `json:"family"`
	Name           string `json:"name"`
	Version        string `json:"version"`
	Arch           string `json:"arch"`
	Hostname       string `json:"hostname"`
	PackageManager string `json:"package_manager"`
	Init           string `json:"init"`
	RuntimeKind    string `json:"runtime_kind"`
}

// CommandResult is the outcome of a RunCommand handle op: the script runs in
// the target's native shell and returns separated stdout/stderr and the exit
// code.
type CommandResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// InitializeParams is sent by the host in the initialize request.
type InitializeParams struct {
	ProtocolVersion string     `json:"protocol_version"`
	Target          TargetInfo `json:"target"`
}

// InitializeResult is the plugin's initialize response. ProtocolVersion must
// equal ProtocolVersion or the host rejects the plugin with a
// plugin_protocol error.
type InitializeResult struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocol_version"`
}

// CheckResult is returned by a module's Check method.
type CheckResult struct {
	NeedsChange bool   `json:"needs_change"`
	Message     string `json:"message,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ApplyResult is returned by a module's Apply method.
type ApplyResult struct {
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// OutputFunc is called for each line of streaming output emitted during Check
// or Apply. On the host side it forwards plugin output notifications to the
// runner; on the plugin side it is exposed through Handle.Output.
type OutputFunc func(line string)

// Handle is given to a plugin's Check/Apply. ALL target effects flow through
// it — including against the local target — so plugins are brought in line
// with first-party modules. The three target primitives are RunCommand,
// PutFile/GetFile, and TargetInfo (delivered at initialize and cached). Output
// carries streaming lines back to the host.
//
// One target op is in flight per session: a plugin that calls RunCommand must
// wait for its result before issuing another op (or a PutFile/GetFile). For
// high-latency transports, batch work into a single script-shaped RunCommand
// rather than many round trips.
//
// File transfer is whole-file in v1: PutFile/GetFile carry the entire payload
// as a single base64-encoded JSON-RPC frame buffered in memory. Chunked
// streaming for large files is deferred to v2; keep payloads small (a few MB
// at most).
type Handle interface {
	HandleServer
	// Info returns the TargetInfo delivered at initialize.
	Info() TargetInfo
	// Output emits a streaming line back to the host's output channel.
	Output(line string)
}

// HandleServer is the host-side backend a Client binds to. Every transport
// (Local, SSH-POSIX, SSH-Windows, WinRM) implements it; the Client dispatches
// plugin handle-op requests to it. Handle embeds this so the subset
// relationship between handle ops and the full plugin Handle stays explicit.
type HandleServer interface {
	// RunCommand executes script in the target's native shell (POSIX sh or
	// PowerShell per TargetInfo.RuntimeKind) and returns stdout, stderr, and
	// the exit code. This is the batching lever for high-latency transports:
	// prefer one script that does several things over several ops.
	RunCommand(ctx context.Context, script string) (CommandResult, error)
	// PutFile writes data to path on the target. File transfer is whole-file
	// in v1: the payload is base64-encoded into a single JSON-RPC frame
	// buffered in memory; chunked streaming is deferred to v2.
	PutFile(ctx context.Context, path string, data []byte) error
	// GetFile reads the contents of path from the target (whole-file, v1).
	GetFile(ctx context.Context, path string) ([]byte, error)
}

// noopHandleServer returns a typed error for every op. Used by inspection paths
// (plugin list/info/staging) that have no target: a plugin inspected only for
// its name/version never invokes handle ops, so this is never called in
// practice, but it keeps the Client contract non-nil.
type noopHandleServer struct{}

func (noopHandleServer) RunCommand(context.Context, string) (CommandResult, error) {
	return CommandResult{}, errHandleUnavailable
}
func (noopHandleServer) PutFile(context.Context, string, []byte) error {
	return errHandleUnavailable
}
func (noopHandleServer) GetFile(context.Context, string) ([]byte, error) {
	return nil, errHandleUnavailable
}

// NoopHandleServer returns a HandleServer whose methods report that no target
// is bound. It is intended for plugin inspection (plugin list/info/staging)
// where there is no target to operate against.
func NoopHandleServer() HandleServer { return noopHandleServer{} }
