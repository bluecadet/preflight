package sdk

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
)

// rpcRequest is the JSON-RPC 2.0 request envelope received from the runner.
type rpcRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

// rpcResponse is the JSON-RPC 2.0 response envelope sent back to the runner.
type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

// rpcError represents a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// initializeResult is the response payload for the "initialize" method.
// Version is the plugin's self-reported version.
type initializeResult struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// serve reads newline-delimited JSON-RPC requests from r and writes responses to w.
// It is the internal implementation used by Serve and tests.
func serveIO(m Module, r io.Reader, w io.Writer) {
	enc := json.NewEncoder(w)
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error: " + err.Error()},
			})
			continue
		}

		resp := dispatch(m, req)
		_ = enc.Encode(resp)
	}
}

// serve is the production entry point: uses os.Stdin / os.Stdout.
func serve(m Module) {
	serveIO(m, os.Stdin, os.Stdout)
}

// dispatch routes a single request to the appropriate module method.
func dispatch(m Module, req rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  initializeResult{Name: m.Name(), Version: m.Version()},
		}

	case "check":
		args := argsFromParams(req.Params)
		result, err := m.Check(args)
		if err != nil {
			result.Error = err.Error()
		}
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}

	case "apply":
		args := argsFromParams(req.Params)
		result, err := m.Apply(args)
		if err != nil {
			result.Error = err.Error()
		}
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}

	default:
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: "method not found: " + req.Method},
		}
	}
}

// argsFromParams extracts the "args" key from JSON-RPC params, returning an
// empty map when absent.
func argsFromParams(params map[string]any) map[string]any {
	if params == nil {
		return map[string]any{}
	}
	if raw, ok := params["args"]; ok {
		if m, ok := raw.(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}
