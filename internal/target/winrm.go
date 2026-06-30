package target

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

// winRMStreamRunner is an optional extension of winRMClient for implementations
// that can write stdout/stderr to arbitrary io.Writer values. The real
// *winrm.Client satisfies this via RunWithContextWithInput; test fakes
// typically do not, so runPSLegacy falls back to RunPSWithContext (batch mode).
type winRMStreamRunner interface {
	RunWithContextWithInput(ctx context.Context, command string, stdout, stderr io.Writer, stdin io.Reader) (int, error)
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
	roundTrips    atomic.Int64
}

// RoundTripCount returns the number of WinRM round-trips made so far. It is
// safe to call concurrently and can be queried at any point during or after
// execution.
func (t *WinRMTarget) RoundTripCount() int64 {
	return t.roundTrips.Load()
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

func (p *winRMPersistentPS) run(ctx context.Context, script string, out OutputFunc) (string, error) {
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
		result, err := readPSOutput(p.reader, id, out)
		resultCh <- readResult{out: result, err: err}
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
	become, err := effectiveBecome(RuntimeKindWindowsPowerShell, opts)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}

	// Modules tagged freshSession (e.g. powershell) run unbounded user-authored
	// scripts that can leave a long-lived powershell.exe wedged. Route them
	// through the per-invocation legacy path and recycle the persistent
	// session afterwards. Built-in modules use bounded scripts and stay on the
	// persistent session for performance.
	runPS := t.runPS
	if windowsPowerShellModuleRequiresFreshSession(module) {
		runPS = t.runPSLegacy
		defer t.resetPSSession()
	}

	backend := &windowsTaskBackend{
		run:       runPS,
		copyPlain: t.CopyFile,
		tempDir:   t.RemoteTempDir(),
		become:    become,
	}
	registry := newWindowsPowerShellRegistry(backend)
	return executeModule(
		ctx,
		taskID,
		module,
		params,
		dryRun,
		onOutput,
		registry,
		func(module string) error {
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
	return readRemoteWindowsFile(ctx, t.Transport(), func(ctx context.Context, script string) (string, error) {
		return t.runPS(ctx, script, nil)
	}, path)
}

func (t *WinRMTarget) Reachable(ctx context.Context) (bool, error) {
	_, err := t.runCmd(ctx, "echo preflight")
	if err != nil {
		return false, err
	}
	return true, nil
}

func (t *WinRMTarget) Info(ctx context.Context) (TargetInfo, error) {
	return remoteWindowsTargetInfo(ctx, t.Transport(), func(ctx context.Context, script string) (string, error) {
		return t.runPS(ctx, script, nil)
	})
}

func (t *WinRMTarget) RunPowerShell(ctx context.Context, script string) (string, error) {
	return t.runPS(ctx, script, nil)
}

func (t *WinRMTarget) RunPowerShellScript(ctx context.Context, script string) (string, error) {
	return t.runPS(ctx, script, nil)
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

	t.roundTrips.Add(1)
	shell, err := creator.CreateShell()
	if err != nil {
		return nil, wrapWinRMTargetError("create persistent shell", err)
	}

	t.roundTrips.Add(1)
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
func (t *WinRMTarget) runPS(ctx context.Context, script string, out OutputFunc) (string, error) {
	return runPSWithFallback(ctx, script, out,
		func(ctx context.Context) (psSessionRunner, error) {
			ps, err := t.getOrCreatePSSession(ctx)
			if ps == nil {
				return nil, err
			}
			return ps, err
		},
		func(cause error) {
			slog.Debug("winrm runPS: persistent session error, falling back to legacy", "err", cause)
			t.resetPSSession()
		},
		t.runPSLegacy,
	)
}

// lineStreamWriter splits incoming bytes into lines and forwards each complete
// line to out as it arrives. All bytes are also accumulated in all for the
// final return value. Trailing \r is trimmed from each line (WinRM sends CRLF).
// Write and flush are safe for concurrent use.
type lineStreamWriter struct {
	mu      sync.Mutex
	out     OutputFunc
	all     strings.Builder
	pending strings.Builder
}

func (w *lineStreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.all.Write(p)
	for _, b := range p {
		if b == '\n' {
			line := strings.TrimSuffix(w.pending.String(), "\r")
			w.pending.Reset()
			if w.out != nil {
				w.out(line)
			}
		} else {
			w.pending.WriteByte(b)
		}
	}
	return len(p), nil
}

// flush emits any trailing bytes that did not end with a newline.
func (w *lineStreamWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending.Len() > 0 && w.out != nil {
		w.out(strings.TrimSuffix(w.pending.String(), "\r"))
		w.pending.Reset()
	}
}

// runPSLegacy executes a PowerShell script by creating a new WinRM shell per
// invocation. Used when no persistent session is available and as a fallback
// when the persistent session fails.
func (t *WinRMTarget) runPSLegacy(ctx context.Context, script string, out OutputFunc) (string, error) {
	if shouldStageWinRMPowerShellScript(script) {
		stdout, err := t.runPSViaTempFile(ctx, script)
		if err != nil {
			return "", err
		}
		replayBatchOutput(stdout, out)
		return stdout, nil
	}
	client, err := t.clientForUse()
	if err != nil {
		return "", err
	}

	// When the client supports streaming output (the real *winrm.Client does),
	// use RunWithContextWithInput so lines reach out as they arrive. Fall back
	// to the batch RunPSWithContext path for test fakes that only implement the
	// minimal winRMClient interface.
	//
	// Prepend [Console]::Out.AutoFlush = $true so PowerShell flushes stdout
	// after every Write-Output rather than buffering until the process exits.
	// Without this, streaming is defeated because small writes accumulate in
	// .NET's StreamWriter buffer (~4 KB) and only arrive when the buffer is
	// full or powershell.exe terminates.
	if streamer, ok := client.(winRMStreamRunner); ok && out != nil {
		encoded := winrm.Powershell("[Console]::Out.AutoFlush = $true;" + script)
		if encoded == "" {
			return "", wrapWinRMTargetError("powershell failed", fmt.Errorf("cannot encode script"))
		}
		sw := &lineStreamWriter{out: out}
		var errBuf bytes.Buffer
		t.roundTrips.Add(1)
		code, err := streamer.RunWithContextWithInput(ctx, encoded, sw, &errBuf, nil)
		sw.flush()
		if err != nil {
			return "", wrapWinRMTargetError("powershell failed", err)
		}
		stderr := errBuf.String()
		if code != 0 {
			if isWinRMCommandLineTooLong(stderr) {
				stdout, err := t.runPSViaTempFile(ctx, script)
				if err != nil {
					return "", err
				}
				replayBatchOutput(stdout, out)
				return stdout, nil
			}
			return "", wrapWinRMTargetError("powershell failed", fmt.Errorf("exited with code %d: %s", code, strings.TrimSpace(stderr)))
		}
		return sw.all.String(), nil
	}

	t.roundTrips.Add(1)
	stdout, stderr, code, err := client.RunPSWithContext(ctx, script)
	if err != nil {
		return "", wrapWinRMTargetError("powershell failed", err)
	}
	if code != 0 {
		if isWinRMCommandLineTooLong(stderr) {
			stdout, err := t.runPSViaTempFile(ctx, script)
			if err != nil {
				return "", err
			}
			replayBatchOutput(stdout, out)
			return stdout, nil
		}
		return "", wrapWinRMTargetError("powershell failed", fmt.Errorf("exited with code %d: %s", code, strings.TrimSpace(stderr)))
	}
	replayBatchOutput(stdout, out)
	return stdout, nil
}

func (t *WinRMTarget) runCmd(ctx context.Context, command string) (string, error) {
	client, err := t.clientForUse()
	if err != nil {
		return "", err
	}
	t.roundTrips.Add(1)
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
		_, _ = t.runPS(ctx, cleanupScript+`Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue`, nil)
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
	t.roundTrips.Add(1)
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
	return uploadBytesChunked(ctx, data, dst, copyBytesChunkSize, t.runPSDirect)
}

func (t *WinRMTarget) copyBytesViaSession(ctx context.Context, ps *winRMPersistentPS, data []byte, dst string) error {
	return uploadBytesChunked(ctx, data, dst, copyBytesSessionChunkSize, func(ctx context.Context, script string) (string, error) {
		return ps.run(ctx, script, nil)
	})
}

// uploadBytesChunked writes data to dst on a remote Windows host by base64-
// encoding and inlining each chunk into a PowerShell script. The submit
// callback delivers the script via whichever transport path the caller chose
// (legacy per-command runPSDirect with its small cmd.exe ceiling, or the
// persistent stdin-driven PS session with a much larger envelope). Both paths
// previously had near-identical create-then-append loops; this is that loop.
//
// encoded chunk strings are safe to interpolate directly into a single-quoted
// PS literal: base64 uses only A-Za-z0-9+/= which cannot contain the '
// delimiter. All other parameters go through powershellJSONVar.
func uploadBytesChunked(
	ctx context.Context,
	data []byte,
	dst string,
	chunkSize int,
	submit func(ctx context.Context, script string) (string, error),
) error {
	pathVar, err := powershellJSONVar("path", dst)
	if err != nil {
		return err
	}

	if len(data) <= chunkSize {
		encoded := base64.StdEncoding.EncodeToString(data)
		_, err = submit(ctx, pathVar+fmt.Sprintf(`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($path, [Convert]::FromBase64String('%s'))
`, encoded))
		return err
	}

	if _, err := submit(ctx, pathVar+`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($path, @())
`); err != nil {
		return err
	}

	for start := 0; start < len(data); start += chunkSize {
		end := min(start+chunkSize, len(data))
		encoded := base64.StdEncoding.EncodeToString(data[start:end])
		if _, err := submit(ctx, pathVar+fmt.Sprintf(`
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
