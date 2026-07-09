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

// TestSSHConfig_Strict_MissingFile_Errors verifies that a non-existent
// KnownHostsFile under the strict policy causes the factory to return a
// descriptive error rather than silently proceeding or auto-creating the
// file.
func TestSSHConfig_Strict_MissingFile_Errors(t *testing.T) {
	cfg := SSHConfig{
		Host:           "127.0.0.1",
		Port:           22,
		Username:       "test",
		Password:       "x",
		KnownHostsFile: "/nonexistent/path/known_hosts",
		HostKeyPolicy:  HostKeyPolicyStrict,
	}
	_, err := defaultSSHRunnerFactory(cfg)
	if err == nil {
		t.Fatal("expected error for missing KnownHostsFile under strict policy, got nil")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Errorf("error should mention known_hosts, got: %v", err)
	}
	if !strings.Contains(err.Error(), "accept-new") && !strings.Contains(err.Error(), "ssh-keyscan") {
		t.Errorf("error should offer trust guidance, got: %v", err)
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

// TestSSHConfig_Insecure_AcceptsAnyKey verifies that the insecure policy
// accepts any host key.
func TestSSHConfig_Insecure_AcceptsAnyKey(t *testing.T) {
	cfg, closer, err := buildSSHClientConfig(SSHConfig{
		Host:          "127.0.0.1",
		Username:      "test",
		Password:      "x",
		HostKeyPolicy: HostKeyPolicyInsecure,
	})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig: %v", err)
	}

	pub1, _, err := generateSSHKeyPair(t)
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	addrStr := net.JoinHostPort("127.0.0.1", "2222")
	remote := fakeAddr{addrStr}
	if err := cfg.HostKeyCallback(addrStr, remote, pub1); err != nil {
		t.Errorf("expected insecure policy to accept any key, got: %v", err)
	}
}

// TestSSHConfig_InvalidHostKeyPolicy_Errors verifies that an unrecognized
// HostKeyPolicy value is rejected as a configuration error.
func TestSSHConfig_InvalidHostKeyPolicy_Errors(t *testing.T) {
	_, closer, err := buildSSHClientConfig(SSHConfig{
		Host:          "127.0.0.1",
		Username:      "test",
		Password:      "x",
		HostKeyPolicy: "bogus",
	})
	defer closeCloser(closer)
	if err == nil {
		t.Fatal("expected error for invalid host_key_policy, got nil")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("expected error to mention the invalid value, got: %v", err)
	}
}

// TestSSHConfig_AcceptNew_UnknownHost_AppendsAndAccepts verifies that under
// the accept-new policy, an unknown host is accepted and its key is appended
// to the known_hosts file so that subsequent connections recognize it.
func TestSSHConfig_AcceptNew_UnknownHost_AppendsAndAccepts(t *testing.T) {
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

	cfg, closer, err := buildSSHClientConfig(SSHConfig{
		Host:           "127.0.0.1",
		Username:       "test",
		Password:       "x",
		KnownHostsFile: khPath,
		HostKeyPolicy:  HostKeyPolicyAcceptNew,
	})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig: %v", err)
	}

	addrStr := net.JoinHostPort("127.0.0.1", "2222")
	remote := fakeAddr{addrStr}

	if err := cfg.HostKeyCallback(addrStr, remote, pub1); err != nil {
		t.Fatalf("expected unknown host to be accepted, got: %v", err)
	}

	data, err := os.ReadFile(khPath)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if !strings.Contains(string(data), "127.0.0.1") {
		t.Errorf("expected known_hosts to contain accepted host, got: %s", data)
	}

	// A freshly loaded callback from the now-updated file should accept the
	// appended key and reject a different one.
	reloaded, err := knownhosts.New(khPath)
	if err != nil {
		t.Fatalf("knownhosts.New: %v", err)
	}
	if err := reloaded(addrStr, remote, pub1); err != nil {
		t.Errorf("expected appended key to be accepted on reload, got: %v", err)
	}
	if err := reloaded(addrStr, remote, pub2); err == nil {
		t.Error("expected a different key to be rejected on reload")
	}
}

// TestSSHConfig_AcceptNew_KnownHostKeyMismatch_Errors verifies that under the
// accept-new policy, a host with an existing known_hosts entry whose key does
// not match is rejected with a clear error rather than silently trusted.
func TestSSHConfig_AcceptNew_KnownHostKeyMismatch_Errors(t *testing.T) {
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
	addrStr := net.JoinHostPort("127.0.0.1", "2222")
	line := knownhosts.Line([]string{addrStr}, pub1)
	if err := os.WriteFile(khPath, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	cfg, closer, err := buildSSHClientConfig(SSHConfig{
		Host:           "127.0.0.1",
		Username:       "test",
		Password:       "x",
		KnownHostsFile: khPath,
		HostKeyPolicy:  HostKeyPolicyAcceptNew,
	})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig: %v", err)
	}

	remote := fakeAddr{addrStr}
	err = cfg.HostKeyCallback(addrStr, remote, pub2)
	if err == nil {
		t.Fatal("expected error for mismatched host key, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") && !strings.Contains(err.Error(), "MITM") {
		t.Errorf("expected error to mention mismatch/MITM, got: %v", err)
	}
}

// TestSSHConfig_Strict_UnknownHost_Errors verifies that under the strict
// policy, a host that is not present in the known_hosts file is rejected
// with guidance on how to establish trust.
func TestSSHConfig_Strict_UnknownHost_Errors(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(khPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	cfg, closer, err := buildSSHClientConfig(SSHConfig{
		Host:           "127.0.0.1",
		Username:       "test",
		Password:       "x",
		KnownHostsFile: khPath,
		HostKeyPolicy:  HostKeyPolicyStrict,
	})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig: %v", err)
	}

	pub1, _, err := generateSSHKeyPair(t)
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	addrStr := net.JoinHostPort("127.0.0.1", "2222")
	remote := fakeAddr{addrStr}
	err = cfg.HostKeyCallback(addrStr, remote, pub1)
	if err == nil {
		t.Fatal("expected error for unknown host under strict policy, got nil")
	}
	if !strings.Contains(err.Error(), "accept-new") && !strings.Contains(err.Error(), "ssh-keyscan") {
		t.Errorf("expected trust guidance in error, got: %v", err)
	}
}

// TestSSHConfig_DefaultPolicyIsAcceptNew verifies that an empty HostKeyPolicy
// behaves like accept-new: unknown hosts are accepted and appended, using the
// default known_hosts path derived from sshUserKeyDir.
func TestSSHConfig_DefaultPolicyIsAcceptNew(t *testing.T) {
	withSSHUserKeyDir(t, t.TempDir())

	cfg, closer, err := buildSSHClientConfig(SSHConfig{
		Host:     "127.0.0.1",
		Username: "test",
		Password: "x",
		// HostKeyPolicy intentionally left empty.
	})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig: %v", err)
	}

	pub1, _, err := generateSSHKeyPair(t)
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	addrStr := net.JoinHostPort("127.0.0.1", "2222")
	remote := fakeAddr{addrStr}
	if err := cfg.HostKeyCallback(addrStr, remote, pub1); err != nil {
		t.Fatalf("expected default policy to accept unknown host, got: %v", err)
	}

	khPath := filepath.Join(sshUserKeyDir(), "known_hosts")
	data, err := os.ReadFile(khPath)
	if err != nil {
		t.Fatalf("read default known_hosts: %v", err)
	}
	if !strings.Contains(string(data), "127.0.0.1") {
		t.Errorf("expected default known_hosts to contain accepted host, got: %s", data)
	}
}

// TestSSHConfig_DefaultKnownHostsUnresolvableHome_Errors verifies that when
// KnownHostsFile is unset and sshUserKeyDir cannot determine a home
// directory (HOME unset), buildSSHClientConfig fails with a clear error
// instead of silently deriving a CWD-relative known_hosts path.
func TestSSHConfig_DefaultKnownHostsUnresolvableHome_Errors(t *testing.T) {
	withSSHUserKeyDir(t, "")

	_, closer, err := buildSSHClientConfig(SSHConfig{
		Host:     "127.0.0.1",
		Username: "test",
		Password: "x",
		// KnownHostsFile and HostKeyPolicy intentionally left empty.
	})
	defer closeCloser(closer)
	if err == nil {
		t.Fatal("expected error when the default known_hosts home directory cannot be determined")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("expected error to mention known_hosts, got: %v", err)
	}
}

// TestSSHConfig_InsecurePolicy_NoFileError verifies that the insecure policy
// does not require or touch a known_hosts file — the factory proceeds
// straight to the dial phase (which may fail for other reasons, such as no
// server listening).
func TestSSHConfig_InsecurePolicy_NoFileError(t *testing.T) {
	cfg := SSHConfig{
		Host:          "127.0.0.1",
		Port:          65530,
		Username:      "test",
		Password:      "x",
		HostKeyPolicy: HostKeyPolicyInsecure,
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
