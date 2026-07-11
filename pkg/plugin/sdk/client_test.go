package sdk

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// mockModule is a simple in-process module used across tests.
type mockModule struct{}

func (mockModule) Name() string    { return "mock" }
func (mockModule) Version() string { return "2.3.4" }

func (mockModule) Check(_ map[string]any, _ Handle) (CheckResult, error) {
	return CheckResult{
		NeedsChange: false,
	}, nil
}

func (mockModule) Apply(_ map[string]any, _ Handle) (ApplyResult, error) {
	return ApplyResult{}, nil
}

// handleModule exercises the handle API: it records what it was called with so
// tests can assert the round trip. Used by the bidirectional-protocol tests.
type handleModule struct {
	checkCalls  int
	applyCalls  int
	lastInfo    TargetInfo
	lastCmd     CommandResult
	putData     []byte
	getData     []byte
	outputLines []string
}

func (m *handleModule) Name() string    { return "handle-mock" }
func (m *handleModule) Version() string { return "1.0.0" }

func (m *handleModule) Check(args map[string]any, h Handle) (CheckResult, error) {
	m.checkCalls++
	m.lastInfo = h.Info()
	res, err := h.RunCommand(context.Background(), "echo hello")
	if err != nil {
		return CheckResult{}, err
	}
	m.lastCmd = res
	data := []byte("plugin-put-content")
	if err := h.PutFile(context.Background(), "/tmp/pf-put", data); err != nil {
		return CheckResult{}, err
	}
	m.putData = data
	got, err := h.GetFile(context.Background(), "/tmp/pf-get")
	if err != nil {
		return CheckResult{}, err
	}
	m.getData = got
	for _, line := range m.outputLines {
		h.Output(line)
	}
	needsChange := false
	if v, ok := args["needs_change"].(bool); ok {
		needsChange = v
	}
	return CheckResult{NeedsChange: needsChange, Message: "checked"}, nil
}

func (m *handleModule) Apply(_ map[string]any, h Handle) (ApplyResult, error) {
	m.applyCalls++
	h.Output("apply line")
	return ApplyResult{Message: "applied"}, nil
}

// fakeHandleServer is an in-process HandleServer for protocol tests.
type fakeHandleServer struct {
	cmdStdout string
	cmdStderr string
	cmdExit   int
	cmdScript string
	putPath   string
	putData   []byte
	getPath   string
	getData   []byte
	getErr    error
}

func (s *fakeHandleServer) RunCommand(_ context.Context, script string) (CommandResult, error) {
	s.cmdScript = script
	return CommandResult{Stdout: s.cmdStdout, Stderr: s.cmdStderr, ExitCode: s.cmdExit}, nil
}
func (s *fakeHandleServer) PutFile(_ context.Context, path string, data []byte) error {
	s.putPath = path
	s.putData = data
	return nil
}
func (s *fakeHandleServer) GetFile(_ context.Context, path string) ([]byte, error) {
	s.getPath = path
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.getData, nil
}

// pipeTransport wires an in-process serveIO server to a Client via two
// io.Pipes. The returned closeFn closes the client-owned pipe ends so serveIO
// sees EOF and exits.
func pipeTransport(t *testing.T, m Module) (r io.Reader, w io.Writer, closeFn func() error) {
	t.Helper()
	srvRead, clientWrite := io.Pipe()
	clientRead, srvWrite := io.Pipe()
	go serveIO(m, srvRead, srvWrite)
	return clientRead, clientWrite, func() error {
		_ = clientWrite.Close()
		_ = clientRead.Close()
		return nil
	}
}

func newClient(t *testing.T, m Module, info TargetInfo, ops HandleServer) *Client {
	t.Helper()
	r, w, closeFn := pipeTransport(t, m)
	c, err := NewClientStream(r, w, closeFn, info, ops)
	if err != nil {
		t.Fatalf("NewClientStream: %v", err)
	}
	return c
}

func TestClientStream_Initialize(t *testing.T) {
	c := newClient(t, mockModule{}, TargetInfo{Family: "linux"}, NoopHandleServer())
	defer func() { _ = c.Close() }()

	if c.Name() != "mock" {
		t.Errorf("expected name=mock, got %q", c.Name())
	}
	if c.Version() != "2.3.4" {
		t.Errorf("expected version=2.3.4, got %q", c.Version())
	}
}

func TestClientStream_Check(t *testing.T) {
	c := newClient(t, mockModule{}, TargetInfo{}, NoopHandleServer())
	defer func() { _ = c.Close() }()

	result, err := c.Check(context.Background(), map[string]any{}, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.NeedsChange {
		t.Errorf("expected NeedsChange=false, got true")
	}
}

func TestClientStream_Apply(t *testing.T) {
	c := newClient(t, mockModule{}, TargetInfo{}, NoopHandleServer())
	defer func() { _ = c.Close() }()

	_, err := c.Apply(context.Background(), map[string]any{}, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestClientStream_HandleOpsRoundTrip(t *testing.T) {
	info := TargetInfo{Family: "linux", Name: "ubuntu", RuntimeKind: "posix-sh"}
	ops := &fakeHandleServer{
		cmdStdout: "hello\n",
		getData:   []byte("file-contents"),
	}
	mod := &handleModule{outputLines: []string{"out-1", "out-2"}}
	c := newClient(t, mod, info, ops)
	defer func() { _ = c.Close() }()

	var lines []string
	_, err := c.Check(context.Background(), map[string]any{}, func(line string) { lines = append(lines, line) })
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if mod.lastInfo != info {
		t.Errorf("plugin received info %+v, want %+v", mod.lastInfo, info)
	}
	if ops.cmdScript != "echo hello" {
		t.Errorf("RunCommand script = %q, want %q", ops.cmdScript, "echo hello")
	}
	if mod.lastCmd.Stdout != "hello\n" {
		t.Errorf("RunCommand stdout = %q, want %q", mod.lastCmd.Stdout, "hello\n")
	}
	if string(ops.putData) != "plugin-put-content" {
		t.Errorf("PutFile data = %q", string(ops.putData))
	}
	if ops.putPath != "/tmp/pf-put" {
		t.Errorf("PutFile path = %q", ops.putPath)
	}
	if ops.getPath != "/tmp/pf-get" {
		t.Errorf("GetFile path = %q", ops.getPath)
	}
	if string(mod.getData) != "file-contents" {
		t.Errorf("GetFile data = %q", string(mod.getData))
	}
	want := []string{"out-1", "out-2"}
	if !equalStrings(lines, want) {
		t.Errorf("output lines = %v, want %v", lines, want)
	}
}

func TestClientStream_StreamingApply(t *testing.T) {
	mod := &handleModule{}
	c := newClient(t, mod, TargetInfo{}, &fakeHandleServer{})
	defer func() { _ = c.Close() }()

	var lines []string
	_, err := c.Apply(context.Background(), map[string]any{}, func(line string) { lines = append(lines, line) })
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []string{"apply line"}
	if !equalStrings(lines, want) {
		t.Errorf("output lines = %v, want %v", lines, want)
	}
}

// rawFrameServer is a minimal hand-rolled server used to test protocol-version
// rejection: it can emit an initialize response that omits protocol_version,
// simulating a pre-v1 plugin that the real v1 SDK could never produce.
func rawFrameServer(t *testing.T, initResp string) (r io.Reader, w io.Writer, closeFn func() error) {
	t.Helper()
	srvRead, clientWrite := io.Pipe()
	clientRead, srvWrite := io.Pipe()
	go func() {
		// Read and discard the initialize request, then send the canned response.
		go func() { _, _ = io.Copy(io.Discard, srvRead) }()
		_ = json.NewEncoder(srvWrite).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      json.Number("1"),
			"result":  json.RawMessage(initResp),
		})
	}()
	return clientRead, clientWrite, func() error {
		_ = clientWrite.Close()
		_ = clientRead.Close()
		return nil
	}
}

func TestClientStream_RejectsPreV1Plugin(t *testing.T) {
	// A pre-v1 initialize response: name/version but no protocol_version.
	preV1 := `{"name":"old","version":"0.1.0"}`
	r, w, closeFn := rawFrameServer(t, preV1)
	_, err := NewClientStream(r, w, closeFn, TargetInfo{}, NoopHandleServer())
	if err == nil {
		t.Fatal("expected initialize failure for pre-v1 plugin, got nil")
	}
	if !IsProtocolError(err) {
		t.Fatalf("expected *ProtocolError, got %T: %v", err, err)
	}
}

func TestClientStream_RejectsProtocolMismatch(t *testing.T) {
	mismatch := `{"name":"future","version":"9.0.0","protocol_version":"9"}`
	r, w, closeFn := rawFrameServer(t, mismatch)
	_, err := NewClientStream(r, w, closeFn, TargetInfo{}, NoopHandleServer())
	if err == nil {
		t.Fatal("expected initialize failure for protocol mismatch, got nil")
	}
	if !IsProtocolError(err) {
		t.Fatalf("expected *ProtocolError, got %T: %v", err, err)
	}
}

func TestClientStream_CloseCallsCloseFn(t *testing.T) {
	r, w, closeFn := pipeTransport(t, mockModule{})
	c, err := NewClientStream(r, w, closeFn, TargetInfo{}, NoopHandleServer())
	if err != nil {
		t.Fatalf("NewClientStream: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestClientStream_InitFailureCallsCloseFn(t *testing.T) {
	clientRead, srvWrite := io.Pipe()
	_ = srvWrite.Close()
	_, clientWrite := io.Pipe()
	_ = clientWrite.Close()
	closeCalls := 0
	closeFn := func() error { closeCalls++; return nil }
	_, err := NewClientStream(clientRead, clientWrite, closeFn, TargetInfo{}, NoopHandleServer())
	if err == nil {
		t.Fatal("expected initialize failure, got nil error")
	}
	if !strings.Contains(err.Error(), "plugin initialize") {
		t.Errorf("expected error to mention 'plugin initialize', got %q", err.Error())
	}
	if closeCalls != 1 {
		t.Errorf("expected closeFn called once on init failure, got %d", closeCalls)
	}
}

// equalStrings is a tiny helper to avoid pulling in reflect.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
