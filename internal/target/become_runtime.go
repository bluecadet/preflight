package target

import (
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed become_windows_task.ps1
var becomeWindowsTaskScript string

func effectiveBecome(kind RuntimeKind, opts ExecutionOptions) (*BecomeOptions, error) {
	if !opts.Enabled() {
		return nil, nil
	}
	become := *opts.Become
	if strings.EqualFold(strings.TrimSpace(become.User), "SYSTEM") {
		return nil, fmt.Errorf("become: user %q is no longer supported; migrate to a privileged service account with credential elevation (become.method: runas)", become.User)
	}
	switch kind {
	case RuntimeKindWindowsPowerShell:
		// Windows keeps requiring an explicit user — bare become is a POSIX-only
		// root default.
		if strings.TrimSpace(become.User) == "" {
			return nil, fmt.Errorf("become: user is required when enabled on Windows")
		}
		if become.Method == "" {
			become.Method = "runas"
		}
		if become.Method != "runas" {
			return nil, fmt.Errorf("become: unsupported Windows method %q", become.Method)
		}
	case RuntimeKindPOSIXShell:
		// Bare `become: {enabled: true}` means root on POSIX — fixes the former
		// `sudo -u ''` wrap when user is unset. Method stays locked to sudo.
		if strings.TrimSpace(become.User) == "" {
			become.User = "root"
		}
		if become.Method == "" {
			become.Method = "sudo"
		}
		if become.Method != "sudo" {
			return nil, fmt.Errorf("become: unsupported POSIX method %q", become.Method)
		}
	default:
		return nil, fmt.Errorf("become: unsupported runtime %q", kind)
	}
	return &become, nil
}

type windowsTaskBackend struct {
	run       func(context.Context, string, OutputFunc) (string, error)
	copyPlain func(context.Context, string, string) error
	tempDir   string
	become    *BecomeOptions
}

func (b *windowsTaskBackend) RunPowerShellScript(ctx context.Context, script string, out OutputFunc) (string, error) {
	return runWindowsPowerShellScript(ctx, b.run, b.tempDir, script, b.become, out)
}

func (b *windowsTaskBackend) CopyFile(ctx context.Context, src, dst string) error {
	if b.become == nil && b.copyPlain != nil {
		return b.copyPlain(ctx, src, dst)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	pathVar, err := powershellJSONVar("path", dst)
	if err != nil {
		return err
	}
	payloadVar, err := powershellJSONVar("payload", base64.StdEncoding.EncodeToString(data))
	if err != nil {
		return err
	}
	_, err = runWindowsPowerShellScript(ctx, b.run, b.tempDir, pathVar+`
`+payloadVar+`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($path, [Convert]::FromBase64String($payload))
`, b.become, nil)
	return err
}

func (b *windowsTaskBackend) RemoteTempDir() string {
	return b.tempDir
}

func runWindowsPowerShellScript(ctx context.Context, run func(context.Context, string, OutputFunc) (string, error), tempDir, script string, become *BecomeOptions, out OutputFunc) (string, error) {
	if become == nil {
		return run(ctx, script, out)
	}
	if strings.TrimSpace(become.Password) == "" {
		return "", fmt.Errorf("become: password is required for Windows user %q", become.User)
	}
	wrapped, err := buildWindowsCredentialRunner(tempDir, script, become)
	if err != nil {
		return "", err
	}
	return run(ctx, wrapped, out)
}

// buildWindowsCredentialRunner wraps a PowerShell payload so it executes as a
// different local/domain user over a remote transport (WinRM or SSH-to-Windows).
//
// It runs the payload through a one-shot scheduled task rather than
// CreateProcessWithLogonW (System.Diagnostics.Process with alternate
// credentials). A remote transport's session is non-interactive and has no
// window station, so CreateProcessWithLogonW fails child DLL initialization
// with STATUS_DLL_INIT_FAILED (0xC0000142). Task Scheduler runs the action in
// session 0 under its own service, which sidesteps the window-station
// requirement entirely.
//
// The generated script (executed as the connection account) grants the target
// user the batch-logon right, stages the payload, registers and runs the task
// as the target user with its password, waits for completion, replays the
// payload's stdout, and surfaces a non-zero exit code with the captured stderr.
//
// Note: profile loading follows the task's Password logon (a batch logon loads
// the user profile), so become.LoadProfile is not separately honored on this
// path.
func buildWindowsCredentialRunner(tempDir, payload string, become *BecomeOptions) (string, error) {
	tempVar, err := powershellJSONVar("tempRoot", filepath.ToSlash(tempDir))
	if err != nil {
		return "", err
	}
	payloadVar, err := powershellJSONVar("payload", payload)
	if err != nil {
		return "", err
	}
	userVar, err := powershellJSONVar("becomeUser", become.User)
	if err != nil {
		return "", err
	}
	passwordVar, err := powershellJSONVar("becomePassword", become.Password)
	if err != nil {
		return "", err
	}

	return tempVar + "\n" + payloadVar + "\n" + userVar + "\n" + passwordVar + "\n" + becomeWindowsTaskScript, nil
}

type posixTaskBackend struct {
	run              func(context.Context, string, []byte) (string, string, int, error)
	copyPlain        func(context.Context, string, string) error
	readPlain        func(context.Context, string) ([]byte, error)
	powerShellBinary string
	probe            func(context.Context) (Probe, error)
	packageManager   func(context.Context) (string, error)
	become           *BecomeOptions
	// initSystem is the cached POSIX init-system signal from the runtime
	// detection probe, captured when the backend is built in SSHTarget.Execute.
	// It lets the become path report the same prerequisite as the non-become
	// path without a second probe round trip.
	initSystem string
}

func (b *posixTaskBackend) RunPOSIXCommand(ctx context.Context, command string, stdin []byte) (string, string, int, error) {
	command, stdin = wrapPOSIXBecome(command, stdin, b.become)
	stdout, stderr, code, err := b.run(ctx, command, stdin)
	if err == nil && b.become != nil && code != 0 {
		// Classify sudo-specific failures into typed environment errors so
		// the run log carries sudo-password-required / sudo-auth-failed reason
		// codes instead of a generic non-zero-exit message.
		if sudoErr := classifySudoFailure(b.become, stderr); sudoErr != nil {
			err = sudoErr
		}
	}
	return stdout, stderr, code, err
}

func (b *posixTaskBackend) CopyFile(ctx context.Context, src, dst string) error {
	if b.become == nil && b.copyPlain != nil {
		return b.copyPlain(ctx, src, dst)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	command := fmt.Sprintf(
		"tmp=$(mktemp /tmp/preflight-copy.XXXXXX) && trap 'rm -f \"$tmp\"' EXIT && base64 -d > \"$tmp\" && chmod 0644 \"$tmp\" && mkdir -p %q && cp \"$tmp\" %q",
		shellDir(dst), dst,
	)
	_, stderr, code, err := b.RunPOSIXCommand(ctx, command, []byte(encoded))
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("ssh copy exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return nil
}

func (b *posixTaskBackend) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if b.become == nil && b.readPlain != nil {
		return b.readPlain(ctx, path)
	}
	stdout, stderr, code, err := b.RunPOSIXCommand(ctx, fmt.Sprintf("base64 < %q", path), nil)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("ssh read exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(stdout))
}

func (b *posixTaskBackend) PowerShellBinary() string {
	return b.powerShellBinary
}

// Probe returns the cached POSIX detection signals. The probe is gathered on
// the underlying runtime before the become backend is built, so become
// re-uses the same cached result rather than re-probing through sudo.
func (b *posixTaskBackend) Probe(ctx context.Context) (Probe, error) {
	if b.probe == nil {
		return Probe{}, nil
	}
	return b.probe(ctx)
}

// PackageManager returns the cached package-manager fact. The become backend
// delegates to the runtime's cached probe (the same one used without become);
// package-manager detection is a property of the target, not the execution
// identity.
func (b *posixTaskBackend) PackageManager(ctx context.Context) (string, error) {
	if b.packageManager == nil {
		return "", nil
	}
	return b.packageManager(ctx)
}

// InitSystem returns the cached POSIX init-system signal captured when the
// backend is built in SSHTarget.Execute. It lets the become path report the
// same systemd prerequisite as the non-become path without a second probe.
func (b *posixTaskBackend) InitSystem() string {
	return b.initSystem
}

func (b *posixTaskBackend) RunPowerShellScript(ctx context.Context, script string, out OutputFunc) (string, error) {
	if b.powerShellBinary == "" {
		return "", fmt.Errorf("posix-shell runtime: powershell is not available on the remote host")
	}
	stdout, stderr, code, err := b.RunPOSIXCommand(ctx, buildEncodedPowerShellCommand(b.powerShellBinary, script), nil)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("ssh powershell exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	replayBatchOutput(stdout, out)
	return stdout, nil
}

func wrapPOSIXBecome(command string, stdin []byte, become *BecomeOptions) (string, []byte) {
	if become == nil {
		return command, stdin
	}
	if strings.TrimSpace(become.Password) == "" {
		// No password supplied: require NOPASSWD. `sudo -n` makes a
		// password-requiring sudo fail deterministically (non-interactive,
		// no prompt) instead of hanging the run or silently misbehaving.
		wrapped := fmt.Sprintf("sudo -n -u %s /bin/sh -lc %s", shellQuote(become.User), shellQuote(command))
		return wrapped, stdin
	}
	// Password supplied: feed it via sudo -S with an empty prompt.
	wrapped := fmt.Sprintf("sudo -S -p '' -u %s /bin/sh -lc %s", shellQuote(become.User), shellQuote(command))
	withPassword := append([]byte(become.Password+"\n"), stdin...)
	return wrapped, withPassword
}

// classifySudoFailure maps a non-zero sudo exit to a typed BecomeEnvError when
// the failure is a sudo privilege problem (password required by `sudo -n`, or
// a rejected password via `sudo -S`). Other non-zero exits are left to the
// caller as generic command failures. sudo writes its diagnostics to stderr;
// the strings matched are the stable messages emitted by sudo across
// versions.
//
// Classification is best-effort and matches English sudo stderr messages.
// Non-English locales may not be classified and will fall through to a generic
// failure. Locale-stable exit-code classification is a future improvement.
func classifySudoFailure(become *BecomeOptions, stderr string) error {
	if become == nil {
		return nil
	}
	lower := strings.ToLower(stderr)
	// `sudo -n` exits non-zero with "a password is required" when NOPASSWD is
	// not configured and no password was supplied.
	if strings.TrimSpace(become.Password) == "" && strings.Contains(lower, "a password is required") {
		return NewSudoPasswordRequiredError(RuntimeKindPOSIXShell)
	}
	// `sudo -S` with a bad/locked password prints "sorry, try again" (or the
	// "incorrect password" / "authentication failure" variants) and exits
	// non-zero.
	if strings.TrimSpace(become.Password) != "" && (strings.Contains(lower, "sorry, try again") || strings.Contains(lower, "incorrect password") || strings.Contains(lower, "authentication failure")) {
		return NewSudoAuthFailedError(RuntimeKindPOSIXShell)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func runtimeKindForLocal() RuntimeKind {
	if runtime.GOOS == "windows" {
		return RuntimeKindWindowsPowerShell
	}
	return RuntimeKindPOSIXShell
}
