package sdk

import (
	"context"
	"fmt"
	"time"
)

// ProtocolVersion is the highest wire-protocol version this SDK build speaks.
// MinProtocolVersion is the lowest. Both host and plugin advertise this range
// (plus a capability set) at initialize; negotiation picks the highest
// version present in both ranges. A future additive change bumps
// ProtocolVersion while leaving MinProtocolVersion where it is, so existing
// binaries keep negotiating successfully instead of being hard-rejected.
//
// Version history:
//   - v1 — initial handle-op protocol (run_command/put_file/get_file, output
//     notifications).
//   - v2 — adds the "cancel" notification and per-request context.Context
//     cancellation.
const (
	ProtocolVersion    = 2
	MinProtocolVersion = 2
)

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

// protocolRange is one side's advertised wire-protocol support during the
// initialize handshake: the inclusive [MinProtocolVersion, ProtocolVersion]
// range of versions it can speak, plus the capability names it recognizes.
// Both initializeParams and initializeResult embed it so host and plugin run
// identical negotiation logic. Unrecognized peer fields are tolerated by
// ordinary json.Unmarshal semantics.
type protocolRange struct {
	ProtocolVersion    int      `json:"protocol_version"`
	MinProtocolVersion int      `json:"min_protocol_version"`
	Capabilities       []string `json:"capabilities,omitempty"`
}

// localProtocolRange returns this SDK build's supported version range,
// advertised to a peer at initialize. extra is appended to the capability
// set advertised alongside it.
func localProtocolRange(extra []string) protocolRange {
	return protocolRange{
		ProtocolVersion:    ProtocolVersion,
		MinProtocolVersion: MinProtocolVersion,
		Capabilities:       extra,
	}
}

// Negotiated is the outcome of the initialize handshake: the wire-protocol
// version and capability set both sides agreed on. On the host side it is
// readable from a Client after a successful handshake. On the plugin side it
// is attached to the context.Context passed to Check/Apply; retrieve it with
// NegotiatedFromContext. Either way, callers branch on features at runtime
// instead of gating them on a version bump.
type Negotiated struct {
	// Version is the highest protocol version present in both peers' ranges.
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

// negotiateProtocol resolves the wire-protocol version and capability set
// shared between a local and peer protocolRange. It succeeds when the two
// version ranges overlap, selecting the highest mutually supported version;
// otherwise it returns a *ProtocolError naming both sides' supported ranges.
func negotiateProtocol(local, peer protocolRange) (Negotiated, error) {
	lo := max(local.MinProtocolVersion, peer.MinProtocolVersion)
	hi := min(local.ProtocolVersion, peer.ProtocolVersion)
	if lo > hi {
		return Negotiated{}, &ProtocolError{
			LocalMin: local.MinProtocolVersion,
			LocalMax: local.ProtocolVersion,
			PeerMin:  peer.MinProtocolVersion,
			PeerMax:  peer.ProtocolVersion,
		}
	}
	return Negotiated{Version: hi, capabilities: intersectCapabilities(local.Capabilities, peer.Capabilities)}, nil
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

// getFileResult is the wire shape of a get_file response: the file bytes
// base64-encoded. Defined once so host and plugin sides agree on the field.
type getFileResult struct {
	Data string `json:"data"`
}

// initializeParams is sent by the host in the initialize request: its
// supported protocol range and capabilities, plus the target it is running
// against.
type initializeParams struct {
	protocolRange
	Target TargetInfo `json:"target"`
}

// initializeResult is the plugin's initialize response: its supported
// protocol range and capabilities, plus its self-reported name and version.
type initializeResult struct {
	protocolRange
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

// ProtocolError reports a peer whose supported wire-protocol version range
// does not overlap with this side's, so no version could be negotiated.
// LocalMin/LocalMax and PeerMin/PeerMax are each inclusive version bounds,
// letting callers report both sides' supported ranges.
type ProtocolError struct {
	LocalMin, LocalMax int
	PeerMin, PeerMax   int
}

// Error implements error.
func (e *ProtocolError) Error() string {
	peer := fmt.Sprintf("v%d-v%d", e.PeerMin, e.PeerMax)
	if e.PeerMin == 0 && e.PeerMax == 0 {
		peer = "no protocol_version reported (pre-negotiation plugin)"
	}
	return fmt.Sprintf(
		"plugin protocol: no overlapping version range; this side supports v%d-v%d, peer supports %s",
		e.LocalMin, e.LocalMax, peer,
	)
}
