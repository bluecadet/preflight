package target

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
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
	// Timeout bounds both the TCP connect and the SSH handshake. Zero means
	// the 30s default (defaultSSHTimeout) is used.
	Timeout time.Duration
	// Jump, when set, configures a single-hop SSH bastion (a ProxyJump) to
	// dial through before reaching Host. The jump host has its own
	// independent auth and host-key policy; it does not inherit anything
	// from the target config it fronts. Jump.Jump must be nil — nested
	// (multi-hop) bastions are not supported.
	Jump *SSHConfig
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
	closed        bool
	runtimeMu     sync.Mutex
	runtime       sshRuntime
}

// errSSHTargetClosed is returned by clientRunner and reconnect once Close
// has run, so that a connection-level error on a closed target's in-flight
// call cannot resurrect a runner that nothing will ever close again.
var errSSHTargetClosed = errors.New("ssh: target is closed")

func NewSSHTarget(cfg SSHConfig, registry ModuleRegistry) *SSHTarget {
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
	t.closed = true
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
	if t.closed {
		return nil, errSSHTargetClosed
	}
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

	if t.closed {
		if closer, ok := failed.(sshCloser); ok {
			_ = closer.Close()
		}
		return nil, errSSHTargetClosed
	}

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
