package target

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// fakeKeepaliveConn is a stub sshKeepaliveConn used to drive sshKeepaliveLoop
// in isolation from a real *ssh.Client.
type fakeKeepaliveConn struct {
	mu       sync.Mutex
	requests int
	fail     bool
}

func (f *fakeKeepaliveConn) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if f.fail {
		return false, nil, fmt.Errorf("send request: connection reset")
	}
	return true, nil, nil
}

func (f *fakeKeepaliveConn) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func TestSSHKeepaliveLoop_SendsRequestsAtIntervalAndStopsOnClose(t *testing.T) {
	conn := &fakeKeepaliveConn{}
	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		sshKeepaliveLoop(conn, 5*time.Millisecond, stop, func() {
			t.Error("onRepeatedFailure should not be called when requests succeed")
		})
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for conn.count() < 3 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for keepalive requests, got %d", conn.count())
		case <-time.After(time.Millisecond):
		}
	}

	close(stop)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sshKeepaliveLoop to return after stop closed")
	}
}

func TestSSHKeepaliveLoop_ClosesClientAfterTwoConsecutiveFailures(t *testing.T) {
	conn := &fakeKeepaliveConn{fail: true}
	stop := make(chan struct{})
	done := make(chan struct{})

	var failureCalls atomic.Int64
	go func() {
		sshKeepaliveLoop(conn, 5*time.Millisecond, stop, func() {
			failureCalls.Add(1)
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sshKeepaliveLoop to return after repeated failure")
	}

	if got := failureCalls.Load(); got != 1 {
		t.Fatalf("expected onRepeatedFailure to be called exactly once, got %d", got)
	}
	if conn.count() < 2 {
		t.Fatalf("expected at least 2 keepalive attempts before giving up, got %d", conn.count())
	}
}

// fakeSSHConnCloser is a fake sshRunner that also implements sshCloser, used
// to test the reconnect path in SSHTarget.run.
type fakeSSHConnCloser struct {
	fakeSSHRunner
	closed atomic.Bool
}

func (f *fakeSSHConnCloser) Close() error {
	f.closed.Store(true)
	return nil
}

func TestSSHTarget_RunReconnectsOnConnectionError(t *testing.T) {
	first := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, io.EOF
		},
	}}
	second := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			if command != "echo hi" {
				t.Fatalf("unexpected command on reconnected runner: %q", command)
			}
			return "hi", "", 0, nil
		},
	}}

	var factoryCalls atomic.Int64
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = first
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		n := factoryCalls.Add(1)
		if n == 1 {
			return second, nil
		}
		t.Fatalf("unexpected extra runnerFactory call #%d", n)
		return nil, nil
	}

	stdout, _, code, err := tgt.run(context.Background(), "echo hi", nil)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if stdout != "hi" || code != 0 {
		t.Fatalf("unexpected result: stdout=%q code=%d", stdout, code)
	}
	if factoryCalls.Load() != 1 {
		t.Fatalf("expected runnerFactory to be called once for reconnect, got %d", factoryCalls.Load())
	}
	if !first.closed.Load() {
		t.Fatal("expected the dead runner to be closed on reconnect")
	}
	if tgt.runner != sshRunner(second) {
		t.Fatal("expected the cached runner to be the reconnected one")
	}
}

// TestSSHTarget_ReconnectAfterConcurrentClose covers the race where Close()
// nils the cached runner while a call is still in flight: reconnect must dial
// a fresh runner rather than returning the nil cached runner (which would
// panic in run's retry).
func TestSSHTarget_ReconnectAfterConcurrentClose(t *testing.T) {
	failed := &fakeSSHConnCloser{}
	fresh := &fakeSSHConnCloser{}

	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = nil // simulates Close() having run mid-call
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		return fresh, nil
	}

	runner, err := tgt.reconnect(failed)
	if err != nil {
		t.Fatalf("reconnect returned error: %v", err)
	}
	if runner == nil {
		t.Fatal("reconnect returned a nil runner")
	}
	if runner != sshRunner(fresh) {
		t.Fatal("expected reconnect to dial a fresh runner")
	}
}

func TestSSHTarget_RunDoesNotRetryOnNonConnectionError(t *testing.T) {
	var runCalls atomic.Int64
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			runCalls.Add(1)
			return "", "boom", 1, nil
		},
	}
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		t.Fatal("runnerFactory should not be called for a plain non-connection error")
		return nil, nil
	}

	_, stderr, code, err := tgt.run(context.Background(), "echo hi", nil)
	if err != nil {
		t.Fatalf("run returned unexpected error: %v", err)
	}
	if stderr != "boom" || code != 1 {
		t.Fatalf("unexpected result: stderr=%q code=%d", stderr, code)
	}
	if runCalls.Load() != 1 {
		t.Fatalf("expected exactly one Run call, got %d", runCalls.Load())
	}
}

func TestSSHTarget_RunDoesNotRetryOnContextCanceled(t *testing.T) {
	var factoryCalls atomic.Int64
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, context.Canceled
		},
	}
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		factoryCalls.Add(1)
		return nil, fmt.Errorf("should not be called")
	}

	_, _, _, err := tgt.run(context.Background(), "echo hi", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if factoryCalls.Load() != 0 {
		t.Fatalf("expected no reconnect attempt for a cancelled context, got %d factory calls", factoryCalls.Load())
	}
}

func TestSSHTarget_RunSurfacesErrorWhenReconnectAlsoFails(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, io.EOF
		},
	}
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		return nil, fmt.Errorf("connection refused")
	}

	_, _, _, err := tgt.run(context.Background(), "echo hi", nil)
	if err == nil {
		t.Fatal("expected an error when reconnect dialing fails")
	}
	if !strings.Contains(err.Error(), "reconnect") {
		t.Fatalf("expected error to mention reconnect, got: %v", err)
	}
}

func TestSSHTarget_RunSurfacesErrorWhenRetriedCallAlsoFails(t *testing.T) {
	first := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, io.EOF
		},
	}}
	second := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, io.EOF
		},
	}}

	var factoryCalls atomic.Int64
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = first
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		factoryCalls.Add(1)
		return second, nil
	}

	_, _, _, err := tgt.run(context.Background(), "echo hi", nil)
	if err == nil {
		t.Fatal("expected an error when the retried call also fails")
	}
	if factoryCalls.Load() != 1 {
		t.Fatalf("expected exactly one reconnect attempt (no retry loop), got %d", factoryCalls.Load())
	}
}

func TestSSHTarget_CloseClosesReconnectedRunner(t *testing.T) {
	first := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, io.EOF
		},
	}}
	second := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "ok", "", 0, nil
		},
	}}

	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = first
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		return second, nil
	}

	if _, _, _, err := tgt.run(context.Background(), "echo hi", nil); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if err := tgt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !second.closed.Load() {
		t.Fatal("expected Close to close the reconnected runner")
	}
}

// TestSSHTarget_ClosedTargetRejectsReconnect covers the resurrection bug
// where Close() nils t.runner but reconnect (invoked by run's one-shot
// retry when an in-flight call races Close and fails with a
// connection-level error) would otherwise see the nil runner and happily
// dial and cache a fresh one, resurrecting a connection on a target that
// nothing will ever close again. Once Close has run, both clientRunner and
// reconnect must refuse to dial and report the target as closed instead.
func TestSSHTarget_ClosedTargetRejectsReconnect(t *testing.T) {
	failed := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, io.EOF
		},
	}}

	var factoryCalls atomic.Int64
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		factoryCalls.Add(1)
		return &fakeSSHConnCloser{}, nil
	}

	if _, err := tgt.clientRunner(); err != nil {
		t.Fatalf("clientRunner returned error: %v", err)
	}
	if factoryCalls.Load() != 1 {
		t.Fatalf("expected exactly one dial before Close, got %d", factoryCalls.Load())
	}

	if err := tgt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	// Simulates a call that already held the (now-closed) runner racing
	// Close(): its Run fails with a connection-level error, driving run's
	// one-shot reconnect against the now-closed target.
	if _, err := tgt.reconnect(failed); !errors.Is(err, errSSHTargetClosed) {
		t.Fatalf("expected reconnect to reject a closed target, got: %v", err)
	}
	if factoryCalls.Load() != 1 {
		t.Fatalf("expected no additional dial after Close, got %d total dials", factoryCalls.Load())
	}

	if _, err := tgt.clientRunner(); !errors.Is(err, errSSHTargetClosed) {
		t.Fatalf("expected clientRunner to reject use after Close, got: %v", err)
	}
}

// TestIsSSHConnectionError covers each branch isSSHConnectionError checks,
// including wrapped variants and the deliberately-excluded context errors.
func TestIsSSHConnectionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "io.EOF", err: io.EOF, want: true},
		{name: "wrapped io.EOF", err: fmt.Errorf("read: %w", io.EOF), want: true},
		{name: "net.OpError", err: &net.OpError{Op: "read", Net: "tcp", Err: errors.New("boom")}, want: true},
		{name: "wrapped net.OpError", err: fmt.Errorf("run command: %w", &net.OpError{Op: "read", Net: "tcp", Err: errors.New("boom")}), want: true},
		{name: "ssh.ExitMissingError", err: &ssh.ExitMissingError{}, want: true},
		{name: "wrapped ssh.ExitMissingError", err: fmt.Errorf("run: %w", &ssh.ExitMissingError{}), want: true},
		{name: "closed network connection string", err: errors.New("read tcp: use of closed network connection"), want: true},
		{name: "ssh disconnect string", err: errors.New("ssh: disconnect, reason 2: connection lost"), want: true},
		{name: "context.Canceled", err: context.Canceled, want: false},
		{name: "wrapped context.Canceled", err: fmt.Errorf("run: %w", context.Canceled), want: false},
		{name: "context.DeadlineExceeded", err: context.DeadlineExceeded, want: false},
		{name: "wrapped context.DeadlineExceeded", err: fmt.Errorf("run: %w", context.DeadlineExceeded), want: false},
		{name: "unrelated exit status error", err: errors.New("exit status 1"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSSHConnectionError(tc.err); got != tc.want {
				t.Errorf("isSSHConnectionError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
