package sdk

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// rpcResponse is a test helper for decoding JSON-RPC responses.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

// runServe pipes a single JSON request through serveIO and returns the decoded response.
func runServe(t *testing.T, m Module, reqJSON string) rpcResponse {
	t.Helper()

	pr, pw := io.Pipe()
	var outBuf strings.Builder

	done := make(chan struct{})
	go func() {
		defer close(done)
		serveIO(m, pr, &outBuf)
	}()

	if _, err := io.WriteString(pw, reqJSON+"\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	_ = pw.Close()
	<-done

	var resp rpcResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(outBuf.String())), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", outBuf.String(), err)
	}
	return resp
}

func TestServe_Check(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":"1","method":"check","params":{"args":{}}}`
	resp := runServe(t, mockModule{}, req)

	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %v", resp.Error)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var result CheckResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal CheckResult: %v", err)
	}
	if result.NeedsChange {
		t.Errorf("expected NeedsChange=false, got true")
	}
	if result.State["status"] != "ok" {
		t.Errorf("expected state.status=ok, got %v", result.State["status"])
	}
}

func TestServe_Apply(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":"2","method":"apply","params":{"args":{}}}`
	resp := runServe(t, mockModule{}, req)

	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %v", resp.Error)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var result ApplyResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal ApplyResult: %v", err)
	}
	if result.State["status"] != "applied" {
		t.Errorf("expected state.status=applied, got %v", result.State["status"])
	}
}

func TestServe_UnknownMethod(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":"3","method":"bogus","params":{}}`
	resp := runServe(t, mockModule{}, req)
	if resp.Error == nil {
		t.Fatal("expected rpc error for unknown method, got nil")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected error code -32601, got %d", resp.Error.Code)
	}
}

func TestServe_Initialize(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":"0","method":"initialize","params":{"protocol_version":"1","target":{"family":"linux"}}}`
	resp := runServe(t, mockModule{}, req)
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %v", resp.Error)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var init InitializeResult
	if err := json.Unmarshal(raw, &init); err != nil {
		t.Fatalf("unmarshal InitializeResult: %v", err)
	}
	if init.Name != "mock" {
		t.Errorf("expected name=mock, got %q", init.Name)
	}
	if init.ProtocolVersion != ProtocolVersion {
		t.Errorf("expected protocol_version=%q, got %q", ProtocolVersion, init.ProtocolVersion)
	}
}

// streamingModule emits output via the Handle during Check/Apply.
type streamingModule struct{}

func (streamingModule) Name() string    { return "streaming-mock" }
func (streamingModule) Version() string { return "1.0.0" }

func (streamingModule) Check(_ map[string]any, h Handle) (CheckResult, error) {
	h.Output("check line 1")
	h.Output("check line 2")
	return CheckResult{NeedsChange: true, Message: "needs update"}, nil
}

func (streamingModule) Apply(_ map[string]any, h Handle) (ApplyResult, error) {
	h.Output("apply line 1")
	h.Output("apply line 2")
	h.Output("apply line 3")
	return ApplyResult{Message: "applied"}, nil
}

func runServeMulti(t *testing.T, m Module, reqs []string) []string {
	t.Helper()
	pr, pw := io.Pipe()
	var outBuf strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveIO(m, pr, &outBuf)
	}()
	for _, req := range reqs {
		if _, err := io.WriteString(pw, req+"\n"); err != nil {
			t.Fatalf("write request: %v", err)
		}
	}
	_ = pw.Close()
	<-done
	return strings.Split(strings.TrimSpace(outBuf.String()), "\n")
}

func TestServe_StreamingCheck(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":"10","method":"check","params":{"args":{}}}`
	lines := runServeMulti(t, streamingModule{}, []string{req})
	// 2 output notifications + 1 response
	if len(lines) != 3 {
		t.Fatalf("expected 3 output lines, got %d: %v", len(lines), lines)
	}
	var notif1 struct {
		Method string          `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &notif1); err != nil {
		t.Fatalf("unmarshal notif1: %v", err)
	}
	if notif1.Method != "output" {
		t.Errorf("expected method=output, got %q", notif1.Method)
	}
	if notif1.Params["line"] != "check line 1" {
		t.Errorf("expected line 'check line 1', got %v", notif1.Params["line"])
	}
}

func TestServe_StreamingApply(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":"11","method":"apply","params":{"args":{}}}`
	lines := runServeMulti(t, streamingModule{}, []string{req})
	if len(lines) != 4 {
		t.Fatalf("expected 4 output lines, got %d: %v", len(lines), lines)
	}
}
