package target

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Host-key verification policies for SSHConfig.HostKeyPolicy.
const (
	// HostKeyPolicyAcceptNew verifies against KnownHostsFile; an unknown host
	// is trusted on first use and its key is appended to the file, while a
	// known host with a mismatched key is rejected. This is the default.
	HostKeyPolicyAcceptNew = "accept-new"
	// HostKeyPolicyStrict verifies against KnownHostsFile only; both unknown
	// hosts and mismatched keys are rejected.
	HostKeyPolicyStrict = "strict"
	// HostKeyPolicyInsecure disables host-key verification entirely.
	HostKeyPolicyInsecure = "insecure"
)

// buildHostKeyCallback constructs the ssh.HostKeyCallback for cfg according
// to its HostKeyPolicy (defaulting to HostKeyPolicyAcceptNew when unset).
func buildHostKeyCallback(cfg SSHConfig) (ssh.HostKeyCallback, error) {
	policy := cfg.HostKeyPolicy
	if policy == "" {
		policy = HostKeyPolicyAcceptNew
	}

	if policy == HostKeyPolicyInsecure {
		slog.Warn("ssh: host key verification disabled", "host", cfg.Host)
		return ssh.InsecureIgnoreHostKey(), nil //nolint:gosec // explicit opt-in via host_key_policy: insecure
	}

	path := cfg.KnownHostsFile
	if path == "" {
		dir := sshUserKeyDir()
		if dir == "" {
			return nil, errors.New("ssh: cannot determine home directory for default known_hosts file; set known_hosts_file or host_key_policy: insecure")
		}
		path = filepath.Join(dir, "known_hosts")
	}

	switch policy {
	case HostKeyPolicyStrict:
		if _, statErr := os.Stat(path); statErr != nil {
			return nil, fmt.Errorf("ssh: known_hosts file %q not found; establish trust first by connecting once with host_key_policy %q, or run `ssh-keyscan -H %s >> %s`: %w", path, HostKeyPolicyAcceptNew, cfg.Host, path, statErr)
		}
		cb, err := knownhosts.New(path)
		if err != nil {
			return nil, fmt.Errorf("ssh: load known_hosts %q: %w", path, err)
		}
		return verifyingHostKeyCallback(cb, path, func(hostname string, _ ssh.PublicKey, cause error) error {
			return fmt.Errorf("ssh: host key for %s is not present in known_hosts %q; establish trust first by connecting once with host_key_policy %q, or run `ssh-keyscan -H %s >> %s`: %w", hostname, path, HostKeyPolicyAcceptNew, hostname, path, cause)
		}), nil
	case HostKeyPolicyAcceptNew:
		if err := ensureKnownHostsFile(path); err != nil {
			return nil, err
		}
		cb, err := knownhosts.New(path)
		if err != nil {
			return nil, fmt.Errorf("ssh: load known_hosts %q: %w", path, err)
		}
		return verifyingHostKeyCallback(cb, path, func(hostname string, key ssh.PublicKey, _ error) error {
			line := knownhosts.Line([]string{hostname}, key)
			f, openErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if openErr != nil {
				return fmt.Errorf("ssh: append known_hosts %q: %w", path, openErr)
			}
			if _, writeErr := f.WriteString(line + "\n"); writeErr != nil {
				_ = f.Close()
				return fmt.Errorf("ssh: append known_hosts %q: %w", path, writeErr)
			}
			if closeErr := f.Close(); closeErr != nil {
				return fmt.Errorf("ssh: append known_hosts %q: %w", path, closeErr)
			}
			slog.Info("ssh: accepted new host key", "host", hostname, "known_hosts", path)
			return nil
		}), nil
	default:
		return nil, fmt.Errorf("ssh: invalid host_key_policy %q for host %s: must be %q, %q, or %q", cfg.HostKeyPolicy, cfg.Host, HostKeyPolicyAcceptNew, HostKeyPolicyStrict, HostKeyPolicyInsecure)
	}
}

// ensureKnownHostsFile creates path, and its parent directory (mode 0700), as
// an empty file when it does not already exist, so that a fresh accept-new
// known_hosts file does not cause knownhosts.New to fail before any host has
// been trusted.
func ensureKnownHostsFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("ssh: stat known_hosts %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("ssh: create known_hosts directory for %q: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("ssh: create known_hosts file %q: %w", path, err)
	}
	return f.Close()
}

// verifyingHostKeyCallback wraps cb, the callback loaded from a known_hosts
// file, so that both of the file-backed host-key policies (accept-new and
// strict) get clear, actionable errors instead of the terser ones returned
// by the knownhosts package directly. Both policies treat a key mismatch
// (keyErr.Want non-empty — a possible MITM) identically, via
// hostKeyMismatchError; they differ only in how they handle a host with no
// known_hosts entry at all (keyErr.Want empty), which is where they diverge
// and is left to onUnknown: accept-new trusts the host on first use and
// appends its key to path (this assumes a single process dials each host at
// most once per run; concurrent appends across processes are not guarded
// with file locking), while strict rejects it with guidance on how to
// establish trust.
func verifyingHostKeyCallback(cb ssh.HostKeyCallback, path string, onUnknown func(hostname string, key ssh.PublicKey, cause error) error) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return err
		}
		if len(keyErr.Want) > 0 {
			return hostKeyMismatchError(hostname, path, err)
		}
		return onUnknown(hostname, key, err)
	}
}

// hostKeyMismatchError formats a possible-MITM error for a host whose
// known_hosts entry does not match the key presented during the handshake.
func hostKeyMismatchError(hostname, path string, cause error) error {
	return fmt.Errorf("ssh: host key for %s does not match the known_hosts entry in %q (possible MITM attack); if this change is expected, remove the stale known_hosts line for %s and reconnect: %w", hostname, path, hostname, cause)
}
