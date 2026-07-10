package sdk

import (
	"io"
	"strings"
	"testing"
)

// pipeTransport wires an in-process serveIO server to a Client via two
// io.Pipes, mirroring how a real subprocess's stdin/stdout would be connected.
// The returned closeFn closes the client-owned pipe ends so serveIO sees EOF
// and exits.
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

func TestClientStream_Initialize(t *testing.T) {
	r, w, closeFn := pipeTransport(t, mockModule{})
	defer func() { _ = closeFn() }()

	c, err := NewClientStream(r, w, nil)
	if err != nil {
		t.Fatalf("NewClientStream: %v", err)
	}

	if c.Name() != "mock" {
		t.Errorf("expected name=mock, got %q", c.Name())
	}
	if c.Version() != "2.3.4" {
		t.Errorf("expected version=2.3.4, got %q", c.Version())
	}
}

func TestClientStream_Check(t *testing.T) {
	r, w, closeFn := pipeTransport(t, mockModule{})
	defer func() { _ = closeFn() }()

	c, err := NewClientStream(r, w, nil)
	if err != nil {
		t.Fatalf("NewClientStream: %v", err)
	}

	result, err := c.Check(map[string]any{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.NeedsChange {
		t.Errorf("expected NeedsChange=false, got true")
	}
	if result.State["status"] != "ok" {
		t.Errorf("expected state.status=ok, got %v", result.State["status"])
	}
}

func TestClientStream_Apply(t *testing.T) {
	r, w, closeFn := pipeTransport(t, mockModule{})
	defer func() { _ = closeFn() }()

	c, err := NewClientStream(r, w, nil)
	if err != nil {
		t.Fatalf("NewClientStream: %v", err)
	}

	result, err := c.Apply(map[string]any{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.State["status"] != "applied" {
		t.Errorf("expected state.status=applied, got %v", result.State["status"])
	}
}

func TestClientStream_StreamingCheck(t *testing.T) {
	r, w, closeFn := pipeTransport(t, streamingMockModule{})
	defer func() { _ = closeFn() }()

	c, err := NewClientStream(r, w, nil)
	if err != nil {
		t.Fatalf("NewClientStream: %v", err)
	}

	var lines []string
	result, err := c.CheckStreaming(map[string]any{}, func(line string) { lines = append(lines, line) })
	if err != nil {
		t.Fatalf("CheckStreaming: %v", err)
	}
	if !result.NeedsChange {
		t.Errorf("expected NeedsChange=true")
	}
	if result.Message != "needs update" {
		t.Errorf("expected message 'needs update', got %q", result.Message)
	}
	want := []string{"check line 1", "check line 2"}
	if !equalStrings(lines, want) {
		t.Errorf("expected output lines %v, got %v", want, lines)
	}
}

func TestClientStream_CloseCallsCloseFn(t *testing.T) {
	r, w, closeFn := pipeTransport(t, mockModule{})

	c, err := NewClientStream(r, w, closeFn)
	if err != nil {
		t.Fatalf("NewClientStream: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestClientStream_InitFailureCallsCloseFn(t *testing.T) {
	// Peer that closes its write end immediately, so the client sees EOF
	// during the initialize handshake.
	clientRead, srvWrite := io.Pipe()
	_ = srvWrite.Close()
	_, clientWrite := io.Pipe()
	_ = clientWrite.Close()

	closeCalls := 0
	closeFn := func() error { closeCalls++; return nil }

	_, err := NewClientStream(clientRead, clientWrite, closeFn)
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
