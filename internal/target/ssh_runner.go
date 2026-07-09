package target

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshKeepaliveInterval is the interval between keepalive requests sent on an
// established SSH connection. It is fixed (not user-configurable) and is a
// package var only so tests can drive the keepalive loop faster than 30s.
var sshKeepaliveInterval = 30 * time.Second

// sshKeepaliveConn is the minimal surface of *ssh.Client needed to send
// keepalive requests, extracted so sshKeepaliveLoop can be unit tested with a
// stub instead of a real network connection.
type sshKeepaliveConn interface {
	SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error)
}

// sshKeepaliveLoop sends a keepalive@openssh.com global request on conn every
// interval until stop is closed. Two consecutive failed requests are treated
// as a dead connection: onRepeatedFailure is invoked (the real caller wires
// this to close the underlying client, so the next command over the cached
// runner fails fast and triggers SSHTarget's reconnect path) and the loop
// exits, since further keepalives on an already-failed connection are
// pointless.
func sshKeepaliveLoop(conn sshKeepaliveConn, interval time.Duration, stop <-chan struct{}, onRepeatedFailure func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	consecutiveFailures := 0
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if _, _, err := conn.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				consecutiveFailures++
				if consecutiveFailures >= 2 {
					onRepeatedFailure()
					return
				}
				continue
			}
			consecutiveFailures = 0
		}
	}
}

// sshClient is the subset of *ssh.Client's methods used by sshClientRunner,
// extracted so sshClientRunner can be unit tested (in particular Close's
// close-ordering behavior) with a fake instead of a real network connection.
// *ssh.Client satisfies this interface.
type sshClient interface {
	sshKeepaliveConn
	NewSession() (*ssh.Session, error)
	Close() error
}

type sshClientRunner struct {
	client sshClient

	// bastion is the jump-host connection this runner's target client was
	// tunneled through, when SSHConfig.Jump was set. It is closed after the
	// target client in Close. Nil for a direct connection.
	bastion io.Closer

	stopKeepalive chan struct{}
	closeOnce     sync.Once
	closeErr      error
}

// newSSHClientRunner wraps client (optionally tunneled through bastion, when
// SSHConfig.Jump was set) in an sshClientRunner and starts its keepalive
// goroutine. bastion is nil for a direct connection.
func newSSHClientRunner(client *ssh.Client, bastion io.Closer) *sshClientRunner {
	runner := &sshClientRunner{client: client, bastion: bastion}
	runner.startKeepalive()
	return runner
}

// startKeepalive launches the keepalive goroutine for this runner's client.
// It must be called at most once per runner (newSSHClientRunner calls it
// right after dialing). Keepalive requests are only sent on the target
// client, never on the bastion directly: they flow over the tunnel carried
// by the bastion's TCP connection, so activity on the target client also
// keeps the bastion connection alive.
func (r *sshClientRunner) startKeepalive() {
	r.stopKeepalive = make(chan struct{})
	go sshKeepaliveLoop(r.client, sshKeepaliveInterval, r.stopKeepalive, func() {
		slog.Warn("ssh: keepalive failed twice in a row, closing connection")
		_ = r.Close()
	})
}

// Close stops the keepalive goroutine (if running) and closes the underlying
// target client, then the bastion connection (if any). It is safe to call
// multiple times, including concurrently from the keepalive goroutine itself
// when it self-closes after repeated failures.
func (r *sshClientRunner) Close() error {
	r.closeOnce.Do(func() {
		if r.stopKeepalive != nil {
			close(r.stopKeepalive)
		}
		r.closeErr = r.client.Close()
		if r.bastion != nil {
			r.closeErr = errors.Join(r.closeErr, r.bastion.Close())
		}
	})
	return r.closeErr
}

// NewSession opens a new multiplexed channel on the existing SSH connection.
// Implements sshSessionCreator to enable the persistent PowerShell session.
func (r *sshClientRunner) NewSession() (*ssh.Session, error) {
	return r.client.NewSession()
}

func (r *sshClientRunner) Run(ctx context.Context, command string, stdin []byte) (string, string, int, error) {
	session, err := r.client.NewSession()
	if err != nil {
		return "", "", 0, err
	}
	defer func() {
		_ = session.Close()
	}()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if stdin != nil {
		session.Stdin = bytes.NewReader(stdin)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return stdout.String(), stderr.String(), 0, ctx.Err()
	case err := <-errCh:
		if err == nil {
			return stdout.String(), stderr.String(), 0, nil
		}
		if exitErr, ok := err.(*ssh.ExitError); ok {
			return stdout.String(), stderr.String(), exitErr.ExitStatus(), nil
		}
		return stdout.String(), stderr.String(), 0, err
	}
}
