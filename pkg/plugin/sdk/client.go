package sdk

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// Client is the runner-side handle for a running plugin process.
type Client struct {
	name string
	cmd  *exec.Cmd
	enc  *json.Encoder
	dec  *json.Decoder
	mu   sync.Mutex
	seq  atomic.Int64
}

// NewClient starts the plugin at executablePath, sends an "initialize" request,
// and returns a ready-to-use Client.
func NewClient(executablePath string) (*Client, error) {
	cmd := exec.Command(executablePath)

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

	c := &Client{
		cmd: cmd,
		enc: json.NewEncoder(stdin),
		dec: json.NewDecoder(stdout),
	}

	// Send initialize and capture the plugin's declared name.
	var initResult initializeResult
	if err := c.call("initialize", nil, &initResult); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("plugin initialize: %w", err)
	}
	c.name = initResult.Name

	return c, nil
}

// Name returns the plugin's self-reported name.
func (c *Client) Name() string { return c.name }

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

// Close terminates the plugin process.
func (c *Client) Close() error {
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

// call sends a single JSON-RPC request and decodes the result into out.
func (c *Client) call(method string, params any, out any) error {
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

	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *rpcError       `json:"error"`
	}
	if err := c.dec.Decode(&resp); err != nil {
		if err == io.EOF {
			return fmt.Errorf("plugin closed connection unexpectedly")
		}
		return fmt.Errorf("decode response: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	if out != nil && resp.Result != nil {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("unmarshal result: %w", err)
		}
	}

	return nil
}
