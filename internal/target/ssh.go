package target

import (
	"context"
	"errors"
	"fmt"
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

type SSHConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	PrivateKey string
	// PrivateKeyPassphrase is the passphrase for an encrypted PrivateKey.
	PrivateKeyPassphrase string
	// KnownHostsFile is the path to a known_hosts file used to verify the
	// remote host key. When empty the connection proceeds without host key
	// verification (insecure; only acceptable on isolated networks).
	KnownHostsFile string
	// HostKeyAlgorithms restricts the accepted host key algorithms during the
	// SSH handshake. When nil, the SSH client library's built-in default
	// host-key algorithm list is used. This field applies regardless of
	// whether KnownHostsFile is set.
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

	hostKeyCallback := ssh.InsecureIgnoreHostKey() //nolint:gosec // insecure fallback when KnownHostsFile is not configured
	if cfg.KnownHostsFile != "" {
		cb, err := knownhosts.New(cfg.KnownHostsFile)
		if err != nil {
			return nil, fmt.Errorf("ssh: load known_hosts %q: %w", cfg.KnownHostsFile, err)
		}
		hostKeyCallback = cb
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
	return &sshClientRunner{client: client}, nil
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

func (t *SSHTarget) run(ctx context.Context, command string, stdin []byte) (string, string, int, error) {
	runner, err := t.clientRunner()
	if err != nil {
		return "", "", 0, err
	}
	return runner.Run(ctx, command, stdin)
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
