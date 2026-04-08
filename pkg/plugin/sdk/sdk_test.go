package sdk

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// mockModule is a simple in-process module used across tests.
type mockModule struct{}

func (mockModule) Name() string    { return "mock" }
func (mockModule) Version() string { return "2.3.4" }

func (mockModule) Check(_ map[string]any) (CheckResult, error) {
	return CheckResult{
		NeedsChange: false,
		State:       map[string]any{"status": "ok"},
	}, nil
}

func (mockModule) Apply(_ map[string]any) (ApplyResult, error) {
	return ApplyResult{
		State: map[string]any{"status": "applied"},
	}, nil
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

	// Write the request and close the writer so serveIO sees EOF.
	_, err := io.WriteString(pw, reqJSON+"\n")
	if err != nil {
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
	req := `{"jsonrpc":"2.0","id":"1","method":"check","params":{"module":"mock","args":{}}}`
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
	req := `{"jsonrpc":"2.0","id":"2","method":"apply","params":{"module":"mock","args":{}}}`
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
	req := `{"jsonrpc":"2.0","id":"0","method":"initialize","params":{}}`
	resp := runServe(t, mockModule{}, req)

	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %v", resp.Error)
	}

	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var init initializeResult
	if err := json.Unmarshal(raw, &init); err != nil {
		t.Fatalf("unmarshal initializeResult: %v", err)
	}

	if init.Name != "mock" {
		t.Errorf("expected name=mock, got %q", init.Name)
	}
	if init.Version != "2.3.4" {
		t.Errorf("expected version=2.3.4, got %q", init.Version)
	}
}
