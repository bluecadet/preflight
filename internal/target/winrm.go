package target

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/masterzen/winrm"

	"github.com/bluecadet/preflight/internal/winutil"
)

type WinRMConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	HTTPS    bool
	Insecure bool
	Timeout  time.Duration
}

type winRMClient interface {
	RunPSWithContext(ctx context.Context, command string) (string, string, int, error)
	RunCmdWithContext(ctx context.Context, command string) (string, string, int, error)
}

// winRMShellCreator is an optional extension of winRMClient for implementations
// that can create a raw WinRM shell. The real *winrm.Client satisfies this;
// test fakes typically do not, and the persistent-session path is skipped for them.
type winRMShellCreator interface {
	CreateShell() (*winrm.Shell, error)
}

type winRMClientFactory func(WinRMConfig) (winRMClient, error)

var defaultWinRMClientFactory winRMClientFactory = func(cfg WinRMConfig) (winRMClient, error) {
	endpoint := winrm.NewEndpoint(cfg.Host, cfg.Port, cfg.HTTPS, cfg.Insecure, nil, nil, nil, cfg.Timeout)
	return winrm.NewClient(endpoint, cfg.Username, cfg.Password)
}

// WinRMTarget communicates with a remote Windows machine via WinRM.
type WinRMTarget struct {
	config        WinRMConfig
	clientFactory winRMClientFactory
	mu            sync.Mutex
	client        winRMClient
	psSessionMu   sync.Mutex
	psSession     *winRMPersistentPS
}

// winRMPersistentPS holds a single long-running PowerShell process started
// inside a reused WinRM shell. All Check/Apply scripts are serialised through
// it, eliminating per-task shell-create and powershell.exe startup overhead.
type winRMPersistentPS struct {
	shell  *winrm.Shell
	cmd    *winrm.Command
	reader *bufio.Reader
	mu     sync.Mutex
}

// winRMPersistentPSReadTimeout caps how long we wait for a single script's
// completion marker before declaring the session wedged. Without this, a
// PowerShell host that gets stuck (waiting on file I/O, a previous task that
// never returned, a blocked Test-Path, etc.) causes the whole runner to hang
// indefinitely with no actionable error.
const winRMPersistentPSReadTimeout = 90 * time.Second

func (p *winRMPersistentPS) run(ctx context.Context, script string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	id := generateSessionID()
	line := buildPSStdinLine(script, id) + "\n"
	if _, err := p.cmd.Stdin.Write([]byte(line)); err != nil {
		return "", &psSessionError{fmt.Errorf("write stdin: %w", err)}
	}

	type readResult struct {
		out string
		err error
	}
	resultCh := make(chan readResult, 1)
	go func() {
		out, err := readPSOutput(p.reader, id)
		resultCh <- readResult{out: out, err: err}
	}()

	readCtx, cancel := context.WithTimeout(ctx, winRMPersistentPSReadTimeout)
	defer cancel()

	select {
	case r := <-resultCh:
		return r.out, r.err
	case <-readCtx.Done():
		slog.Debug("winrm persistent ps: read timed out, declaring session wedged", "id", id, "timeout", winRMPersistentPSReadTimeout)
		return "", &psSessionError{fmt.Errorf("read stdout: %w (no DONE/ERR marker within %s)", readCtx.Err(), winRMPersistentPSReadTimeout)}
	}
}

func (p *winRMPersistentPS) close() {
	if p.cmd != nil {
		_ = p.cmd.Close()
	}
	if p.shell != nil {
		_ = p.shell.Close()
	}
}

const winRMMaxInlinePowerShellCommandLen = 7000

func NewWinRMTarget(cfg WinRMConfig) *WinRMTarget {
	if cfg.Port == 0 {
		if cfg.HTTPS {
			cfg.Port = 5986
		} else {
			cfg.Port = 5985
		}
	}
	return &WinRMTarget{
		config:        cfg,
		clientFactory: defaultWinRMClientFactory,
	}
}

func (t *WinRMTarget) Transport() Transport {
	return TransportWinRM
}

func (t *WinRMTarget) Execute(ctx context.Context, taskID string, module string, params map[string]any, opts ExecutionOptions, dryRun bool, onOutput OutputFunc) (Result, error) {
	// User-authored powershell scripts can leave the persistent powershell.exe
	// in a wedged state (process-level $env, async output formatting state,
	// in-flight child processes) and can legitimately run longer than the
	// persistent session's completion-marker timeout. Built-in modules use
	// bounded scripts and keep the session for performance; the powershell
	// module is the only user-script vector, so bypass and recycle around it.
	runPS := t.runPS
	if module == "powershell" {
		runPS = t.runPSLegacy
	}
	defer func() {
		if module == "powershell" {
			t.resetPSSession()
		}
	}()

	become, err := effectiveBecome(RuntimeKindWindowsPowerShell, opts)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}
	backend := &windowsTaskBackend{
		run:       runPS,
		copyPlain: t.CopyFile,
		tempDir:   t.RemoteTempDir(),
		become:    become,
	}
	registry := newWindowsPowerShellRegistry(backend)
	return executeRemoteModule(
		ctx,
		taskID,
		module,
		params,
		dryRun,
		onOutput,
		registry,
		func(module string) error {
			if _, ok := registry[module]; ok && become != nil {
				return fmt.Errorf("winrm: module %q does not support become", module)
			}
			return unsupportedRuntimeModuleError(RuntimeKindWindowsPowerShell, module)
		},
	)
}

func (t *WinRMTarget) CopyFile(ctx context.Context, src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("winrm: read src %q: %w", src, err)
	}
	ps, err := t.getOrCreatePSSession(ctx)
	if err == nil && ps != nil {
		err := t.copyBytesViaSession(ctx, ps, data, dst)
		if err == nil {
			return nil
		}
		if !isSessionError(err) {
			return wrapWinRMTargetError(fmt.Sprintf("copy %q -> %q", src, dst), err)
		}
		t.resetPSSession()
	}
	if err := t.copyBytes(ctx, data, dst); err != nil {
		return wrapWinRMTargetError(fmt.Sprintf("copy %q -> %q", src, dst), err)
	}
	return nil
}

func (t *WinRMTarget) ReadFile(ctx context.Context, path string) ([]byte, error) {
	return readRemoteWindowsFile(ctx, t.Transport(), t.runPS, path)
}

func (t *WinRMTarget) Reachable(ctx context.Context) (bool, error) {
	_, err := t.runCmd(ctx, "echo preflight")
	if err != nil {
		return false, err
	}
	return true, nil
}

func (t *WinRMTarget) Info(ctx context.Context) (TargetInfo, error) {
	return remoteWindowsTargetInfo(ctx, t.Transport(), t.runPS)
}

func (t *WinRMTarget) RunPowerShell(ctx context.Context, script string) (string, error) {
	return t.runPS(ctx, script)
}

func (t *WinRMTarget) RunPowerShellScript(ctx context.Context, script string) (string, error) {
	return t.runPS(ctx, script)
}

func (t *WinRMTarget) RemoteTempDir() string {
	return windowsRemoteTempDir
}

func (t *WinRMTarget) clientForUse() (winRMClient, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.client != nil {
		return t.client, nil
	}
	if t.clientFactory == nil {
		t.clientFactory = defaultWinRMClientFactory
	}
	client, err := t.clientFactory(t.config)
	if err != nil {
		return nil, wrapWinRMTargetError("create client", err)
	}
	t.client = client
	return client, nil
}

// getOrCreatePSSession returns the cached persistent PS session, creating it on
// first call. Returns nil (without error) when the underlying client does not
// implement winRMShellCreator (e.g. test fakes), in which case the caller falls
// back to per-command execution.
func (t *WinRMTarget) getOrCreatePSSession(ctx context.Context) (*winRMPersistentPS, error) {
	t.psSessionMu.Lock()
	defer t.psSessionMu.Unlock()
	if t.psSession != nil {
		return t.psSession, nil
	}

	client, err := t.clientForUse()
	if err != nil {
		return nil, err
	}
	creator, ok := client.(winRMShellCreator)
	if !ok {
		return nil, nil // client doesn't support raw shells; use legacy path
	}

	shell, err := creator.CreateShell()
	if err != nil {
		return nil, wrapWinRMTargetError("create persistent shell", err)
	}

	cmd, err := shell.ExecuteWithContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "-")
	if err != nil {
		_ = shell.Close()
		return nil, wrapWinRMTargetError("start persistent powershell", err)
	}

	t.psSession = &winRMPersistentPS{shell: shell, cmd: cmd, reader: bufio.NewReader(cmd.Stdout)}
	return t.psSession, nil
}

func (t *WinRMTarget) resetPSSession() {
	t.psSessionMu.Lock()
	defer t.psSessionMu.Unlock()
	if t.psSession != nil {
		t.psSession.close()
		t.psSession = nil
	}
}

// Close releases the persistent PS session if one was created. The underlying
// WinRM connection is managed by the client and is not explicitly closed.
func (t *WinRMTarget) Close() error {
	t.resetPSSession()
	return nil
}

// runPS executes a PowerShell script on the remote host. It first tries the
// persistent session (one long-lived powershell.exe process per target), which
// avoids the per-task shell-create and process-startup overhead. If the session
// does not exist, cannot be created, or signals a transport failure, it falls
// back to runPSLegacy which opens a fresh shell per invocation.
func (t *WinRMTarget) runPS(ctx context.Context, script string) (string, error) {
	ps, err := t.getOrCreatePSSession(ctx)
	if err == nil && ps != nil {
		out, psErr := ps.run(ctx, script)
		if psErr == nil {
			return out, nil
		}
		if isSessionError(psErr) {
			slog.Debug("winrm runPS: persistent session error, falling back to legacy", "err", psErr)
			t.resetPSSession()
		} else {
			return out, psErr
		}
	}
	return t.runPSLegacy(ctx, script)
}

// runPSLegacy executes a PowerShell script by creating a new WinRM shell per
// invocation. Used when no persistent session is available and as a fallback
// when the persistent session fails.
func (t *WinRMTarget) runPSLegacy(ctx context.Context, script string) (string, error) {
	if shouldStageWinRMPowerShellScript(script) {
		return t.runPSViaTempFile(ctx, script)
	}
	client, err := t.clientForUse()
	if err != nil {
		return "", err
	}
	stdout, stderr, code, err := client.RunPSWithContext(ctx, script)
	if err != nil {
		return "", wrapWinRMTargetError("powershell failed", err)
	}
	if code != 0 {
		if isWinRMCommandLineTooLong(stderr) {
			return t.runPSViaTempFile(ctx, script)
		}
		return "", wrapWinRMTargetError("powershell failed", fmt.Errorf("exited with code %d: %s", code, strings.TrimSpace(stderr)))
	}
	return stdout, nil
}

func (t *WinRMTarget) runCmd(ctx context.Context, command string) (string, error) {
	client, err := t.clientForUse()
	if err != nil {
		return "", err
	}
	stdout, stderr, code, err := client.RunCmdWithContext(ctx, command)
	if err != nil {
		return "", wrapWinRMTargetError("command failed", err)
	}
	if code != 0 {
		return "", wrapWinRMTargetError("command failed", fmt.Errorf("exited with code %d: %s", code, strings.TrimSpace(stderr)))
	}
	return stdout, nil
}

func (t *WinRMTarget) runPSViaTempFile(ctx context.Context, script string) (string, error) {
	remotePath := fmt.Sprintf(`%s\run-%d.ps1`, strings.TrimRight(t.RemoteTempDir(), `\/`), time.Now().UnixNano())

	// Upload: prefer session-based chunking (32 KiB chunks, no new shell per
	// chunk); fall back to the legacy path (1.5 KiB chunks via new shells).
	ps, _ := t.getOrCreatePSSession(ctx)
	var uploaded bool
	if ps != nil {
		if err := t.copyBytesViaSession(ctx, ps, []byte(script), remotePath); err != nil {
			if !isSessionError(err) {
				return "", fmt.Errorf("winrm powershell stage oversized script: %w", err)
			}
			t.resetPSSession()
		} else {
			uploaded = true
		}
	}
	if !uploaded {
		if err := t.copyBytes(ctx, []byte(script), remotePath); err != nil {
			return "", fmt.Errorf("winrm powershell stage oversized script: %w", err)
		}
	}

	defer func() {
		// Cleanup through the persistent session when available; the Remove-Item
		// script is tiny so it always fits inline as a fallback.
		cleanupScript, cleanupErr := powershellJSONVar("path", remotePath)
		if cleanupErr != nil {
			return
		}
		_, _ = t.runPS(ctx, cleanupScript+`Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue`)
	}()

	// Execute via cmd.exe with -ExecutionPolicy Bypass so that unsigned staged
	// PS1 files run correctly regardless of the machine's execution policy.
	// This also preserves the become execution path which relies on runCmd.
	command := fmt.Sprintf(`powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%s"`, remotePath)
	out, err := t.runCmd(ctx, command)
	if err != nil {
		return "", fmt.Errorf("winrm powershell oversized script fallback: %w", err)
	}
	return out, nil
}

func (t *WinRMTarget) runPSDirect(ctx context.Context, script string) (string, error) {
	client, err := t.clientForUse()
	if err != nil {
		return "", err
	}
	stdout, stderr, code, err := client.RunPSWithContext(ctx, script)
	if err != nil {
		return "", wrapWinRMTargetError("powershell failed", err)
	}
	if code != 0 {
		return "", wrapWinRMTargetError("powershell failed", fmt.Errorf("exited with code %d: %s", code, strings.TrimSpace(stderr)))
	}
	return stdout, nil
}

func isWinRMCommandLineTooLong(stderr string) bool {
	return strings.Contains(strings.ToLower(stderr), "command line is too long")
}

func shouldStageWinRMPowerShellScript(script string) bool {
	encoded := encodePowerShellScript(script)
	commandLen := len("powershell.exe -NoProfile -NonInteractive -EncodedCommand ") + len(encoded)
	return commandLen >= winRMMaxInlinePowerShellCommandLen
}

// copyBytesChunkSize is the maximum raw bytes per upload round trip when using
// runPSDirect (legacy path). Each chunk is base64-encoded and inlined into a
// PowerShell script that is UTF-16LE + base64 encoded for -EncodedCommand. The
// WinRM shell (cmd.exe) enforces an ~8 KB command-line limit, so payloads
// above ~1.5 KB trigger "command line is too long". 1536 bytes leaves a
// comfortable margin.
const copyBytesChunkSize = 1536

// copyBytesSessionChunkSize is the maximum raw bytes per upload round trip
// when using the persistent PS session. Scripts are sent via stdin (not
// command-line), so the cmd.exe limit does not apply. The practical ceiling is
// the WinRM max envelope size (150 KB default). A 32 KiB chunk base64-encodes
// to ~43 KiB; after buildPSStdinLine wraps and re-encodes it reaches ~60 KiB,
// well within the 150 KB envelope limit.
const copyBytesSessionChunkSize = 32 * 1024

func (t *WinRMTarget) copyBytes(ctx context.Context, data []byte, dst string) error {
	pathVar, err := powershellJSONVar("path", dst)
	if err != nil {
		return err
	}

	if len(data) <= copyBytesChunkSize {
		// Single round trip: create parent directory and write all bytes at once.
		// base64 uses only A-Za-z0-9+/= which cannot contain the ' delimiter.
		encoded := base64.StdEncoding.EncodeToString(data)
		_, err = t.runPSDirect(ctx, pathVar+fmt.Sprintf(`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($path, [Convert]::FromBase64String('%s'))
`, encoded))
		return err
	}

	if _, err := t.runPSDirect(ctx, pathVar+`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($path, @())
`); err != nil {
		return err
	}

	for start := 0; start < len(data); start += copyBytesChunkSize {
		end := min(start+copyBytesChunkSize, len(data))
		encoded := base64.StdEncoding.EncodeToString(data[start:end])
		appendScript, err := powershellJSONVar("path", dst)
		if err != nil {
			return err
		}
		// encoded is safe to interpolate directly into a single-quoted PS string:
		// base64 uses only A-Za-z0-9+/= which cannot contain the ' delimiter.
		// All other parameters use powershellJSONVar for injection safety.
		if _, err := t.runPSDirect(ctx, appendScript+fmt.Sprintf(`
$bytes = [Convert]::FromBase64String('%s')
$stream = [IO.File]::Open($path, [IO.FileMode]::Append, [IO.FileAccess]::Write, [IO.FileShare]::Read)
try {
  $stream.Write($bytes, 0, $bytes.Length)
} finally {
  $stream.Dispose()
}
`, encoded)); err != nil {
			return err
		}
	}
	return nil
}

// copyBytesViaSession uploads data to dst using the persistent PS session.
// Scripts go through stdin, so chunks can be much larger than the cmd.exe
// command-line limit allows in copyBytes. Falls back gracefully: the caller
// (CopyFile) retries via copyBytes when a *psSessionError is returned.
func (t *WinRMTarget) copyBytesViaSession(ctx context.Context, ps *winRMPersistentPS, data []byte, dst string) error {
	pathVar, err := powershellJSONVar("path", dst)
	if err != nil {
		return err
	}

	if len(data) <= copyBytesSessionChunkSize {
		encoded := base64.StdEncoding.EncodeToString(data)
		_, err = ps.run(ctx, pathVar+fmt.Sprintf(`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($path, [Convert]::FromBase64String('%s'))
`, encoded))
		return err
	}

	if _, err := ps.run(ctx, pathVar+`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($path, @())
`); err != nil {
		return err
	}

	appendPathVar, err := powershellJSONVar("path", dst)
	if err != nil {
		return err
	}
	for start := 0; start < len(data); start += copyBytesSessionChunkSize {
		end := min(start+copyBytesSessionChunkSize, len(data))
		encoded := base64.StdEncoding.EncodeToString(data[start:end])
		if _, err := ps.run(ctx, appendPathVar+fmt.Sprintf(`
$bytes = [Convert]::FromBase64String('%s')
$stream = [IO.File]::Open($path, [IO.FileMode]::Append, [IO.FileAccess]::Write, [IO.FileShare]::Read)
try {
  $stream.Write($bytes, 0, $bytes.Length)
} finally {
  $stream.Dispose()
}
`, encoded)); err != nil {
			return err
		}
	}
	return nil
}

func normalizeEnvScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "user":
		return "User"
	default:
		return "Machine"
	}
}

func normalizeFirewallRuleParams(params map[string]any) (map[string]any, error) {
	normalized := winutil.CloneParams(params)
	ports, err := winutil.NormalizeFirewallPorts(normalized["ports"])
	if err != nil {
		return nil, fmt.Errorf("firewall_rule: %w", err)
	}
	normalized["ports"] = ports
	return normalized, nil
}

func hashLocalFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("hash %q: %w", path, err)
	}
	return hashBytes(data), nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func winRMPackageRemotePath(index int, source string) string {
	return fmt.Sprintf(`%s\%03d-%s`, windowsRemoteTempDir, index, filepath.Base(source))
}
