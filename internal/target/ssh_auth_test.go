package target

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// closeCloser closes c, ignoring any error, when c is non-nil. Test mirror of
// the production closeAgent helper, used to release the SSH agent connection
// buildSSHClientConfig may return.
func closeCloser(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}

func TestBuildSSHClientConfig_DefaultsTimeoutTo30s(t *testing.T) {
	withSSHUserKeyDir(t, t.TempDir())
	cfg, closer, err := buildSSHClientConfig(SSHConfig{Host: "host", Username: "user", Password: "x"})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	if cfg.Timeout != defaultSSHTimeout {
		t.Fatalf("expected default timeout %s, got %s", defaultSSHTimeout, cfg.Timeout)
	}
}

func TestBuildSSHClientConfig_HonorsExplicitTimeout(t *testing.T) {
	withSSHUserKeyDir(t, t.TempDir())
	want := 5 * time.Second
	cfg, closer, err := buildSSHClientConfig(SSHConfig{Host: "host", Username: "user", Password: "x", Timeout: want})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	if cfg.Timeout != want {
		t.Fatalf("expected timeout %s, got %s", want, cfg.Timeout)
	}
}

// withSSHUserKeyDir overrides the package-level default-key-directory lookup
// for the duration of a test.
func withSSHUserKeyDir(t *testing.T, dir string) {
	t.Helper()
	orig := sshUserKeyDir
	sshUserKeyDir = func() string { return dir }
	t.Cleanup(func() { sshUserKeyDir = orig })
}

// withSSHAuthSock overrides the package-level SSH_AUTH_SOCK lookup for the
// duration of a test.
func withSSHAuthSock(t *testing.T, sock string) {
	t.Helper()
	orig := sshAuthSockEnv
	sshAuthSockEnv = func() string { return sock }
	t.Cleanup(func() { sshAuthSockEnv = orig })
}

// generateEncryptedTestKey returns a PEM-encoded ed25519 private key
// encrypted with the given passphrase.
func generateEncryptedTestKey(t *testing.T, passphrase string) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	if err != nil {
		t.Fatalf("MarshalPrivateKeyWithPassphrase: %v", err)
	}
	return pem.EncodeToMemory(block)
}

func TestBuildSSHClientConfig_EncryptedKeyWithCorrectPassphrase(t *testing.T) {
	withSSHAuthSock(t, "")
	withSSHUserKeyDir(t, t.TempDir())
	keyPEM := generateEncryptedTestKey(t, "s3cret-passphrase")

	cfg, closer, err := buildSSHClientConfig(SSHConfig{
		Host:                 "host",
		Username:             "user",
		PrivateKey:           string(keyPEM),
		PrivateKeyPassphrase: "s3cret-passphrase",
	})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	if len(cfg.Auth) != 1 {
		t.Fatalf("expected 1 auth method (PublicKeys), got %d", len(cfg.Auth))
	}
}

func TestBuildSSHClientConfig_EncryptedKeyWithoutPassphraseErrors(t *testing.T) {
	withSSHAuthSock(t, "")
	withSSHUserKeyDir(t, t.TempDir())
	keyPEM := generateEncryptedTestKey(t, "s3cret-passphrase")

	_, closer, err := buildSSHClientConfig(SSHConfig{
		Host:       "host",
		Username:   "user",
		PrivateKey: string(keyPEM),
	})
	defer closeCloser(closer)
	if err == nil {
		t.Fatal("expected error for encrypted key with no passphrase")
	}
	if !strings.Contains(err.Error(), "private_key_passphrase") {
		t.Fatalf("expected error to mention private_key_passphrase, got: %v", err)
	}
}

func TestBuildSSHClientConfig_DefaultKeyDiscoveryAddsAuthMethod(t *testing.T) {
	withSSHAuthSock(t, "")
	dir := t.TempDir()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519"), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	withSSHUserKeyDir(t, dir)

	cfg, closer, err := buildSSHClientConfig(SSHConfig{Host: "host", Username: "user"})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	if len(cfg.Auth) != 1 {
		t.Fatalf("expected 1 auth method from default key discovery, got %d", len(cfg.Auth))
	}
}

func TestBuildSSHClientConfig_NoAuthMethodsAvailableErrors(t *testing.T) {
	withSSHAuthSock(t, "")
	withSSHUserKeyDir(t, t.TempDir())

	_, closer, err := buildSSHClientConfig(SSHConfig{Host: "kiosk-01", Username: "user"})
	defer closeCloser(closer)
	if err == nil {
		t.Fatal("expected error when no authentication method is available")
	}
	if !strings.Contains(err.Error(), "no authentication method available for host kiosk-01") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// shortTempSockPath returns a path under a short-named temp dir for name.
// Unix socket paths are limited to ~104 bytes on macOS, and t.TempDir()
// embeds the full (often long) test name, so a dedicated short-named temp
// dir is used instead.
func shortTempSockPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "pf-ssh-sock")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

func TestBuildSSHClientConfig_AgentSocketDeadWithPasswordStillBuilds(t *testing.T) {
	withSSHAuthSock(t, shortTempSockPath(t, "does-not-exist.sock"))
	withSSHUserKeyDir(t, t.TempDir())

	cfg, closer, err := buildSSHClientConfig(SSHConfig{Host: "host", Username: "user", Password: "x"})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	if len(cfg.Auth) != 1 {
		t.Fatalf("expected 1 auth method (password), got %d", len(cfg.Auth))
	}
}

func TestBuildSSHClientConfig_AgentAddsAuthMethod(t *testing.T) {
	// Use a short-named temp dir (rather than t.TempDir(), whose path embeds
	// the full test name) since unix socket paths are limited to ~104 bytes
	// on macOS.
	dir, err := os.MkdirTemp("", "pf-ssh-agent")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "agent.sock")
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() { _ = agent.ServeAgent(agent.NewKeyring(), conn) }()
		}
	}()

	withSSHAuthSock(t, sockPath)
	withSSHUserKeyDir(t, t.TempDir())

	cfg, closer, err := buildSSHClientConfig(SSHConfig{Host: "host", Username: "user"})
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	if len(cfg.Auth) != 1 {
		t.Fatalf("expected 1 auth method (agent), got %d", len(cfg.Auth))
	}
	if closer == nil {
		t.Fatal("expected a non-nil closer for the dialed agent connection")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("closer.Close() returned unexpected error: %v", err)
	}
}

func TestBuildSSHClientConfig_AgentOnlyCandidateSurfacesDialError(t *testing.T) {
	withSSHAuthSock(t, shortTempSockPath(t, "does-not-exist.sock"))
	withSSHUserKeyDir(t, t.TempDir())

	_, closer, err := buildSSHClientConfig(SSHConfig{Host: "host", Username: "user"})
	defer closeCloser(closer)
	if err == nil {
		t.Fatal("expected error when the agent is the only auth candidate and dialing fails")
	}
	if !strings.Contains(err.Error(), "agent") {
		t.Fatalf("expected error to mention the agent, got: %v", err)
	}
}
