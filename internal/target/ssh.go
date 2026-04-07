package target

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	runtime       sshRuntime
	runtimeOnce   sync.Once
	runtimeErr    error
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

func (t *SSHTarget) Execute(ctx context.Context, taskID string, module string, params map[string]any, dryRun bool, onOutput OutputFunc) (Result, error) {
	runtime, err := t.runtimeForUse(ctx)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}
	return executeRemoteModule(ctx, taskID, module, params, dryRun, onOutput, runtime.Registry(), func(module string) error {
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
	t.runtimeOnce.Do(func() {
		t.runtime, t.runtimeErr = t.detectRuntime(ctx)
	})
	return t.runtime, t.runtimeErr
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
	target *SSHTarget
	binary string
}

func (r *sshWindowsPowerShellRuntime) Kind() RuntimeKind {
	return RuntimeKindWindowsPowerShell
}

func (r *sshWindowsPowerShellRuntime) Registry() remoteModuleRegistry {
	return newWindowsPowerShellRegistry(r)
}

func (r *sshWindowsPowerShellRuntime) RunPowerShellScript(ctx context.Context, script string) (string, error) {
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
$arch = $os.OSArchitecture
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
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	cmd := fmt.Sprintf("mkdir -p %q && base64 -d > %q", shellDir(dst), dst)
	_, _, code, err := r.target.run(ctx, cmd, []byte(encoded))
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("ssh copy exited with code %d", code)
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
