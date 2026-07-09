package target

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestDefaultSSHRunnerFactory_NestedJumpRejected verifies that a jump config
// which itself specifies a jump host (a nested bastion) is rejected with a
// clear error before any dial is attempted.
func TestDefaultSSHRunnerFactory_NestedJumpRejected(t *testing.T) {
	withSSHAuthSock(t, "")
	withSSHUserKeyDir(t, t.TempDir())

	cfg := SSHConfig{
		Host:     "target.example.com",
		Username: "user",
		Password: "x",
		Jump: &SSHConfig{
			Host:     "bastion.example.com",
			Username: "jumpuser",
			Password: "y",
			Jump: &SSHConfig{
				Host: "second-bastion.example.com",
			},
		},
	}

	_, err := defaultSSHRunnerFactory(cfg)
	if err == nil {
		t.Fatal("expected error for nested jump host, got nil")
	}
	if !strings.Contains(err.Error(), "only a single jump hop is supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDefaultSSHRunnerFactory_JumpNoAuthMethod verifies that the jump host's
// own SSHConfig is run through buildSSHClientConfig (identical auth-method
// resolution as any other SSHConfig), and that a jump host with no usable
// auth method fails with the same "no authentication method" error,
// identifying the jump hop, before any network dial is attempted.
func TestDefaultSSHRunnerFactory_JumpNoAuthMethod(t *testing.T) {
	withSSHAuthSock(t, "")
	withSSHUserKeyDir(t, t.TempDir())

	cfg := SSHConfig{
		Host:     "target.example.com",
		Username: "user",
		Password: "x",
		Jump: &SSHConfig{
			Host:     "bastion.example.com",
			Username: "jumpuser",
			// No password, private key, agent, or default key available.
		},
	}

	_, err := defaultSSHRunnerFactory(cfg)
	if err == nil {
		t.Fatal("expected error for jump host with no authentication method, got nil")
	}
	if !strings.Contains(err.Error(), "no authentication method available for host bastion.example.com") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "jump host") {
		t.Fatalf("expected error to identify the jump hop, got: %v", err)
	}
}

// fakeSSHClient is a minimal fake satisfying the sshClient interface used
// internally by sshClientRunner, letting Close() be unit tested without a
// real network connection. When log/mu are set, Close appends name to log
// (guarded by mu) so tests can assert close ordering across multiple fakes.
type fakeSSHClient struct {
	name     string
	log      *[]string
	mu       *sync.Mutex
	closeErr error
}

func (f *fakeSSHClient) NewSession() (*ssh.Session, error) {
	return nil, errors.New("fakeSSHClient: NewSession not implemented")
}

func (f *fakeSSHClient) Close() error {
	if f.log != nil {
		f.mu.Lock()
		*f.log = append(*f.log, f.name)
		f.mu.Unlock()
	}
	return f.closeErr
}

func (f *fakeSSHClient) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	return false, nil, nil
}

// fakeCloser is a minimal io.Closer fake used as sshClientRunner.bastion in
// tests. When log/mu are set, Close appends name to log (guarded by mu) so
// tests can assert close ordering relative to the target client.
type fakeCloser struct {
	name string
	log  *[]string
	mu   *sync.Mutex
	err  error
}

func (f *fakeCloser) Close() error {
	if f.log != nil {
		f.mu.Lock()
		*f.log = append(*f.log, f.name)
		f.mu.Unlock()
	}
	return f.err
}

// TestSSHClientRunner_CloseClosesTargetThenBastion verifies that Close
// closes the target client and the bastion closer, target first.
func TestSSHClientRunner_CloseClosesTargetThenBastion(t *testing.T) {
	var log []string
	var mu sync.Mutex

	runner := &sshClientRunner{
		client:  &fakeSSHClient{name: "target", log: &log, mu: &mu},
		bastion: &fakeCloser{name: "bastion", log: &log, mu: &mu},
	}

	if err := runner.Close(); err != nil {
		t.Fatalf("Close returned unexpected error: %v", err)
	}

	if len(log) != 2 || log[0] != "target" || log[1] != "bastion" {
		t.Fatalf("expected target closed before bastion, got %v", log)
	}
}

// TestSSHClientRunner_CloseJoinsErrors verifies that errors from closing the
// target client and the bastion are both surfaced.
func TestSSHClientRunner_CloseJoinsErrors(t *testing.T) {
	targetErr := errors.New("target close failed")
	bastionErr := errors.New("bastion close failed")

	runner := &sshClientRunner{
		client:  &fakeSSHClient{closeErr: targetErr},
		bastion: &fakeCloser{err: bastionErr},
	}

	err := runner.Close()
	if err == nil {
		t.Fatal("expected joined error, got nil")
	}
	if !errors.Is(err, targetErr) {
		t.Errorf("expected joined error to include target close error: %v", err)
	}
	if !errors.Is(err, bastionErr) {
		t.Errorf("expected joined error to include bastion close error: %v", err)
	}
}

// TestSSHClientRunner_CloseNoBastion verifies that Close works normally
// (direct-connection case) when bastion is nil.
func TestSSHClientRunner_CloseNoBastion(t *testing.T) {
	runner := &sshClientRunner{
		client: &fakeSSHClient{},
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("Close returned unexpected error: %v", err)
	}
}
