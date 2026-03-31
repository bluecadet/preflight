package target

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// fakeAddr is a minimal net.Addr for use in host-key callback tests.
type fakeAddr struct{ addr string }

func (f fakeAddr) Network() string { return "tcp" }
func (f fakeAddr) String() string  { return f.addr }

// TestSSHConfig_KnownHostsFile_Missing verifies that a non-existent
// KnownHostsFile causes the factory to return a descriptive error rather than
// silently falling back to insecure mode.
func TestSSHConfig_KnownHostsFile_Missing(t *testing.T) {
	cfg := SSHConfig{
		Host:           "127.0.0.1",
		Port:           22,
		Username:       "test",
		KnownHostsFile: "/nonexistent/path/known_hosts",
	}
	_, err := defaultSSHRunnerFactory(cfg)
	if err == nil {
		t.Fatal("expected error for missing KnownHostsFile, got nil")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Errorf("error should mention known_hosts, got: %v", err)
	}
}

// TestSSHConfig_KnownHostsFile_Callback verifies that the host-key callback
// loaded from a known_hosts file accepts the registered key and rejects others.
// We test the callback function directly without dialing a real server.
func TestSSHConfig_KnownHostsFile_Callback(t *testing.T) {
	pub1, _, err := generateSSHKeyPair(t)
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	pub2, _, err := generateSSHKeyPair(t)
	if err != nil {
		t.Fatalf("generate second key pair: %v", err)
	}

	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")

	line := knownhosts.Line([]string{"[127.0.0.1]:2222"}, pub1)
	if err := os.WriteFile(khPath, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	cb, err := knownhosts.New(khPath)
	if err != nil {
		t.Fatalf("knownhosts.New: %v", err)
	}

	addrStr := net.JoinHostPort("127.0.0.1", "2222")
	remote := fakeAddr{addrStr}

	if err := cb(addrStr, remote, pub1); err != nil {
		t.Errorf("expected registered key to be accepted, got: %v", err)
	}
	if err := cb(addrStr, remote, pub2); err == nil {
		t.Error("expected unregistered key to be rejected, got nil error")
	}
}

// TestSSHConfig_InsecureMode_NoFileError verifies that when KnownHostsFile is
// empty, the factory does not fail due to a missing file — it proceeds to the
// dial phase (which may fail for other reasons such as no server).
func TestSSHConfig_InsecureMode_NoFileError(t *testing.T) {
	cfg := SSHConfig{
		Host:     "127.0.0.1",
		Port:     65530,
		Username: "test",
		// KnownHostsFile intentionally empty — insecure mode
	}
	_, err := defaultSSHRunnerFactory(cfg)
	// We expect a network/dial error, not a knownhosts file error.
	if err == nil {
		t.Fatal("expected dial error to non-existent server, got nil")
	}
	if strings.Contains(err.Error(), "known_hosts") {
		t.Errorf("insecure mode should not mention known_hosts, got: %v", err)
	}
}

func generateSSHKeyPair(t *testing.T) (ssh.PublicKey, ssh.Signer, error) {
	t.Helper()
	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(edPriv)
	if err != nil {
		return nil, nil, err
	}
	pub, err := ssh.NewPublicKey(edPub)
	if err != nil {
		return nil, nil, err
	}
	return pub, signer, nil
}
