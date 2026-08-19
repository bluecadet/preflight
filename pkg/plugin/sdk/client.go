package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync/atomic"
	"time"
)

// Client is the runner-side handle for a JSON-RPC plugin peer. It is
// transport-agnostic: it speaks the wire protocol over the reader/writer pair
// supplied at construction and delegates cleanup to an optional close function.
//
// Both sides act as client and server: the Client sends initialize/check/apply
// requests and forwards the plugin's output notifications to an OutputFunc,
// while simultaneously answering the plugin's handle-op requests
// (run_command/put_file/get_file) through the bound HandleServer. One outgoing
// call is in flight at a time (the codec serializes them); incoming handle-op
// requests are handled concurrently so a plugin's Check can issue a RunCommand
// while the host's check call is still outstanding.
type Client struct {
	codec *codec
	ops   HandleServer

	name       string
	version    string
	negotiated Negotiated

	// out holds the current call's OutputFunc. It is swapped per Check/Apply
	// under callMu (one outgoing call in flight at a time), so a single
	// writer races only with the notification handler's read. An atomic
	// pointer makes that read lock-free; a nil pointer means no output.
	out atomic.Pointer[OutputFunc]
}

// ClientOptions configures a Client's connection to a plugin peer. The zero
// value is valid: no target is delivered at initialize, handle-op requests
// are answered with "no target is bound", and the default CancelGrace and
// capability set apply. Inspection paths (plugin list/info/staging) use the
// zero value.
type ClientOptions struct {
	// Info is the TargetInfo delivered to the plugin at initialize.
	Info TargetInfo
	// Ops answers the plugin's handle-op requests (RunCommand/PutFile/GetFile).
	// A nil Ops behaves like NoopHandleServer(): every handle op fails with a
	// "no target is bound" error.
	Ops HandleServer
	// Capabilities are additional capability names this host advertises
	// during the initialize handshake, beyond whatever the SDK itself
	// requires. Plugin authors and host integrators use this to detect
	// optional features at runtime instead of gating them on a protocol
	// version bump.
	Capabilities []string
	// CancelGrace overrides how long this client waits for a peer to unwind
	// after a cancel notification. Zero uses the package default, CancelGrace.
	// A zero grace period cannot be requested explicitly — this is a stated
	// limitation, not an oversight; a client that genuinely needs to skip
	// the grace window should close the connection directly instead.
	CancelGrace time.Duration
}

// NewClient connects a Client to a JSON-RPC plugin peer over the given
// reader/writer pair and performs the initialize handshake: it sends this
// side's protocol version range, capabilities, and TargetInfo, and requires
// the peer's advertised range to overlap. If closeFn is non-nil it is invoked
// exactly once from Close (and on initialize failure).
//
// ctx bounds the handshake only. It deliberately does not govern the peer's
// lifetime: a peer whose process died the instant an operation context was
// cancelled could never honour the cancel notification, which is the whole
// point of protocol v2. Peer teardown is Close's job.
func NewClient(ctx context.Context, r io.Reader, w io.Writer, closeFn func() error, opts ClientOptions) (*Client, error) {
	c := &Client{ops: opts.Ops}
	c.codec = newCodec(r, w, c.handleRequest, c.handleNotification, closeFn, opts.CancelGrace)
	c.codec.start()

	if err := c.initialize(ctx, opts); err != nil {
		_ = c.codec.Close()
		return nil, err
	}
	return c, nil
}

// initialize sends the initialize request with this side's protocol range
// and TargetInfo, and negotiates a version and capability set against the
// peer's response. A peer whose range does not overlap is rejected with a
// *ProtocolError naming both sides' supported ranges.
func (c *Client) initialize(ctx context.Context, opts ClientOptions) error {
	params := initializeParams{
		protocolRange: localProtocolRange(opts.Capabilities),
		Target:        opts.Info,
	}
	var res initializeResult
	if err := c.codec.call(ctx, "initialize", params, &res); err != nil {
		return fmt.Errorf("plugin initialize: %w", err)
	}
	negotiated, err := negotiateProtocol(params.protocolRange, res.protocolRange)
	if err != nil {
		return err
	}
	c.name = res.Name
	c.version = res.Version
	c.negotiated = negotiated
	return nil
}

// handleRequest dispatches an incoming plugin→host request (a handle op).
func (c *Client) handleRequest(ctx context.Context, method string, params json.RawMessage) (any, *rpcError) {
	ops := c.ops
	if ops == nil {
		return nil, &rpcError{Code: -32001, Message: "plugin handle: no target backend bound"}
	}
	switch method {
	case "run_command":
		var p struct {
			Script string `json:"script"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: "run_command params: " + err.Error()}
		}
		res, err := ops.RunCommand(ctx, p.Script)
		if err != nil {
			return nil, &rpcError{Code: -32000, Message: err.Error()}
		}
		return res, nil
	case "put_file":
		var p struct {
			Path string `json:"path"`
			Data string `json:"data"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: "put_file params: " + err.Error()}
		}
		data, err := decodeBase64(p.Data)
		if err != nil {
			return nil, &rpcError{Code: -32602, Message: "put_file data: " + err.Error()}
		}
		if err := ops.PutFile(ctx, p.Path, data); err != nil {
			return nil, &rpcError{Code: -32000, Message: err.Error()}
		}
		return struct{}{}, nil
	case "get_file":
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: "get_file params: " + err.Error()}
		}
		data, err := ops.GetFile(ctx, p.Path)
		if err != nil {
			return nil, &rpcError{Code: -32000, Message: err.Error()}
		}
		return getFileResult{Data: encodeBase64(data)}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}

// handleNotification forwards an output notification to the current call's
// OutputFunc. The current OutputFunc is set per Check/Apply call under outMu.
func (c *Client) handleNotification(params json.RawMessage) {
	var p struct {
		Line string `json:"line"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	if outPtr := c.out.Load(); outPtr != nil {
		(*outPtr)(p.Line)
	}
}

// Name returns the plugin's self-reported name.
func (c *Client) Name() string { return c.name }

// Version returns the plugin's self-reported version.
func (c *Client) Version() string { return c.version }

// ProtocolVersion returns the wire-protocol version negotiated with the
// peer during initialize.
func (c *Client) ProtocolVersion() int { return c.negotiated.Version }

// HasCapability reports whether capability was advertised by both this
// client and the peer during initialize.
func (c *Client) HasCapability(capability string) bool { return c.negotiated.HasCapability(capability) }

// Capabilities returns the capability names negotiated with the peer during
// initialize, in no particular order.
func (c *Client) Capabilities() []string { return c.negotiated.Capabilities() }

// Check calls the plugin's check method, forwarding output to out.
//
// Cancelling ctx is not a local give-up: the codec sends a cancel notification
// naming the in-flight request, which cancels the context the plugin's own
// Check received, then waits up to CancelGrace for the plugin to unwind and
// answer before returning ctx.Err().
func (c *Client) Check(ctx context.Context, args map[string]any, out OutputFunc) (CheckResult, error) {
	c.setOut(out)
	defer c.setOut(nil)
	var result CheckResult
	if err := c.codec.call(ctx, "check", map[string]any{"args": args}, &result); err != nil {
		return CheckResult{}, err
	}
	if result.Error != "" {
		return result, fmt.Errorf("plugin check: %s", result.Error)
	}
	return result, nil
}

// Apply calls the plugin's apply method, forwarding output to out. Cancelling
// ctx behaves as described on Check: the plugin's context is cancelled over
// the wire and given CancelGrace to unwind.
func (c *Client) Apply(ctx context.Context, args map[string]any, out OutputFunc) (ApplyResult, error) {
	c.setOut(out)
	defer c.setOut(nil)
	var result ApplyResult
	if err := c.codec.call(ctx, "apply", map[string]any{"args": args}, &result); err != nil {
		return ApplyResult{}, err
	}
	if result.Error != "" {
		return result, fmt.Errorf("plugin apply: %s", result.Error)
	}
	return result, nil
}

func (c *Client) setOut(out OutputFunc) {
	if out == nil {
		c.out.Store(nil)
		return
	}
	c.out.Store(&out)
}

// Close terminates the plugin peer (hard kill on the transport, as before).
// Subsequent calls are no-ops.
func (c *Client) Close() error {
	return c.codec.Close()
}

// NewClientFromCmd starts the given command and connects a Client to its
// stdin/stdout for JSON-RPC communication. The command must not have been
// started yet. This is the standard entry point for spawning a plugin
// executable, whether for a bound target (opts.Info/opts.Ops set) or for
// inspection (the zero ClientOptions).
//
// Do not build cmd with exec.CommandContext on an operation context: that kills
// the peer the moment the operation is cancelled, racing the cancel
// notification to zero and making the plugin-side context.Context decorative.
// The process is killed by Close instead, after the peer has had CancelGrace
// to unwind.
func NewClientFromCmd(ctx context.Context, cmd *exec.Cmd, opts ClientOptions) (*Client, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin: %w", err)
	}
	return NewClient(ctx, stdout, stdin, func() error {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return cmd.Wait()
	}, opts)
}

// IsProtocolError reports whether err is a *ProtocolError.
func IsProtocolError(err error) bool {
	var pe *ProtocolError
	return errors.As(err, &pe)
}
