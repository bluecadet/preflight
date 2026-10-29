package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
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
	c := newClient(t, mockModule{}, TargetInfo{Family: "windows"}, nil)
	defer func() { _ = c.Close() }()

	if c.Name() != "mock" {
		t.Errorf("expected name=mock, got %q", c.Name())
	}
	if c.Version() != "2.3.4" {
		t.Errorf("expected version=2.3.4, got %q", c.Version())
	}
}

func TestClientStream_Check(t *testing.T) {
	c := newClient(t, mockModule{}, TargetInfo{}, nil)
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
	c := newClient(t, mockModule{}, TargetInfo{}, nil)
	defer func() { _ = c.Close() }()

	_, err := c.Apply(context.Background(), map[string]any{}, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestClientStream_HandleOpsRoundTrip(t *testing.T) {
	info := TargetInfo{Family: "windows", RuntimeKind: "windows-powershell"}
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

// rawFrameServer is a minimal hand-rolled server for capability negotiation
// and protocol rejection tests. Its raw initialize result may deliberately
// omit protocol_version, unlike responses produced by the real SDK.
func rawFrameServer(t *testing.T, initResp string) (r io.Reader, w io.Writer, closeFn func() error) {
	t.Helper()
	srvRead, clientWrite := io.Pipe()
	clientRead, srvWrite := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = srvRead.Close() }()
		defer func() { _ = srvWrite.Close() }()

		// Await a complete initialize before replying. An unsolicited response
		// can otherwise arrive before NewClient has registered its pending call
		// and be discarded. Correlate with the actual request, not an assumed ID.
		var request frame
		if err := json.NewDecoder(srvRead).Decode(&request); err != nil {
			return
		}
		if request.Method != "initialize" || request.ID <= 0 {
			return
		}
		if err := json.NewEncoder(srvWrite).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result":  json.RawMessage(initResp),
		}); err != nil {
			return
		}
		// Continue consuming later requests/notifications until client teardown.
		// Keep this in the tracked server goroutine so cleanup also joins it.
		_, _ = io.Copy(io.Discard, srvRead)
	}()

	// Bound the transport itself: a context alone cannot interrupt a blocked
	// io.Pipe write (including a cancellation notification after init times out).
	const timeout = 5 * time.Second
	timer := time.AfterFunc(timeout, func() {
		_ = clientWrite.CloseWithError(context.DeadlineExceeded)
		_ = clientRead.CloseWithError(context.DeadlineExceeded)
	})
	closeFn = func() error {
		timer.Stop()
		_ = clientWrite.Close()
		_ = clientRead.Close()
		select {
		case <-done:
			return nil
		case <-time.After(timeout):
			return errors.New("timed out stopping raw frame server")
		}
	}
	t.Cleanup(func() {
		if err := closeFn(); err != nil {
			t.Error(err)
		}
	})
	return clientRead, clientWrite, closeFn
}

func TestRawFrameServer_InitializeOrderingAndID(t *testing.T) {
	// Closing an absent or incomplete request makes the ordering assertion
	// deterministic: neither case may elicit a response, regardless of scheduling.
	// A non-default ID also catches helpers that hard-code the first client ID.
	for _, tt := range []struct {
		name    string
		request string
		wantID  int64
	}{
		{name: "no request"},
		{name: "incomplete request", request: `{"jsonrpc":"2.0","id":42,"method":"initialize","params":`},
		{name: "complete request", request: `{"jsonrpc":"2.0","id":42,"method":"initialize","params":{}}` + "\n", wantID: 42},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Deliberately retain a pre-v1 payload: the helper must not normalize
			// protocol fixtures while correlating the response envelope.
			const initResp = `{"name":"old","version":"0.1.0"}`
			r, w, _ := rawFrameServer(t, initResp)

			type exchangeResult struct {
				response frame
				err      error
			}
			done := make(chan exchangeResult, 1)
			go func() {
				if _, err := io.WriteString(w, tt.request); err != nil {
					done <- exchangeResult{err: err}
					return
				}
				if tt.wantID == 0 {
					_ = w.(io.Closer).Close()
				}
				var response frame
				err := json.NewDecoder(r).Decode(&response)
				if err == nil && tt.wantID != 0 {
					// The server must keep draining after its canned response, e.g.
					// so a later cancellation notification cannot block the client.
					_, err = io.WriteString(w, `{"jsonrpc":"2.0","method":"cancel","params":{"id":42}}`+"\n")
				}
				done <- exchangeResult{response: response, err: err}
			}()

			select {
			case got := <-done:
				if tt.wantID == 0 {
					if !errors.Is(got.err, io.EOF) {
						t.Fatalf("response before a complete initialize: frame=%+v, err=%v; want EOF", got.response, got.err)
					}
					return
				}
				if got.err != nil {
					t.Fatalf("initialize exchange: %v", got.err)
				}
				if got.response.JSONRPC != "2.0" || got.response.ID != tt.wantID {
					t.Errorf("response envelope = %+v, want JSON-RPC 2.0 with ID %d", got.response, tt.wantID)
				}
				if string(got.response.Result) != initResp {
					t.Errorf("result = %s, want unchanged fixture %s", got.response.Result, initResp)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for initialize exchange")
			}
		})
	}
}

func TestClientStream_RejectsPreV1Plugin(t *testing.T) {
	// A pre-v1 initialize response: name/version but no protocol_version
	// at all (decodes to zero), so it never matches this SDK's version.
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
	// A future plugin speaking v9 against this SDK's v3.
	mismatch := `{"name":"future","version":"9.0.0","protocol_version":9}`
	r, w, closeFn := rawFrameServer(t, mismatch)
	_, err := NewClient(context.Background(), r, w, closeFn, ClientOptions{})
	if err == nil {
		t.Fatal("expected initialize failure for protocol mismatch, got nil")
	}
	var pe *ProtocolError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ProtocolError, got %T: %v", err, err)
	}
	if pe.Local != ProtocolVersion || pe.Peer != 9 {
		t.Errorf("Local/Peer = %d/%d, want %d/9", pe.Local, pe.Peer, ProtocolVersion)
	}
	if !strings.Contains(err.Error(), "v3") || !strings.Contains(err.Error(), "v9") {
		t.Errorf("error %q does not name both versions", err.Error())
	}
}

// TestClientStream_CapabilityIntersection asserts that Client.Capabilities
// reflects only the capability names both sides advertised, and that
// HasCapability answers accordingly — the mechanism that lets a feature be
// detected at runtime instead of gated on a version bump.
func TestClientStream_CapabilityIntersection(t *testing.T) {
	resp := `{"name":"caps","version":"1.0.0","protocol_version":3,"capabilities":["foo","bar"]}`
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
