package sdk

import (
	"context"
	"fmt"
	"time"
)

// ProtocolVersion is the wire-protocol version this SDK build speaks. Host
// and plugin must report exactly this version at initialize; any other value
// is rejected with a *ProtocolError.
//
// Version history:
//   - v1 — initial handle-op protocol (run_command/put_file/get_file, output
//     notifications).
//   - v2 — adds the "cancel" notification and per-request context.Context
//     cancellation.
//   - v3 — drops name, package_manager, and init from TargetInfo and
//     requires an exact version match.
const ProtocolVersion = 3

// CancelGrace is the default window a peer waits, after sending a cancel
// notification for an in-flight request, for that request to unwind and
// answer before it gives up and returns the context error. It is the window
// in which a plugin's cancelled Check/Apply can release locks, delete
// half-written files, and return. A plugin that does not return within the
// window is torn down with the session (a stated limitation).
//
// ClientOptions.CancelGrace overrides this per client; the zero value there
// falls back to CancelGrace.
const CancelGrace = 2 * time.Second

// cancelParams is the payload of the bidirectional "cancel" notification:
// the ID of the request the sender is abandoning. The receiving side cancels
// the context.Context it handed that request's handler.
type cancelParams struct {
	ID int64 `json:"id"`
}

// protocolHello is one side's advertisement during the initialize handshake:
// its wire-protocol version plus the capability names it recognizes. Both
// initializeParams and initializeResult embed it so host and plugin run
// identical checks. Unrecognized peer fields are tolerated by ordinary
// json.Unmarshal semantics.
type protocolHello struct {
	ProtocolVersion int      `json:"protocol_version"`
	Capabilities    []string `json:"capabilities,omitempty"`
}

// localHello returns this SDK build's advertisement for a peer at
// initialize. extra is the capability set advertised alongside the version.
func localHello(extra []string) protocolHello {
	return protocolHello{ProtocolVersion: ProtocolVersion, Capabilities: extra}
}

// Negotiated is the outcome of the initialize handshake: the wire-protocol
// version and capability set both sides agreed on. On the host side it is
// readable from a Client after a successful handshake. On the plugin side it
// is attached to the context.Context passed to Check/Apply; retrieve it with
// NegotiatedFromContext. Either way, callers branch on features at runtime
// instead of gating them on a version bump.
type Negotiated struct {
	// Version is the protocol version both peers speak.
	Version int
	// capabilities is the set of capability names both sides advertised.
	capabilities map[string]struct{}
}

// negotiatedContextKey is the context.Context key Negotiated is stored under
// for a plugin's Check/Apply. Unexported so only this package can set it;
// NegotiatedFromContext is the only way to read it back.
type negotiatedContextKey struct{}

// contextWithNegotiated returns a copy of ctx carrying n, retrievable by a
// plugin's Check/Apply via NegotiatedFromContext.
func contextWithNegotiated(ctx context.Context, n Negotiated) context.Context {
	return context.WithValue(ctx, negotiatedContextKey{}, n)
}

// NegotiatedFromContext returns the Negotiated handshake result carried by
// ctx, and whether one was present. Plugin authors call this from Check/Apply
// to read the protocol version and capability set negotiated with the host,
// so optional behavior can be gated on a capability rather than a protocol
// version bump. ok is false for a context that did not come from a Check/Apply
// call (for example, one built directly in a unit test).
func NegotiatedFromContext(ctx context.Context) (n Negotiated, ok bool) {
	n, ok = ctx.Value(negotiatedContextKey{}).(Negotiated)
	return n, ok
}

// HasCapability reports whether name was advertised by both peers during the
// handshake.
func (n Negotiated) HasCapability(name string) bool {
	_, ok := n.capabilities[name]
	return ok
}

// Capabilities returns the negotiated capability names in no particular
// order.
func (n Negotiated) Capabilities() []string {
	out := make([]string, 0, len(n.capabilities))
	for c := range n.capabilities {
		out = append(out, c)
	}
	return out
}

// negotiateProtocol checks that peer speaks exactly local's protocol version
// and resolves the capability set both sides advertised. A version mismatch
// returns a *ProtocolError naming both versions.
func negotiateProtocol(local, peer protocolHello) (Negotiated, error) {
	if local.ProtocolVersion != peer.ProtocolVersion {
		return Negotiated{}, &ProtocolError{Local: local.ProtocolVersion, Peer: peer.ProtocolVersion}
	}
	return Negotiated{Version: local.ProtocolVersion, capabilities: intersectCapabilities(local.Capabilities, peer.Capabilities)}, nil
}

// intersectCapabilities returns the capability names present in both a and b.
func intersectCapabilities(a, b []string) map[string]struct{} {
	bSet := make(map[string]struct{}, len(b))
	for _, c := range b {
		bSet[c] = struct{}{}
	}
	out := make(map[string]struct{})
	for _, c := range a {
		if _, ok := bSet[c]; ok {
			out[c] = struct{}{}
		}
	}
	return out
}

// TargetInfo is the target context delivered to a plugin at initialize.
// Absent signals are empty strings, never missing keys, so plugin code can
// branch on them with simple equality. RuntimeKind tells the plugin which
// shell RunCommand speaks (always windows-powershell).
type TargetInfo struct {
	Family      string `json:"family"`
	Version     string `json:"version"`
	Arch        string `json:"arch"`
	Hostname    string `json:"hostname"`
	RuntimeKind string `json:"runtime_kind"`
}

// CommandResult is the outcome of a RunCommand handle op: the script runs in
// the target's native shell and returns separated stdout/stderr and the exit
// code.
type CommandResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// getFileResult is the wire shape of a get_file response: the file bytes
// base64-encoded. Defined once so host and plugin sides agree on the field.
type getFileResult struct {
	Data string `json:"data"`
}

// initializeParams is sent by the host in the initialize request: its
// protocol version and capabilities, plus the target it is running
// against.
type initializeParams struct {
	protocolHello
	Target TargetInfo `json:"target"`
}

// initializeResult is the plugin's initialize response: its supported
// protocol version and capabilities, plus its self-reported name and version.
type initializeResult struct {
	protocolHello
	Name    string `json:"name"`
	Version string `json:"version"`
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
// (Local, SSH, WinRM) implements it; the Client dispatches
// plugin handle-op requests to it. Handle embeds this so the subset
// relationship between handle ops and the full plugin Handle stays explicit.
type HandleServer interface {
	// RunCommand executes script in the target's native shell (PowerShell) and returns stdout, stderr, and
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

// ProtocolError reports a peer whose wire-protocol version differs from this
// side's, so the handshake cannot proceed.
type ProtocolError struct {
	Local, Peer int
}

// Error implements error.
func (e *ProtocolError) Error() string {
	if e.Peer == 0 {
		return fmt.Sprintf("plugin protocol: peer reported no protocol_version (pre-negotiation plugin); this side requires v%d", e.Local)
	}
	return fmt.Sprintf("plugin protocol: version mismatch; this side requires v%d, peer reports v%d", e.Local, e.Peer)
}
