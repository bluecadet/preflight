package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
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

	name    string
	version string

	outMu sync.RWMutex
	out   OutputFunc
}

// NewClientStream connects a Client to a JSON-RPC plugin peer over the given
// reader/writer pair, performs the initialize handshake (sending protocol_version
// and the enriched TargetInfo, and requiring the plugin to echo protocol_version
// back), and binds the plugin's handle-op requests to ops. If closeFn is
// non-nil it is invoked exactly once from Close (and on initialize failure).
//
// ops carries the target effects the plugin exercises (RunCommand/PutFile/GetFile).
// For inspection paths with no target, pass NoopHandleServer(). info is the
// TargetInfo delivered to the plugin at initialize.
func NewClientStream(r io.Reader, w io.Writer, closeFn func() error, info TargetInfo, ops HandleServer) (*Client, error) {
	c := &Client{ops: ops}
	c.codec = newCodec(r, w, c.handleRequest, c.handleNotification, closeFn)
	c.codec.start()

	if err := c.initialize(info); err != nil {
		_ = c.codec.Close()
		return nil, err
	}
	return c, nil
}

// initialize sends the initialize request with protocol_version and TargetInfo,
// and requires the plugin's response to carry a matching protocol_version. A
// pre-v1 plugin (no protocol_version or a different one) is rejected with a
// ProtocolError so callers can surface the distinct plugin_protocol class.
func (c *Client) initialize(info TargetInfo) error {
	params := InitializeParams{
		ProtocolVersion: ProtocolVersion,
		Target:          info,
	}
	var res InitializeResult
	if err := c.codec.call(context.Background(), "initialize", params, &res); err != nil {
		return fmt.Errorf("plugin initialize: %w", err)
	}
	if res.ProtocolVersion != ProtocolVersion {
		return &ProtocolError{Got: res.ProtocolVersion, Want: ProtocolVersion}
	}
	c.name = res.Name
	c.version = res.Version
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
		return map[string]any{"data": encodeBase64(data)}, nil
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
	c.outMu.RLock()
	out := c.out
	c.outMu.RUnlock()
	if out != nil {
		out(p.Line)
	}
}

// Name returns the plugin's self-reported name.
func (c *Client) Name() string { return c.name }

// Version returns the plugin's self-reported version.
func (c *Client) Version() string { return c.version }

// Check calls the plugin's check method, forwarding output to out.
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

// Apply calls the plugin's apply method, forwarding output to out.
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
	c.outMu.Lock()
	c.out = out
	c.outMu.Unlock()
}

// Close terminates the plugin peer (hard kill on the transport, as before).
// Subsequent calls are no-ops.
func (c *Client) Close() error {
	return c.codec.Close()
}

// NewClient starts the plugin at executablePath for inspection (plugin
// list/info/staging): no target is bound, so a noop HandleServer is used and
// TargetInfo is empty. Use NewClientContext for runtime use against a target.
func NewClient(executablePath string) (*Client, error) {
	return NewClientForInspection(context.Background(), executablePath)
}

// NewClientForInspection starts a plugin with no target bound, for plugin
// list/info/staging paths that only read name/version.
func NewClientForInspection(ctx context.Context, executablePath string) (*Client, error) {
	return startClient(ctx, executablePath, TargetInfo{}, NoopHandleServer())
}

// NewClientContext starts the plugin, performs the v1 initialize handshake
// (protocol_version + enriched TargetInfo), and binds the plugin's handle ops
// to ops. Used by the runtime adapter against a real target.
func NewClientContext(ctx context.Context, executablePath string, info TargetInfo, ops HandleServer) (*Client, error) {
	return startClient(ctx, executablePath, info, ops)
}

func startClient(ctx context.Context, executablePath string, info TargetInfo, ops HandleServer) (*Client, error) {
	cmd := exec.CommandContext(ctx, executablePath)
	return NewClientFromCmd(cmd, info, ops)
}

// NewClientFromCmd starts the given command and connects a Client to its
// stdin/stdout for JSON-RPC communication. The command must not have been
// started yet. info is delivered at initialize; ops answers handle-op requests.
func NewClientFromCmd(cmd *exec.Cmd, info TargetInfo, ops HandleServer) (*Client, error) {
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
	return NewClientStream(stdout, stdin, func() error {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return cmd.Wait()
	}, info, ops)
}

// ProtocolError reports a plugin that failed the protocol-version handshake.
// Pre-v1 plugins (no protocol_version) and protocol mismatches both surface as
// this typed error so the host can raise the distinct plugin_protocol class.
type ProtocolError struct {
	Got  string
	Want string
}

func (e *ProtocolError) Error() string {
	if e.Got == "" {
		return fmt.Sprintf("plugin protocol: plugin did not report protocol_version (want %q); pre-v1 plugins are not supported", e.Want)
	}
	return fmt.Sprintf("plugin protocol: plugin reported protocol_version %q, want %q", e.Got, e.Want)
}

// IsProtocolError reports whether err is a *ProtocolError.
func IsProtocolError(err error) bool {
	var pe *ProtocolError
	return errors.As(err, &pe)
}
