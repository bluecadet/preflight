package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// Client is the runner-side handle for a JSON-RPC plugin peer. It is
// transport-agnostic: it speaks the wire protocol over the reader/writer
// pair supplied at construction and delegates cleanup to an optional
// close function.
type Client struct {
	name    string
	version string
	enc     *json.Encoder
	dec     *json.Decoder
	close   func() error
	mu      sync.Mutex
	seq     atomic.Int64
}

// NewClientStream connects a Client to a JSON-RPC plugin peer over the given
// reader/writer pair and performs the "initialize" handshake. The reader is
// the peer's response stream; the writer is the peer's request stream. The
// Client does not own the streams: if closeFn is non-nil it is invoked exactly
// once from Close (and on initialize failure) to release any underlying
// transport resources; if nil, Close is a no-op.
func NewClientStream(r io.Reader, w io.Writer, closeFn func() error) (*Client, error) {
	c := &Client{
		enc:   json.NewEncoder(w),
		dec:   json.NewDecoder(r),
		close: closeFn,
	}

	var initResult initializeResult
	if err := c.call("initialize", nil, &initResult); err != nil {
		if closeFn != nil {
			_ = closeFn()
		}
		return nil, fmt.Errorf("plugin initialize: %w", err)
	}
	c.name = initResult.Name
	c.version = initResult.Version
	return c, nil
}

// NewClient starts the plugin at executablePath, sends an "initialize" request,
// and returns a ready-to-use Client.
func NewClient(executablePath string) (*Client, error) {
	return NewClientContext(context.Background(), executablePath)
}

// NewClientContext starts the plugin with a context, sends an "initialize"
// request, and returns a ready-to-use Client.
func NewClientContext(ctx context.Context, executablePath string) (*Client, error) {
	cmd := exec.CommandContext(ctx, executablePath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin %s: %w", executablePath, err)
	}

	return NewClientStream(stdout, stdin, func() error {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return cmd.Wait()
	})
}

// NewClientFromCmd starts the given command and connects a Client to its
// stdin/stdout for JSON-RPC communication. The command must not have been
// started yet. The caller must call Close() when done.
func NewClientFromCmd(cmd *exec.Cmd) (*Client, error) {
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
	})
}

// Name returns the plugin's self-reported name.
func (c *Client) Name() string { return c.name }

// Version returns the plugin's self-reported version.
func (c *Client) Version() string { return c.version }

// Check calls the plugin's check method.
func (c *Client) Check(args map[string]any) (CheckResult, error) {
	var result CheckResult
	params := map[string]any{"args": args}
	if err := c.call("check", params, &result); err != nil {
		return CheckResult{}, err
	}
	if result.Error != "" {
		return result, fmt.Errorf("plugin check: %s", result.Error)
	}
	return result, nil
}

// Apply calls the plugin's apply method.
func (c *Client) Apply(args map[string]any) (ApplyResult, error) {
	var result ApplyResult
	params := map[string]any{"args": args}
	if err := c.call("apply", params, &result); err != nil {
		return ApplyResult{}, err
	}
	if result.Error != "" {
		return result, fmt.Errorf("plugin apply: %s", result.Error)
	}
	return result, nil
}

// Close terminates the plugin peer. It invokes the close function supplied
// at construction (if any); subsequent calls are no-ops only in the sense that
// the underlying transport defines their semantics.
func (c *Client) Close() error {
	if c.close != nil {
		return c.close()
	}
	return nil
}

// callStreaming sends a single JSON-RPC request, dispatching any output
// notification frames to out, and decodes the final response into result.
func (c *Client) callStreaming(method string, params any, out OutputFunc, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.seq.Add(1)

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}
	if params != nil {
		if p, ok := params.(map[string]any); ok {
			req.Params = p
		}
	}

	if err := c.enc.Encode(req); err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	for {
		var raw struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      any             `json:"id"`
			Method  string          `json:"method"`
			Params  map[string]any  `json:"params"`
			Result  json.RawMessage `json:"result"`
			Error   *rpcError       `json:"error"`
		}
		if err := c.dec.Decode(&raw); err != nil {
			if err == io.EOF {
				return fmt.Errorf("plugin closed connection unexpectedly")
			}
			return fmt.Errorf("decode response: %w", err)
		}

		// Notification frame: no id, has method == "output"
		if raw.ID == nil && raw.Method == "output" {
			if out != nil {
				if line, ok := raw.Params["line"].(string); ok {
					out(line)
				}
			}
			continue
		}

		// Response frame
		if raw.Error != nil {
			return fmt.Errorf("rpc error %d: %s", raw.Error.Code, raw.Error.Message)
		}
		if result != nil && raw.Result != nil {
			if err := json.Unmarshal(raw.Result, result); err != nil {
				return fmt.Errorf("unmarshal result: %w", err)
			}
		}
		return nil
	}
}

// call sends a single JSON-RPC request and decodes the result into out.
func (c *Client) call(method string, params any, out any) error {
	return c.callStreaming(method, params, nil, out)
}

// CheckStreaming calls the plugin's check method and dispatches output lines to out.
func (c *Client) CheckStreaming(args map[string]any, out OutputFunc) (CheckResult, error) {
	var result CheckResult
	params := map[string]any{"args": args}
	if err := c.callStreaming("check", params, out, &result); err != nil {
		return CheckResult{}, err
	}
	if result.Error != "" {
		return result, fmt.Errorf("plugin check: %s", result.Error)
	}
	return result, nil
}

// ApplyStreaming calls the plugin's apply method and dispatches output lines to out.
func (c *Client) ApplyStreaming(args map[string]any, out OutputFunc) (ApplyResult, error) {
	var result ApplyResult
	params := map[string]any{"args": args}
	if err := c.callStreaming("apply", params, out, &result); err != nil {
		return ApplyResult{}, err
	}
	if result.Error != "" {
		return result, fmt.Errorf("plugin apply: %s", result.Error)
	}
	return result, nil
}
