// Command preflight-plugin-prev1 simulates a pre-v1 plugin: it responds to
// initialize with name/version only (no protocol_version), so the v1 host must
// reject it with a plugin_protocol error. It is a raw JSON-RPC server, not
// using the v1 SDK, so it can produce the pre-v1 shape the real SDK never
// would.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if req.Method == "initialize" {
			// Pre-v1: no protocol_version field.
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  map[string]any{"name": "prev1", "version": "0.0.1"},
			})
			// Exit after initialize; the host rejects before check/apply.
			return
		}
		_ = enc.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error":   map[string]any{"code": -32601, "message": fmt.Sprintf("method not found: %s", req.Method)},
		})
	}
}
