package target

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// sshAuthSockEnv returns the SSH_AUTH_SOCK environment variable. It is a
// package var so tests can override agent discovery.
var sshAuthSockEnv = func() string {
	return os.Getenv("SSH_AUTH_SOCK")
}

// sshUserKeyDir returns the directory to search for default SSH private
// keys (normally ~/.ssh). It is a package var so tests can override default
// key discovery with a temp directory.
var sshUserKeyDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh")
}

// defaultSSHKeyFiles lists the filenames of default private keys to try, in
// preference order, when no explicit private key or password is configured.
var defaultSSHKeyFiles = []string{"id_ed25519", "id_ecdsa", "id_rsa"}

// buildSSHClientConfig translates an SSHConfig into an ssh.ClientConfig,
// applying the default connection/handshake timeout when Timeout is unset.
//
// Auth methods are tried in this order: explicit private key, SSH agent,
// default keys discovered under ~/.ssh, then password. Default keys are only
// offered when neither an explicit PrivateKey nor a Password is configured —
// an explicit credential is treated as authoritative.
//
// The returned io.Closer, when non-nil, is the SSH agent connection dialed
// while resolving auth methods. Callers must close it once the handshake
// that uses this ClientConfig has completed (success or failure); the agent
// is not needed afterward. It is returned even when buildSSHClientConfig
// itself returns a non-nil error, since the agent may already have been
// dialed by that point.
func buildSSHClientConfig(cfg SSHConfig) (*ssh.ClientConfig, io.Closer, error) {
	authMethods := make([]ssh.AuthMethod, 0, 4)

	if cfg.PrivateKey != "" {
		signer, err := parseSSHPrivateKey(cfg.PrivateKey, cfg.PrivateKeyPassphrase)
		if err != nil {
			return nil, nil, err
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	agentMethod, agentCloser, agentErr := sshAgentAuthMethod()
	if agentMethod != nil {
		authMethods = append(authMethods, agentMethod)
	}

	if cfg.PrivateKey == "" && cfg.Password == "" {
		if signers := defaultSSHSigners(); len(signers) > 0 {
			authMethods = append(authMethods, ssh.PublicKeys(signers...))
		}
	}

	if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}

	if len(authMethods) == 0 {
		if agentErr != nil {
			return nil, agentCloser, fmt.Errorf("ssh: no authentication method available for host %s: %w", cfg.Host, agentErr)
		}
		return nil, agentCloser, fmt.Errorf("ssh: no authentication method available for host %s: set password, private_key, or make an SSH agent/default key available", cfg.Host)
	}

	hostKeyCallback, err := buildHostKeyCallback(cfg)
	if err != nil {
		return nil, agentCloser, err
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultSSHTimeout
	}
	return &ssh.ClientConfig{
		User:              cfg.Username,
		Auth:              authMethods,
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: cfg.HostKeyAlgorithms,
		Timeout:           timeout,
	}, agentCloser, nil
}

// parseSSHPrivateKey parses an inline PEM-encoded private key or, if that
// fails, treats keyOrPath as a file path and reads/parses it from disk. When
// passphrase is non-empty, ssh.ParsePrivateKeyWithPassphrase is used;
// otherwise ssh.ParsePrivateKey is used, and an encrypted key produces a
// clear error pointing at private_key_passphrase.
func parseSSHPrivateKey(keyOrPath, passphrase string) (ssh.Signer, error) {
	signer, err := parseSSHKeyBytes([]byte(keyOrPath), passphrase)
	if err != nil {
		if data, readErr := os.ReadFile(keyOrPath); readErr == nil {
			signer, err = parseSSHKeyBytes(data, passphrase)
		}
	}
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, fmt.Errorf("ssh: private key is encrypted: set private_key_passphrase")
		}
		return nil, fmt.Errorf("ssh: parse private key: %w", err)
	}
	return signer, nil
}

func parseSSHKeyBytes(data []byte, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
	}
	return ssh.ParsePrivateKey(data)
}

// sshAgentAuthMethod dials the SSH agent at sshAuthSockEnv (when set) and
// returns an ssh.AuthMethod backed by it, along with the dialed connection
// as an io.Closer. It returns (nil, nil, nil) when SSH_AUTH_SOCK is unset,
// and (nil, nil, err) when the agent is configured but the dial fails.
//
// The agent connection is dialed fresh on every call; the caller owns it and
// must close it once it is done with the handshake that may use it.
func sshAgentAuthMethod() (ssh.AuthMethod, io.Closer, error) {
	sockPath := sshAuthSockEnv()
	if sockPath == "" {
		return nil, nil, nil
	}
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to SSH agent at %s: %w", sockPath, err)
	}
	agentClient := agent.NewClient(conn)
	return ssh.PublicKeysCallback(agentClient.Signers), conn, nil
}

// defaultSSHSigners looks for id_ed25519, id_ecdsa, and id_rsa (in that
// order) under sshUserKeyDir and returns a signer for each that exists and
// parses as an unencrypted key. Missing, encrypted, or unparsable default
// keys are skipped rather than treated as errors.
func defaultSSHSigners() []ssh.Signer {
	dir := sshUserKeyDir()
	if dir == "" {
		return nil
	}
	var signers []ssh.Signer
	for _, name := range defaultSSHKeyFiles {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			continue
		}
		signers = append(signers, signer)
	}
	return signers
}

// closeAgent closes c, ignoring any error, when c is non-nil. It releases
// the SSH agent connection returned by buildSSHClientConfig once a handshake
// that may have used it has completed (successfully or not); the agent is
// not needed afterward.
func closeAgent(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}
