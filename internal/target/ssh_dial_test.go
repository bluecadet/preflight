package target

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDialSSHClient_HandshakeTimesOut verifies that dialSSHClient bounds the
// SSH handshake itself, not just the TCP connect. x/crypto/ssh's own
// ssh.Dial only applies config.Timeout to net.DialTimeout, leaving a
// stalled-but-connected remote (accepts the TCP connection, never speaks)
// able to hang the handshake forever; dialSSHConnBounded fixes this with an
// explicit conn deadline.
func TestDialSSHClient_HandshakeTimesOut(t *testing.T) {
	withSSHUserKeyDir(t, t.TempDir())

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = l.Close() }()
	go func() {
		for {
			conn, acceptErr := l.Accept()
			if acceptErr != nil {
				return
			}
			// Accept but never write or close: simulates a stalled sshd
			// that never completes the version exchange/handshake. conn is
			// intentionally left open; l.Close() at test end is sufficient
			// cleanup for this short-lived test.
			_ = conn
		}
	}()

	host, portStr, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("Atoi: %v", err)
	}

	start := time.Now()
	_, err = dialSSHClient(SSHConfig{
		Host:          host,
		Port:          port,
		Username:      "user",
		Password:      "x",
		Timeout:       200 * time.Millisecond,
		HostKeyPolicy: HostKeyPolicyInsecure,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a handshake timeout error, got nil")
	}
	// Generous upper bound to avoid flakes while still proving the handshake
	// did not hang indefinitely (it would with unbounded ssh.Dial).
	if elapsed > 3*time.Second {
		t.Fatalf("expected the dial to fail quickly, took %s: %v", elapsed, err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "timeout") && !strings.Contains(msg, "deadline") {
		t.Fatalf("expected error to mention a timeout/deadline, got: %v", err)
	}
}

// TestNewSSHTarget_DoesNotMutateConfigPort verifies that NewSSHTarget stores
// the SSHConfig it was given as-is, without defaulting a zero Port to 22:
// that default is applied at dial time by sshAddr instead.
func TestNewSSHTarget_DoesNotMutateConfigPort(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	if got := tgt.Config().Port; got != 0 {
		t.Fatalf("expected NewSSHTarget to leave Port at 0, got %d", got)
	}
}

// TestSSHAddr_DefaultsPort verifies that sshAddr defaults an unset Port to
// 22 when formatting the dial address.
func TestSSHAddr_DefaultsPort(t *testing.T) {
	if got, want := sshAddr(SSHConfig{Host: "h"}), "h:22"; got != want {
		t.Fatalf("sshAddr(%+v) = %q, want %q", SSHConfig{Host: "h"}, got, want)
	}
}
