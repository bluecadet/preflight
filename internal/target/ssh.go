package target

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// defaultSSHTimeout is the connection/handshake timeout used when SSHConfig's
// Timeout field is left at its zero value.
const defaultSSHTimeout = 30 * time.Second

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

type SSHConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	PrivateKey string
	// PrivateKeyPassphrase is the passphrase for an encrypted PrivateKey.
	PrivateKeyPassphrase string
	// KnownHostsFile is the path to a known_hosts file used to verify the
	// remote host key, per HostKeyPolicy. When empty, it defaults to
	// known_hosts under sshUserKeyDir (normally ~/.ssh/known_hosts).
	KnownHostsFile string
	// HostKeyPolicy controls how the remote host key is verified. Valid
	// values are HostKeyPolicyAcceptNew (default), HostKeyPolicyStrict, and
	// HostKeyPolicyInsecure. Any other non-empty value is a configuration
	// error.
	HostKeyPolicy string
	// HostKeyAlgorithms restricts the accepted host key algorithms during the
	// SSH handshake. When nil, the SSH client library's built-in default
	// host-key algorithm list is used. This field applies regardless of
	// HostKeyPolicy.
	HostKeyAlgorithms []string
	// Timeout is the connection/handshake timeout for ssh.Dial. Zero means
	// the 30s default (defaultSSHTimeout) is used.
	Timeout time.Duration
}

type sshRunner interface {
	Run(ctx context.Context, command string, stdin []byte) (stdout string, stderr string, exitCode int, err error)
}

// sshSessionCreator is an optional extension of sshRunner for implementations
// that can open a new multiplexed SSH session on the existing connection. The
// real sshClientRunner satisfies this; test fakes typically do not, so the
// persistent-session path is automatically skipped in tests.
type sshSessionCreator interface {
	NewSession() (*ssh.Session, error)
}

type sshRunnerFactory func(SSHConfig) (sshRunner, error)

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
// default keys discovered under ~/.ssh, then password.
func buildSSHClientConfig(cfg SSHConfig) (*ssh.ClientConfig, error) {
	authMethods := make([]ssh.AuthMethod, 0, 4)

	if cfg.PrivateKey != "" {
		signer, err := parseSSHPrivateKey(cfg.PrivateKey, cfg.PrivateKeyPassphrase)
		if err != nil {
			return nil, err
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	agentMethod, agentErr := sshAgentAuthMethod()
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
			return nil, fmt.Errorf("ssh: no authentication method available for host %s: %w", cfg.Host, agentErr)
		}
		return nil, fmt.Errorf("ssh: no authentication method available for host %s: set password, private_key, or make an SSH agent/default key available", cfg.Host)
	}

	hostKeyCallback, err := buildHostKeyCallback(cfg)
	if err != nil {
		return nil, err
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
	}, nil
}

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
		path = filepath.Join(sshUserKeyDir(), "known_hosts")
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
		return strictHostKeyCallback(cb, path), nil
	case HostKeyPolicyAcceptNew:
		if err := ensureKnownHostsFile(path); err != nil {
			return nil, err
		}
		cb, err := knownhosts.New(path)
		if err != nil {
			return nil, fmt.Errorf("ssh: load known_hosts %q: %w", path, err)
		}
		return acceptNewHostKeyCallback(cb, path), nil
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

// acceptNewHostKeyCallback wraps cb with trust-on-first-use behavior: a host
// with no known_hosts entry is accepted and its key is appended to path,
// while a host with a mismatched entry is rejected as a possible MITM. This
// assumes a single process dials each host at most once per run; concurrent
// appends across processes are not guarded with file locking.
func acceptNewHostKeyCallback(cb ssh.HostKeyCallback, path string) ssh.HostKeyCallback {
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

		line := knownhosts.Line([]string{hostname}, key)
		f, openErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if openErr != nil {
			return fmt.Errorf("ssh: append known_hosts %q: %w", path, openErr)
		}
		defer f.Close()
		if _, writeErr := f.WriteString(line + "\n"); writeErr != nil {
			return fmt.Errorf("ssh: append known_hosts %q: %w", path, writeErr)
		}
		slog.Info("ssh: accepted new host key", "host", hostname, "known_hosts", path)
		return nil
	}
}

// strictHostKeyCallback wraps cb so that both unknown hosts and mismatched
// keys produce clear, actionable errors instead of the terser errors
// returned by the knownhosts package directly.
func strictHostKeyCallback(cb ssh.HostKeyCallback, path string) ssh.HostKeyCallback {
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
		return fmt.Errorf("ssh: host key for %s is not present in known_hosts %q; establish trust first by connecting once with host_key_policy %q, or run `ssh-keyscan -H %s >> %s`: %w", hostname, path, HostKeyPolicyAcceptNew, hostname, path, err)
	}
}

// hostKeyMismatchError formats a possible-MITM error for a host whose
// known_hosts entry does not match the key presented during the handshake.
func hostKeyMismatchError(hostname, path string, cause error) error {
	return fmt.Errorf("ssh: host key for %s does not match the known_hosts entry in %q (possible MITM attack); if this change is expected, remove the stale known_hosts line for %s and reconnect: %w", hostname, path, hostname, cause)
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
// returns an ssh.AuthMethod backed by it. It returns (nil, nil) when
// SSH_AUTH_SOCK is unset, and (nil, err) when the agent is configured but
// the dial fails.
//
// The agent connection is dialed once here and kept open for the lifetime
// of the process; it is a lightweight unix socket and callers do not need
// to close it explicitly.
func sshAgentAuthMethod() (ssh.AuthMethod, error) {
	sockPath := sshAuthSockEnv()
	if sockPath == "" {
		return nil, nil
	}
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("connect to SSH agent at %s: %w", sockPath, err)
	}
	agentClient := agent.NewClient(conn)
	return ssh.PublicKeysCallback(agentClient.Signers), nil
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

var defaultSSHRunnerFactory sshRunnerFactory = func(cfg SSHConfig) (sshRunner, error) {
	clientConfig, err := buildSSHClientConfig(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		return nil, err
	}
	runner := &sshClientRunner{client: client}
	runner.startKeepalive()
	return runner, nil
}

type sshRuntime interface {
	Kind() RuntimeKind
	Registry() ModuleRegistry
	CopyFile(ctx context.Context, src, dst string) error
	ReadFile(ctx context.Context, path string) ([]byte, error)
	Reachable(ctx context.Context) (bool, error)
	Info(ctx context.Context) (TargetInfo, error)
	RunPowerShellScript(ctx context.Context, script string, out OutputFunc) (string, error)
}

type sshCloser interface {
	Close() error
}

// SSHTarget communicates with a remote machine over SSH.
type SSHTarget struct {
	config        SSHConfig
	registry      ModuleRegistry
	runnerFactory sshRunnerFactory
	mu            sync.Mutex
	runner        sshRunner
	runtimeMu     sync.Mutex
	runtime       sshRuntime
}

func NewSSHTarget(cfg SSHConfig, registry ModuleRegistry) *SSHTarget {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	return &SSHTarget{
		config:        cfg,
		registry:      registry,
		runnerFactory: defaultSSHRunnerFactory,
	}
}

// Config returns the SSHConfig that was used to construct this target.
func (t *SSHTarget) Config() SSHConfig {
	return t.config
}

func (t *SSHTarget) Transport() Transport {
	return TransportSSH
}

func (t *SSHTarget) Execute(ctx context.Context, taskID string, module string, params map[string]any, opts ExecutionOptions, dryRun bool, onOutput OutputFunc) (Result, error) {
	runtime, err := t.runtimeForUse(ctx)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}
	become, err := effectiveBecome(runtime.Kind(), opts)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}

	registry := runtime.Registry()
	if become != nil {
		switch rt := runtime.(type) {
		case *sshWindowsPowerShellRuntime:
			backend := &windowsTaskBackend{
				run:       rt.RunPowerShellScript,
				copyPlain: rt.CopyFile,
				tempDir:   rt.RemoteTempDir(),
				become:    become,
			}
			registry = newWindowsPowerShellRegistry(backend)
		case *sshPOSIXShellRuntime:
			backend := &posixTaskBackend{
				run:              rt.RunPOSIXCommand,
				copyPlain:        rt.CopyFile,
				readPlain:        rt.ReadFile,
				powerShellBinary: rt.PowerShellBinary(),
				become:           become,
			}
			registry = newPOSIXShellRegistry(backend)
		}
	}

	return executeModule(ctx, taskID, module, params, dryRun, onOutput, registry, func(module string) error {
		if become != nil {
			return fmt.Errorf("ssh: module %q does not support become", module)
		}
		return t.unsupportedModuleError(module, runtime.Kind())
	})
}

func (t *SSHTarget) CopyFile(ctx context.Context, src, dst string) error {
	runtime, err := t.runtimeForUse(ctx)
	if err != nil {
		return err
	}
	return runtime.CopyFile(ctx, src, dst)
}

func (t *SSHTarget) ReadFile(ctx context.Context, path string) ([]byte, error) {
	runtime, err := t.runtimeForUse(ctx)
	if err != nil {
		return nil, err
	}
	return runtime.ReadFile(ctx, path)
}

func (t *SSHTarget) Reachable(ctx context.Context) (bool, error) {
	runtime, err := t.runtimeForUse(ctx)
	if err != nil {
		return false, err
	}
	return runtime.Reachable(ctx)
}

func (t *SSHTarget) Info(ctx context.Context) (TargetInfo, error) {
	runtime, err := t.runtimeForUse(ctx)
	if err != nil {
		return TargetInfo{}, err
	}
	return runtime.Info(ctx)
}

func (t *SSHTarget) RunPowerShell(ctx context.Context, script string) (string, error) {
	runtime, err := t.runtimeForUse(ctx)
	if err != nil {
		return "", err
	}
	return runtime.RunPowerShellScript(ctx, script, nil)
}

func (t *SSHTarget) Close() error {
	t.runtimeMu.Lock()
	runtime := t.runtime
	t.runtime = nil
	t.runtimeMu.Unlock()

	t.mu.Lock()
	runner := t.runner
	t.runner = nil
	t.mu.Unlock()

	var err error
	if closer, ok := runtime.(sshCloser); ok {
		err = closer.Close()
	}
	if closer, ok := runner.(sshCloser); ok {
		err = errors.Join(err, closer.Close())
	}
	return err
}

func (t *SSHTarget) clientRunner() (sshRunner, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.runner != nil {
		return t.runner, nil
	}
	if t.runnerFactory == nil {
		t.runnerFactory = defaultSSHRunnerFactory
	}
	runner, err := t.runnerFactory(t.config)
	if err != nil {
		return nil, wrapSSHTargetError("connect", err)
	}
	t.runner = runner
	return runner, nil
}

func (t *SSHTarget) runtimeForUse(ctx context.Context) (sshRuntime, error) {
	t.runtimeMu.Lock()
	defer t.runtimeMu.Unlock()
	if t.runtime != nil {
		return t.runtime, nil
	}
	rt, err := t.detectRuntime(ctx)
	if err != nil {
		return nil, err
	}
	t.runtime = rt
	return rt, nil
}

// run is the single funnel all SSH commands go through. On a connection-level
// failure (dropped socket, closed channel, etc.) it drops the cached runner,
// rebuilds it via runnerFactory, and retries the command exactly once. A
// second failure (either the reconnect dial or the retried command) is
// returned as-is; run never loops more than one retry.
//
// This also fixes up sshWindowsPowerShellRuntime's cached psSession
// indirectly: getOrCreatePSSession only ever returns the cached session
// without touching t.runner, so a psSession left over from a dead connection
// is never itself reconnected here. Instead, its next use fails with a
// *psSessionError (write/read on the dead channel), runPSWithFallback resets
// the session and falls back to per-invocation execution, and that fallback
// calls t.run — which is exactly this reconnect path. So the persistent PS
// session is always torn down and rebuilt lazily on top of a working
// connection, without run needing to know about it directly.
func (t *SSHTarget) run(ctx context.Context, command string, stdin []byte) (string, string, int, error) {
	runner, err := t.clientRunner()
	if err != nil {
		return "", "", 0, err
	}
	stdout, stderr, code, err := runner.Run(ctx, command, stdin)
	if err == nil || !isSSHConnectionError(err) {
		return stdout, stderr, code, err
	}

	newRunner, reconnectErr := t.reconnect(runner)
	if reconnectErr != nil {
		return stdout, stderr, code, wrapSSHTargetError("reconnect", reconnectErr)
	}
	return newRunner.Run(ctx, command, stdin)
}

// reconnect drops failed (the runner that just errored with a
// connection-level error) and dials a fresh one via runnerFactory, storing it
// as the new cached runner. If another goroutine has already replaced
// t.runner (e.g. it hit the same dead connection concurrently and reconnected
// first), the already-reconnected runner is reused instead of dialing again.
//
// t.mu is held while dialing (matching clientRunner's existing behavior) but
// is never held while invoking Run, so this cannot deadlock against a
// concurrent call in flight on the runner.
func (t *SSHTarget) reconnect(failed sshRunner) (sshRunner, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.runner != failed && t.runner != nil {
		// Another goroutine already reconnected; reuse its runner.
		return t.runner, nil
	}

	if closer, ok := failed.(sshCloser); ok {
		_ = closer.Close()
	}
	t.runner = nil

	if t.runnerFactory == nil {
		t.runnerFactory = defaultSSHRunnerFactory
	}
	runner, err := t.runnerFactory(t.config)
	if err != nil {
		return nil, err
	}
	t.runner = runner
	return runner, nil
}

// isSSHConnectionError reports whether err indicates the underlying SSH
// transport (TCP socket or SSH channel) has failed, as opposed to a normal
// command-level failure (non-zero exit, script error). Connection-level
// errors are eligible for SSHTarget.run's one-shot reconnect-and-retry.
// context.Canceled and context.DeadlineExceeded are deliberately excluded:
// a cancelled/expired context is the caller giving up, not a dead
// connection, and must not trigger a retry.
func isSSHConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}
	var missing *ssh.ExitMissingError
	if errors.As(err, &missing) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "use of closed network connection") {
		return true
	}
	if strings.Contains(msg, "ssh: disconnect") {
		return true
	}
	return false
}

func (t *SSHTarget) detectRuntime(ctx context.Context) (sshRuntime, error) {
	var posixPowerShellBinary string
	for _, binary := range []string{"powershell.exe", "pwsh", "powershell"} {
		available, isWindows, err := t.probePowerShellBinary(ctx, binary)
		if err != nil {
			return nil, err
		}
		if !available {
			continue
		}
		if isWindows {
			return &sshWindowsPowerShellRuntime{target: t, binary: binary}, nil
		}
		if posixPowerShellBinary == "" {
			posixPowerShellBinary = binary
		}
	}

	stdout, stderr, code, err := t.run(ctx, "printf preflight", nil)
	if err != nil {
		return nil, err
	}
	if code == 0 && strings.TrimSpace(stdout) == "preflight" {
		return &sshPOSIXShellRuntime{target: t, powerShellBinary: posixPowerShellBinary}, nil
	}

	message := strings.TrimSpace(stderr)
	if message == "" {
		message = strings.TrimSpace(stdout)
	}
	if message == "" {
		message = "no supported remote shell runtime detected"
	}
	return nil, wrapSSHTargetError("detect runtime", fmt.Errorf("unable to detect a supported remote runtime: %s", message))
}

func (t *SSHTarget) probePowerShellBinary(ctx context.Context, binary string) (bool, bool, error) {
	stdout, _, code, err := t.run(ctx, buildEncodedPowerShellCommand(binary, `
if ([System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT) {
  Write-Output 'preflight-windows'
} else {
  Write-Output 'preflight-nonwindows'
}
`), nil)
	if err != nil {
		return false, false, err
	}
	if code != 0 {
		return false, false, nil
	}
	switch strings.TrimSpace(stdout) {
	case "preflight-windows":
		return true, true, nil
	case "preflight-nonwindows":
		return true, false, nil
	default:
		return false, false, nil
	}
}

func (t *SSHTarget) unsupportedModuleError(module string, runtimeKind RuntimeKind) error {
	if t.registry != nil {
		mod, ok := t.registry[module]
		if !ok {
			return fmt.Errorf("ssh: unknown module %q", module)
		}
		if _, isPlugin := mod.(PluggableModule); isPlugin {
			return fmt.Errorf("ssh: plugin module %q is not supported yet; use local execution or a staged bundle", module)
		}
	}
	return unsupportedRuntimeModuleError(runtimeKind, module)
}
