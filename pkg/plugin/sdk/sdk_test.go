package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
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
	return runServeWithOptions(t, m, reqJSON, ServeOptions{})
}

// runServeWithOptions is runServe with control over the plugin server's
// advertised ServeOptions, so tests can exercise capability negotiation and
// plugin-side handshake rejection.
func runServeWithOptions(t *testing.T, m Module, reqJSON string, opts ServeOptions) rpcResponse {
	t.Helper()

	pr, pw := io.Pipe()
	var outBuf strings.Builder

	done := make(chan struct{})
	go func() {
		defer close(done)
		serveIO(m, pr, &outBuf, opts)
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

// initInitializeReq is a synthetic, always-successful initialize request
// used by test helpers that need a plugin server past the
// initialize-completion gate before exercising check/apply/unknown-method
// behavior. Its id (999000) is chosen well clear of the small hand-picked
// ids (1-11) the tests in this file use for their own request under test.
const initInitializeReq = `{"jsonrpc":"2.0","id":999000,"method":"initialize","params":{"protocol_version":2,"min_protocol_version":2,"target":{}}}`

// initializedRunServeMulti is initializedRunServeMultiWithOptions with the
// zero ServeOptions.
func initializedRunServeMulti(t *testing.T, m Module, reqs []string) []string {
	t.Helper()
	return initializedRunServeMultiWithOptions(t, m, reqs, ServeOptions{})
}

// initializedRunServeMultiWithOptions completes a real initialize round trip
// first — writing initInitializeReq and blocking for its response before
// sending anything else — then writes reqs and returns every remaining
// response/notification line. Completing initialize as a genuine round trip
// (not pipelined) is what makes the subsequent behavior deterministic: once
// this function starts writing reqs, s.ready is already closed, so nothing
// here exercises (or refights) the benign race
// TestServe_PipelinedCheckNeverRacesInitialize covers on purpose.
//
// A background goroutine drains the output pipe into a buffered channel for
// the whole exchange, because io.Pipe writes block until read: the plugin
// server would deadlock writing its initialize response if nothing were
// reading yet. That goroutine, and the output pipe, are torn down once the
// server itself has finished.
func initializedRunServeMultiWithOptions(t *testing.T, m Module, reqs []string, opts ServeOptions) []string {
	t.Helper()

	pr, pw := io.Pipe()
	outR, outW := io.Pipe()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		serveIO(m, pr, outW, opts)
	}()

	lineCh := make(chan string, 64)
	go func() {
		scanner := bufio.NewScanner(outR)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		close(lineCh)
	}()

	readLine := func() string {
		select {
		case line, ok := <-lineCh:
			if !ok {
				t.Fatalf("output closed before expected response")
			}
			return line
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for a response line")
			return ""
		}
	}

	if _, err := io.WriteString(pw, initInitializeReq+"\n"); err != nil {
		t.Fatalf("write init request: %v", err)
	}
	var initResp rpcResponse
	if err := json.Unmarshal([]byte(readLine()), &initResp); err != nil {
		t.Fatalf("unmarshal init response: %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("initialize failed: %v", initResp.Error)
	}

	for _, req := range reqs {
		if _, err := io.WriteString(pw, req+"\n"); err != nil {
			t.Fatalf("write request: %v", err)
		}
	}
	_ = pw.Close()
	<-serverDone

	// The server has fully returned, so every write it made has already been
	// matched by a read into lineCh (io.Pipe writes block until read).
	// Closing outW/outR unblocks the scanning goroutine so it can exit and
	// close lineCh, which the drain below waits for.
	_ = outW.Close()
	_ = outR.Close()

	var lines []string
	for line := range lineCh {
		lines = append(lines, line)
	}
	return lines
}

// initializedRunServe is initializedRunServeMulti for a single request,
// returning its decoded response.
func initializedRunServe(t *testing.T, m Module, reqJSON string) rpcResponse {
	t.Helper()
	lines := initializedRunServeMulti(t, m, []string{reqJSON})
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d: %v", len(lines), lines)
	}
	var resp rpcResponse
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", lines[0], err)
	}
	return resp
}

func TestServe_Check(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":1,"method":"check","params":{"args":{}}}`
	resp := initializedRunServe(t, mockModule{}, req)

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
}

func TestServe_Apply(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":2,"method":"apply","params":{"args":{}}}`
	resp := initializedRunServe(t, mockModule{}, req)

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
}

func TestServe_UnknownMethod(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":3,"method":"bogus","params":{}}`
	resp := initializedRunServe(t, mockModule{}, req)
	if resp.Error == nil {
		t.Fatal("expected rpc error for unknown method, got nil")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected error code -32601, got %d", resp.Error.Code)
	}
}

// TestServe_MalformedCheckParams asserts that params whose "args" key is the
// wrong JSON type surface as a -32602 invalid-params error rather than
// silently running the plugin with empty args (regression for the
// argsFromParams silent fallback).
func TestServe_MalformedCheckParams(t *testing.T) {
	// args is a JSON array, but the handler expects an object → unmarshal error.
	req := `{"jsonrpc":"2.0","id":4,"method":"check","params":{"args":[1,2,3]}}`
	resp := initializedRunServe(t, mockModule{}, req)
	if resp.Error == nil {
		t.Fatal("expected rpc error for malformed params, got nil")
	}
	if resp.Error.Code != -32602 {
		t.Fatalf("expected error code -32602, got %d (%q)", resp.Error.Code, resp.Error.Message)
	}
	if resp.Result != nil {
		t.Errorf("expected no result, got %s", resp.Result)
	}
}

func TestServe_MalformedApplyParams(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":6,"method":"apply","params":{"args":5}}`
	resp := initializedRunServe(t, mockModule{}, req)
	if resp.Error == nil {
		t.Fatal("expected rpc error for malformed apply params, got nil")
	}
	if resp.Error.Code != -32602 {
		t.Fatalf("expected error code -32602, got %d (%q)", resp.Error.Code, resp.Error.Message)
	}
}

func TestServe_Initialize(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":5,"method":"initialize","params":{"protocol_version":2,"min_protocol_version":2,"target":{"family":"linux"}}}`
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
	if init.ProtocolVersion != ProtocolVersion {
		t.Errorf("expected protocol_version=%d, got %d", ProtocolVersion, init.ProtocolVersion)
	}
}

// TestServe_InitializeTolerantOfUnknownFields asserts that an initialize
// request carrying fields this SDK build does not recognize is accepted
// rather than rejected — the mechanism additive protocol changes rely on.
func TestServe_InitializeTolerantOfUnknownFields(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":7,"method":"initialize","params":{"protocol_version":2,"min_protocol_version":2,"target":{"family":"linux"},"future_field":{"nested":true}}}`
	resp := runServe(t, mockModule{}, req)
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error for unknown field: %v", resp.Error)
	}
}

// TestServe_InitializeAdvertisesCapabilities asserts that a plugin server
// configured with ServeOptions.Capabilities actually puts them on the wire.
// Before this, the plugin side always advertised an empty capability set
// regardless of ServeOptions, so a plugin author had no way to participate
// in capability negotiation.
func TestServe_InitializeAdvertisesCapabilities(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":8,"method":"initialize","params":{"protocol_version":2,"min_protocol_version":2,"target":{}}}`
	resp := runServeWithOptions(t, mockModule{}, req, ServeOptions{Capabilities: []string{"foo", "bar"}})
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
	got := append([]string{}, init.Capabilities...)
	sort.Strings(got)
	if want := []string{"bar", "foo"}; !slices.Equal(got, want) {
		t.Errorf("advertised capabilities = %v, want %v", got, want)
	}
}

// TestServe_InitializeRejectsNonOverlappingRange asserts that the plugin
// side, not just the host, refuses a handshake whose ranges do not overlap.
// A prior version silently echoed its own range and relied entirely on the
// host to notice; here the plugin computes the same negotiateProtocol result
// and fails the initialize call itself with a clear message naming both
// sides' ranges.
func TestServe_InitializeRejectsNonOverlappingRange(t *testing.T) {
	// A future host whose entire range (v9-v9) sits above this SDK's (v2-v2).
	req := `{"jsonrpc":"2.0","id":9,"method":"initialize","params":{"protocol_version":9,"min_protocol_version":9,"target":{}}}`
	resp := runServe(t, mockModule{}, req)
	if resp.Error == nil {
		t.Fatal("expected rpc error for non-overlapping range, got nil")
	}
	if !strings.Contains(resp.Error.Message, "v2-v2") || !strings.Contains(resp.Error.Message, "v9-v9") {
		t.Errorf("error message %q does not name both sides' ranges", resp.Error.Message)
	}
}

// TestServe_RejectsMethodBeforeInitialize sends "check" as the very first
// request, with no "initialize" ever sent. s.ready is never closed, so the
// gate deterministically takes its default branch every run: this is the
// out-of-order invariant in its simplest, always-reproducible form.
func TestServe_RejectsMethodBeforeInitialize(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":1,"method":"check","params":{"args":{}}}`
	resp := runServe(t, mockModule{}, req)
	if resp.Error == nil {
		t.Fatal("expected rpc error for check before initialize, got nil")
	}
	if resp.Error.Code != errCodeNotInitialized {
		t.Errorf("error code = %d, want %d", resp.Error.Code, errCodeNotInitialized)
	}
}

// TestServe_PipelinedCheckNeverRacesInitialize pipelines a "check" request
// immediately after "initialize", without waiting for the initialize
// response — an ordering no real Client produces (it blocks for the
// initialize response before issuing anything else) but nothing in the wire
// format forbids, and exactly the shape of request a misbehaving or
// adversarial peer could send.
//
// Before the initialize-completion gate, this raced on
// server.info/server.negotiated under go test -race, deterministically,
// every run: reproduced against the pre-fix stdio.go, this test failed with
// "WARNING: DATA RACE" 100% of the time.
//
// After the fix, which of the two legitimate outcomes occurs is a genuine,
// harmless race on wall-clock scheduling, not a memory race: the check
// handler may observe the gate as not yet ready (get errCodeNotInitialized)
// or, if the initialize handler happened to finish first, be dispatched
// normally. The channel close/receive makes either outcome memory-safe, so
// this test does not assert which one happens — only that whichever one does
// is well-formed, and that go test -race stays clean. Do not weaken this to
// "always errors": that would reintroduce flakiness for a property (which
// side wins a benign scheduling race) the fix was never meant to pin down.
func TestServe_PipelinedCheckNeverRacesInitialize(t *testing.T) {
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocol_version":2,"min_protocol_version":2,"target":{}}}`
	checkReq := `{"jsonrpc":"2.0","id":2,"method":"check","params":{"args":{}}}`

	lines := runServeMultiWithOptions(t, mockModule{}, []string{initReq, checkReq}, ServeOptions{})
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d: %v", len(lines), lines)
	}

	// Dispatch is concurrent once both requests are decoded, so response
	// order on the wire need not match request order; correlate by id.
	var checkResp *rpcResponse
	for _, line := range lines {
		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("unmarshal response %q: %v", line, err)
		}
		var id int
		if err := json.Unmarshal(resp.ID, &id); err != nil {
			t.Fatalf("unmarshal response id %q: %v", resp.ID, err)
		}
		if id == 2 {
			checkResp = &resp
		}
	}
	if checkResp == nil {
		t.Fatalf("no response for check request (id 2) among: %v", lines)
	}

	switch {
	case checkResp.Error != nil:
		// Lost the benign race: gate was not yet open. The only acceptable
		// error is the out-of-order one — anything else means a different
		// failure mode crept in.
		if checkResp.Error.Code != errCodeNotInitialized {
			t.Errorf("error code = %d, want %d (%q)", checkResp.Error.Code, errCodeNotInitialized, checkResp.Error.Message)
		}
	default:
		// Won the benign race: gate was already open, check ran normally.
		var result CheckResult
		if err := json.Unmarshal(checkResp.Result, &result); err != nil {
			t.Fatalf("unmarshal CheckResult: %v", err)
		}
	}
}

// streamingModule emits output via the Handle during Check/Apply.
type streamingModule struct{}

func (streamingModule) Name() string    { return "streaming-mock" }
func (streamingModule) Version() string { return "1.0.0" }

func (streamingModule) Check(_ context.Context, _ map[string]any, h Handle) (CheckResult, error) {
	h.Output("check line 1")
	h.Output("check line 2")
	return CheckResult{NeedsChange: true, Message: "needs update"}, nil
}

func (streamingModule) Apply(_ context.Context, _ map[string]any, h Handle) (ApplyResult, error) {
	h.Output("apply line 1")
	h.Output("apply line 2")
	h.Output("apply line 3")
	return ApplyResult{Message: "applied"}, nil
}

// runServeMultiWithOptions pipelines reqs through serveIO back-to-back, with
// no synchronization between them, and returns every response/notification
// line. Used directly (not through an initialize-first wrapper) by
// TestServe_PipelinedCheckNeverRacesInitialize, which depends on exactly
// this lack of synchronization to exercise the benign post-fix race between
// an initialize response and an immediately-following request. Tests that
// want a plugin server already past the initialize gate should use
// initializedRunServeMultiWithOptions instead.
func runServeMultiWithOptions(t *testing.T, m Module, reqs []string, opts ServeOptions) []string {
	t.Helper()
	pr, pw := io.Pipe()
	var outBuf strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveIO(m, pr, &outBuf, opts)
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
	req := `{"jsonrpc":"2.0","id":10,"method":"check","params":{"args":{}}}`
	lines := initializedRunServeMulti(t, streamingModule{}, []string{req})
	// 2 output notifications + 1 response
	if len(lines) != 3 {
		t.Fatalf("expected 3 output lines, got %d: %v", len(lines), lines)
	}
	var notif1 struct {
		Method string         `json:"method"`
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
	req := `{"jsonrpc":"2.0","id":11,"method":"apply","params":{"args":{}}}`
	lines := initializedRunServeMulti(t, streamingModule{}, []string{req})
	if len(lines) != 4 {
		t.Fatalf("expected 4 output lines, got %d: %v", len(lines), lines)
	}
}
