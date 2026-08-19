package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// mockModule is a simple in-process module used across tests.
type mockModule struct{}

func (mockModule) Name() string    { return "mock" }
func (mockModule) Version() string { return "2.3.4" }

func (mockModule) Check(_ context.Context, _ map[string]any, _ Handle) (CheckResult, error) {
	return CheckResult{
		NeedsChange: false,
	}, nil
}

func (mockModule) Apply(_ context.Context, _ map[string]any, _ Handle) (ApplyResult, error) {
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

func (m *handleModule) Check(ctx context.Context, args map[string]any, h Handle) (CheckResult, error) {
	m.checkCalls++
	m.lastInfo = h.Info()
	res, err := h.RunCommand(ctx, "echo hello")
	if err != nil {
		return CheckResult{}, err
	}
	m.lastCmd = res
	data := []byte("plugin-put-content")
	if err := h.PutFile(ctx, "/tmp/pf-put", data); err != nil {
		return CheckResult{}, err
	}
	m.putData = data
	got, err := h.GetFile(ctx, "/tmp/pf-get")
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

func (m *handleModule) Apply(_ context.Context, _ map[string]any, h Handle) (ApplyResult, error) {
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
	return pipeTransportWithOptions(t, m, ServeOptions{})
}

// pipeTransportWithOptions is pipeTransport with control over the plugin
// server's advertised ServeOptions (its capabilities), so tests can exercise
// capability negotiation from both sides.
func pipeTransportWithOptions(t *testing.T, m Module, opts ServeOptions) (r io.Reader, w io.Writer, closeFn func() error) {
	t.Helper()
	srvRead, clientWrite := io.Pipe()
	clientRead, srvWrite := io.Pipe()
	go serveIO(m, srvRead, srvWrite, opts)
	return clientRead, clientWrite, func() error {
		_ = clientWrite.Close()
		_ = clientRead.Close()
		return nil
	}
}

func newClient(t *testing.T, m Module, info TargetInfo, ops HandleServer) *Client {
	t.Helper()
	r, w, closeFn := pipeTransport(t, m)
	c, err := NewClient(context.Background(), r, w, closeFn, ClientOptions{Info: info, Ops: ops})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
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
	// A pre-v1 initialize response: name/version but no protocol_version or
	// min_protocol_version at all (both decode to their zero value), so its
	// range never overlaps this SDK's.
	preV1 := `{"name":"old","version":"0.1.0"}`
	r, w, closeFn := rawFrameServer(t, preV1)
	_, err := NewClient(context.Background(), r, w, closeFn, ClientOptions{})
	if err == nil {
		t.Fatal("expected initialize failure for pre-v1 plugin, got nil")
	}
	if !IsProtocolError(err) {
		t.Fatalf("expected *ProtocolError, got %T: %v", err, err)
	}
}

func TestClientStream_RejectsProtocolMismatch(t *testing.T) {
	// A future plugin whose entire supported range (v9-v9) sits above this
	// SDK's (v2-v2): the ranges do not overlap.
	mismatch := `{"name":"future","version":"9.0.0","protocol_version":9,"min_protocol_version":9}`
	r, w, closeFn := rawFrameServer(t, mismatch)
	_, err := NewClient(context.Background(), r, w, closeFn, ClientOptions{})
	if err == nil {
		t.Fatal("expected initialize failure for protocol mismatch, got nil")
	}
	if !IsProtocolError(err) {
		t.Fatalf("expected *ProtocolError, got %T: %v", err, err)
	}
	var pe *ProtocolError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ProtocolError, got %T", err)
	}
	if pe.LocalMin != MinProtocolVersion || pe.LocalMax != ProtocolVersion {
		t.Errorf("LocalMin/LocalMax = %d/%d, want %d/%d", pe.LocalMin, pe.LocalMax, MinProtocolVersion, ProtocolVersion)
	}
	if pe.PeerMin != 9 || pe.PeerMax != 9 {
		t.Errorf("PeerMin/PeerMax = %d/%d, want 9/9", pe.PeerMin, pe.PeerMax)
	}
	if !strings.Contains(err.Error(), "v2-v2") || !strings.Contains(err.Error(), "v9-v9") {
		t.Errorf("error %q does not name both sides' ranges", err.Error())
	}
}

// TestClientStream_NegotiatesOverlappingRange asserts that a peer advertising
// a wider range than this SDK's still succeeds, negotiating down to the
// highest version both sides support — the whole point of a range instead of
// an exact-match version string.
func TestClientStream_NegotiatesOverlappingRange(t *testing.T) {
	// Peer supports v1 through v5; this SDK supports v2-v2. Overlap is v2.
	wide := `{"name":"wide","version":"1.0.0","protocol_version":5,"min_protocol_version":1}`
	r, w, closeFn := rawFrameServer(t, wide)
	c, err := NewClient(context.Background(), r, w, closeFn, ClientOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()
	if got := c.ProtocolVersion(); got != ProtocolVersion {
		t.Errorf("negotiated version = %d, want %d", got, ProtocolVersion)
	}
}

// TestClientStream_CapabilityIntersection asserts that Client.Capabilities
// reflects only the capability names both sides advertised, and that
// HasCapability answers accordingly — the mechanism that lets a feature be
// detected at runtime instead of gated on a version bump.
func TestClientStream_CapabilityIntersection(t *testing.T) {
	resp := `{"name":"caps","version":"1.0.0","protocol_version":2,"min_protocol_version":2,"capabilities":["foo","bar"]}`
	r, w, closeFn := rawFrameServer(t, resp)
	c, err := NewClient(context.Background(), r, w, closeFn, ClientOptions{Capabilities: []string{"bar", "baz"}})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	if !c.HasCapability("bar") {
		t.Error("expected HasCapability(bar) = true (advertised by both sides)")
	}
	if c.HasCapability("foo") {
		t.Error("expected HasCapability(foo) = false (peer-only)")
	}
	if c.HasCapability("baz") {
		t.Error("expected HasCapability(baz) = false (local-only)")
	}
	caps := c.Capabilities()
	if len(caps) != 1 || caps[0] != "bar" {
		t.Errorf("Capabilities() = %v, want [bar]", caps)
	}
}

// negotiationCapturingModule records the Negotiated result its Check
// observed via NegotiatedFromContext, so a test can assert on what the
// plugin side of the handshake saw.
type negotiationCapturingModule struct {
	checkNegotiated Negotiated
	checkOK         bool
}

func (negotiationCapturingModule) Name() string    { return "negotiation-mock" }
func (negotiationCapturingModule) Version() string { return "1.0.0" }

func (m *negotiationCapturingModule) Check(ctx context.Context, _ map[string]any, _ Handle) (CheckResult, error) {
	m.checkNegotiated, m.checkOK = NegotiatedFromContext(ctx)
	return CheckResult{}, nil
}

func (negotiationCapturingModule) Apply(context.Context, map[string]any, Handle) (ApplyResult, error) {
	return ApplyResult{}, nil
}

// TestClientStream_PluginSeesNegotiatedCapabilities is the end-to-end proof
// that capability negotiation is symmetric. The plugin advertises its own
// capabilities via ServeOptions and computes the same Negotiated result the
// host does; a module's Check reads it back via NegotiatedFromContext. This
// is the gap a prior review found: capabilities existed only on the host's
// Client, so a plugin author had no way to advertise or observe them.
func TestClientStream_PluginSeesNegotiatedCapabilities(t *testing.T) {
	mod := &negotiationCapturingModule{}
	r, w, closeFn := pipeTransportWithOptions(t, mod, ServeOptions{Capabilities: []string{"bar", "baz"}})
	c, err := NewClient(context.Background(), r, w, closeFn, ClientOptions{Capabilities: []string{"foo", "bar"}})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	// Host side sees the intersection: only "bar" was advertised by both.
	if !c.HasCapability("bar") || c.HasCapability("foo") || c.HasCapability("baz") {
		t.Fatalf("host Capabilities() = %v, want exactly [bar]", c.Capabilities())
	}

	if _, err := c.Check(context.Background(), map[string]any{}, nil); err != nil {
		t.Fatalf("Check: %v", err)
	}

	if !mod.checkOK {
		t.Fatal("plugin Check did not observe a Negotiated result via NegotiatedFromContext")
	}
	if mod.checkNegotiated.Version != ProtocolVersion {
		t.Errorf("plugin-observed negotiated version = %d, want %d", mod.checkNegotiated.Version, ProtocolVersion)
	}
	if !mod.checkNegotiated.HasCapability("bar") {
		t.Error("plugin-observed negotiated capabilities missing bar")
	}
	if mod.checkNegotiated.HasCapability("foo") || mod.checkNegotiated.HasCapability("baz") {
		t.Errorf("plugin-observed negotiated capabilities = %v, want exactly [bar]", mod.checkNegotiated.Capabilities())
	}
}

func TestClientStream_CloseCallsCloseFn(t *testing.T) {
	r, w, closeFn := pipeTransport(t, mockModule{})
	c, err := NewClient(context.Background(), r, w, closeFn, ClientOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
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
	_, err := NewClient(context.Background(), clientRead, clientWrite, closeFn, ClientOptions{})
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
