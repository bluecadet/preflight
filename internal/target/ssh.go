package target

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type SSHConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	PrivateKey string
	// KnownHostsFile is the path to a known_hosts file used to verify the
	// remote host key. When empty the connection proceeds without host key
	// verification (insecure; only acceptable on isolated networks).
	KnownHostsFile string
	// HostKeyAlgorithms restricts the accepted host key algorithms during the
	// SSH handshake. When nil, the SSH client library's built-in default
	// host-key algorithm list is used. This field applies regardless of
	// whether KnownHostsFile is set.
	HostKeyAlgorithms []string
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

var defaultSSHRunnerFactory sshRunnerFactory = func(cfg SSHConfig) (sshRunner, error) {
	authMethods := make([]ssh.AuthMethod, 0, 2)
	if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}
	if cfg.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		if err != nil {
			if data, readErr := os.ReadFile(cfg.PrivateKey); readErr == nil {
				signer, err = ssh.ParsePrivateKey(data)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("ssh: parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	hostKeyCallback := ssh.InsecureIgnoreHostKey() //nolint:gosec // insecure fallback when KnownHostsFile is not configured
	if cfg.KnownHostsFile != "" {
		cb, err := knownhosts.New(cfg.KnownHostsFile)
		if err != nil {
			return nil, fmt.Errorf("ssh: load known_hosts %q: %w", cfg.KnownHostsFile, err)
		}
		hostKeyCallback = cb
	}
	clientConfig := &ssh.ClientConfig{
		User:              cfg.Username,
		Auth:              authMethods,
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: cfg.HostKeyAlgorithms,
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
	Registry() remoteModuleRegistry
	CopyFile(ctx context.Context, src, dst string) error
	ReadFile(ctx context.Context, path string) ([]byte, error)
	Reachable(ctx context.Context) (bool, error)
	Info(ctx context.Context) (TargetInfo, error)
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
				run: func(ctx context.Context, script string) (string, error) {
					stdout, stderr, code, err := t.run(ctx, buildEncodedPowerShellCommand(rt.binary, script), nil)
					if err != nil {
						return "", err
					}
					if code != 0 {
						return "", fmt.Errorf("ssh powershell exited with code %d: %s", code, strings.TrimSpace(stderr))
					}
					return stdout, nil
				},
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

	return executeRemoteModule(ctx, taskID, module, params, dryRun, onOutput, registry, func(module string) error {
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
		return nil, err
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
	return nil, fmt.Errorf("ssh: unable to detect a supported remote runtime: %s", message)
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
		if _, isPlugin := mod.(*pluginModule); isPlugin {
			return fmt.Errorf("ssh: plugin module %q is not supported yet; use local execution or a staged bundle", module)
		}
	}
	return unsupportedRuntimeModuleError(runtimeKind, module)
}

type sshWindowsPowerShellRuntime struct {
	target      *SSHTarget
	binary      string
	psSessionMu sync.Mutex
	psSession   *sshPersistentPS
}

// sshPersistentPS holds a single long-running PowerShell process started inside
// a reused SSH channel. All Check/Apply scripts are serialised through it,
// eliminating per-task powershell.exe startup overhead (~200–500 ms each).
type sshPersistentPS struct {
	session *ssh.Session
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
}

func (p *sshPersistentPS) run(_ context.Context, script string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	id := generateSessionID()
	line := buildPSStdinLine(script, id) + "\n"
	if _, err := p.stdin.Write([]byte(line)); err != nil {
		return "", &psSessionError{fmt.Errorf("write stdin: %w", err)}
	}
	return readPSOutput(p.scanner, id)
}

func (p *sshPersistentPS) close() {
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.session != nil {
		// Wait for the remote PowerShell process to notice stdin EOF and exit.
		_ = p.session.Wait()
		_ = p.session.Close()
	}
}

func (r *sshWindowsPowerShellRuntime) Kind() RuntimeKind {
	return RuntimeKindWindowsPowerShell
}

func (r *sshWindowsPowerShellRuntime) Registry() remoteModuleRegistry {
	return newWindowsPowerShellRegistry(r)
}

// getOrCreatePSSession returns the cached persistent PS session, creating it on
// first call. Returns nil (without error) when the underlying runner does not
// implement sshSessionCreator (e.g. test fakes), in which case the caller falls
// back to per-command execution.
func (r *sshWindowsPowerShellRuntime) getOrCreatePSSession(ctx context.Context) (*sshPersistentPS, error) {
	r.psSessionMu.Lock()
	defer r.psSessionMu.Unlock()
	if r.psSession != nil {
		return r.psSession, nil
	}

	runner, err := r.target.clientRunner()
	if err != nil {
		return nil, err
	}
	creator, ok := runner.(sshSessionCreator)
	if !ok {
		return nil, nil // runner doesn't support raw sessions; use legacy path
	}

	session, err := creator.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh: create persistent PS session: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("ssh: persistent PS stdin pipe: %w", err)
	}

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		_ = session.Close()
		return nil, fmt.Errorf("ssh: persistent PS stdout pipe: %w", err)
	}

	// Start PowerShell in stdin-reading mode. -Command - causes PS to read
	// and execute commands from stdin until EOF, acting as a persistent REPL.
	cmd := shellQuoteExec(r.binary, []string{"-NoProfile", "-NonInteractive", "-Command", "-"})
	if err := session.Start(cmd); err != nil {
		_ = stdin.Close()
		_ = session.Close()
		return nil, fmt.Errorf("ssh: start persistent powershell: %w", err)
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1 MiB per line; handles large module output
	r.psSession = &sshPersistentPS{session: session, stdin: stdin, scanner: scanner}
	return r.psSession, nil
}

func (r *sshWindowsPowerShellRuntime) resetPSSession() {
	r.psSessionMu.Lock()
	defer r.psSessionMu.Unlock()
	if r.psSession != nil {
		r.psSession.close()
		r.psSession = nil
	}
}

// RunPowerShellScript executes a PowerShell script on the remote Windows host.
// It first tries the persistent session (one long-lived powershell.exe per
// target), which eliminates per-task process-startup overhead. If the session
// cannot be created or signals a transport failure, it falls back to
// runPSLegacy which spawns a fresh PowerShell process per invocation.
func (r *sshWindowsPowerShellRuntime) RunPowerShellScript(ctx context.Context, script string) (string, error) {
	ps, err := r.getOrCreatePSSession(ctx)
	if err == nil && ps != nil {
		out, psErr := ps.run(ctx, script)
		if psErr == nil {
			return out, nil
		}
		if isSessionError(psErr) {
			r.resetPSSession()
		} else {
			return out, psErr
		}
	}
	return r.runPSLegacy(ctx, script)
}

func (r *sshWindowsPowerShellRuntime) runPSLegacy(ctx context.Context, script string) (string, error) {
	stdout, stderr, code, err := r.target.run(ctx, buildEncodedPowerShellCommand(r.binary, script), nil)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("ssh powershell exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

func (r *sshWindowsPowerShellRuntime) CopyFile(ctx context.Context, src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	script, err := powershellJSONVar("path", dst)
	if err != nil {
		return err
	}
	stdout, stderr, code, err := r.target.run(ctx, buildEncodedPowerShellCommand(r.binary, script+`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
$payload = [Console]::In.ReadToEnd()
[IO.File]::WriteAllBytes($path, [Convert]::FromBase64String($payload))
`), []byte(encoded))
	if err != nil {
		return err
	}
	if code != 0 {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = strings.TrimSpace(stdout)
		}
		return fmt.Errorf("ssh copy exited with code %d: %s", code, message)
	}
	return nil
}

func (r *sshWindowsPowerShellRuntime) ReadFile(ctx context.Context, path string) ([]byte, error) {
	script, err := powershellJSONVar("path", path)
	if err != nil {
		return nil, err
	}
	stdout, err := r.RunPowerShellScript(ctx, script+`
if (-not (Test-Path -LiteralPath $path)) {
  throw "file not found: $path"
}
[Convert]::ToBase64String([IO.File]::ReadAllBytes($path))
`)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(stdout))
}

func (r *sshWindowsPowerShellRuntime) Reachable(ctx context.Context) (bool, error) {
	stdout, err := r.RunPowerShellScript(ctx, `Write-Output 'preflight'`)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(stdout) == "preflight", nil
}

func (r *sshWindowsPowerShellRuntime) Info(ctx context.Context) (TargetInfo, error) {
	stdout, err := r.RunPowerShellScript(ctx, `
	$os = Get-CimInstance Win32_OperatingSystem
	$arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
	[pscustomobject]@{
	  hostname = $env:COMPUTERNAME
	  version  = [string]$os.Version
  build    = [string]$os.BuildNumber
  arch     = $arch
} | ConvertTo-Json -Compress
`)
	if err != nil {
		return TargetInfo{}, err
	}
	var payload struct {
		Hostname string `json:"hostname"`
		Version  string `json:"version"`
		Build    string `json:"build"`
		Arch     string `json:"arch"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); err != nil {
		return TargetInfo{}, fmt.Errorf("ssh: parse target info: %w", err)
	}
	return TargetInfo{
		Hostname:  payload.Hostname,
		OSVersion: payload.Version,
		OSBuild:   payload.Build,
		Arch:      normalizeWindowsArch(payload.Arch),
	}, nil
}

func (r *sshWindowsPowerShellRuntime) RemoteTempDir() string {
	return `C:\Windows\Temp\preflight`
}

type sshPOSIXShellRuntime struct {
	target           *SSHTarget
	powerShellBinary string
}

func (r *sshPOSIXShellRuntime) Kind() RuntimeKind {
	return RuntimeKindPOSIXShell
}

func (r *sshPOSIXShellRuntime) Registry() remoteModuleRegistry {
	return newPOSIXShellRegistry(r)
}

func (r *sshPOSIXShellRuntime) RunPOSIXCommand(ctx context.Context, command string, stdin []byte) (string, string, int, error) {
	return r.target.run(ctx, command, stdin)
}

func (r *sshPOSIXShellRuntime) CopyFile(ctx context.Context, src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	cmd := fmt.Sprintf("mkdir -p %q && base64 -d > %q", shellDir(dst), dst)
	stdout, stderr, code, err := r.target.run(ctx, cmd, []byte(encoded))
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("ssh copy exited with code %d: stdout=%q stderr=%q", code, strings.TrimSpace(stdout), strings.TrimSpace(stderr))
	}
	fileMode := info.Mode().Perm()
	if info.Mode()&os.ModeSetuid != 0 {
		fileMode |= 0o4000
	}
	if info.Mode()&os.ModeSetgid != 0 {
		fileMode |= 0o2000
	}
	if info.Mode()&os.ModeSticky != 0 {
		fileMode |= 0o1000
	}
	mode := fmt.Sprintf("%04o", fileMode)
	chmodCmd := fmt.Sprintf("chmod %s %q", mode, dst)
	stdout, stderr, code, err = r.target.run(ctx, chmodCmd, nil)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("ssh chmod exited with code %d: stdout=%q stderr=%q", code, strings.TrimSpace(stdout), strings.TrimSpace(stderr))
	}
	return nil
}

func (r *sshPOSIXShellRuntime) ReadFile(ctx context.Context, path string) ([]byte, error) {
	stdout, _, code, err := r.target.run(ctx, fmt.Sprintf("base64 < %q", path), nil)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("ssh read exited with code %d", code)
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(stdout))
}

func (r *sshPOSIXShellRuntime) Reachable(ctx context.Context) (bool, error) {
	_, _, code, err := r.target.run(ctx, "echo preflight", nil)
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

func (r *sshPOSIXShellRuntime) Info(ctx context.Context) (TargetInfo, error) {
	stdout, _, code, err := r.target.run(ctx, "printf '%s|%s|%s\\n' \"$(hostname)\" \"$(uname -s)\" \"$(uname -m)\"", nil)
	if err != nil {
		return TargetInfo{}, err
	}
	if code != 0 {
		return TargetInfo{}, fmt.Errorf("ssh info exited with code %d", code)
	}
	parts := strings.Split(strings.TrimSpace(stdout), "|")
	if len(parts) != 3 {
		return TargetInfo{}, fmt.Errorf("ssh info: unexpected output %q", stdout)
	}
	return TargetInfo{
		Hostname:  parts[0],
		OSVersion: parts[1],
		Arch:      parts[2],
	}, nil
}

func (r *sshPOSIXShellRuntime) RunPowerShellScript(ctx context.Context, script string) (string, error) {
	if r.powerShellBinary == "" {
		return "", fmt.Errorf("posix-shell runtime: powershell is not available on the remote host")
	}
	stdout, stderr, code, err := r.target.run(ctx, buildEncodedPowerShellCommand(r.powerShellBinary, script), nil)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("ssh powershell exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

func (r *sshPOSIXShellRuntime) PowerShellBinary() string {
	return r.powerShellBinary
}

func buildEncodedPowerShellCommand(binary, script string) string {
	encoded := encodePowerShellScript(script)
	return shellQuoteExec(binary, []string{"-NoProfile", "-NonInteractive", "-EncodedCommand", encoded})
}

func encodePowerShellScript(script string) string {
	codeUnits := utf16.Encode([]rune(script))
	buf := make([]byte, len(codeUnits)*2)
	for i, unit := range codeUnits {
		buf[2*i] = byte(unit)
		buf[2*i+1] = byte(unit >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func shellDir(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return "."
	}
	return path[:idx]
}

func shellQuoteExec(cmd string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, fmt.Sprintf("%q", cmd))
	for _, arg := range args {
		parts = append(parts, fmt.Sprintf("%q", arg))
	}
	return strings.Join(parts, " ")
}

func sshStringSlice(value any) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case []string:
		return typed, nil
	case []any:
		result := make([]string, 0, len(typed))
		for i, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("args[%d] must be string, got %T", i, item)
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("args must be []string, got %T", value)
	}
}

type sshClientRunner struct {
	client *ssh.Client
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
