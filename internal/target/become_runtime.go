package target

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

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
		if become.Method == "" {
			become.Method = "runas"
		}
		if become.Method != "runas" {
			return nil, fmt.Errorf("become: unsupported Windows method %q", become.Method)
		}
	case RuntimeKindPOSIXShell:
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
	run       func(context.Context, string) (string, error)
	copyPlain func(context.Context, string, string) error
	tempDir   string
	become    *BecomeOptions
}

func (b *windowsTaskBackend) RunPowerShellScript(ctx context.Context, script string) (string, error) {
	return runWindowsPowerShellScript(ctx, b.run, b.tempDir, script, b.become)
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
`, b.become)
	return err
}

func (b *windowsTaskBackend) RemoteTempDir() string {
	return b.tempDir
}

func runWindowsPowerShellScript(ctx context.Context, run func(context.Context, string) (string, error), tempDir, script string, become *BecomeOptions) (string, error) {
	if become == nil {
		return run(ctx, script)
	}
	if strings.TrimSpace(become.Password) == "" {
		return "", fmt.Errorf("become: password is required for Windows user %q", become.User)
	}
	wrapped, err := buildWindowsCredentialRunner(tempDir, script, become)
	if err != nil {
		return "", err
	}
	return run(ctx, wrapped)
}

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
	loadProfile := true
	if become.LoadProfile != nil {
		loadProfile = *become.LoadProfile
	}
	loadProfileVar, err := powershellJSONVar("loadProfile", loadProfile)
	if err != nil {
		return "", err
	}

	return tempVar + `
` + payloadVar + `
` + userVar + `
` + passwordVar + `
` + loadProfileVar + `
$ErrorActionPreference = 'Stop'
$workDir = Join-Path $tempRoot ([guid]::NewGuid().ToString('N'))
$payloadPath = Join-Path $workDir 'payload.ps1'
$stdoutPath = Join-Path $workDir 'stdout.txt'
$stderrPath = Join-Path $workDir 'stderr.txt'
try {
  New-Item -ItemType Directory -Path $workDir -Force | Out-Null
  Set-Content -LiteralPath $payloadPath -Value $payload -Encoding UTF8
  $secure = ConvertTo-SecureString $becomePassword -AsPlainText -Force
  $cred = New-Object System.Management.Automation.PSCredential($becomeUser, $secure)
  $args = @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $payloadPath)
  $start = @{
    FilePath = 'powershell.exe'
    ArgumentList = $args
    Credential = $cred
    RedirectStandardOutput = $stdoutPath
    RedirectStandardError = $stderrPath
    Wait = $true
    PassThru = $true
    WindowStyle = 'Hidden'
  }
  if ($loadProfile) {
    $start.LoadUserProfile = $true
  }
  $proc = Start-Process @start
  $stdout = if (Test-Path -LiteralPath $stdoutPath) { [IO.File]::ReadAllText($stdoutPath) } else { '' }
  $stderr = if (Test-Path -LiteralPath $stderrPath) { [IO.File]::ReadAllText($stderrPath) } else { '' }
  if ($proc.ExitCode -ne 0) {
    $message = if ($stderr) { $stderr } else { $stdout }
    throw ("runas exited with code " + $proc.ExitCode + ": " + $message)
  }
  Write-Output $stdout
} finally {
  Remove-Item -LiteralPath $workDir -Force -Recurse -ErrorAction SilentlyContinue
}
`, nil
}

type posixTaskBackend struct {
	run              func(context.Context, string, []byte) (string, string, int, error)
	copyPlain        func(context.Context, string, string) error
	readPlain        func(context.Context, string) ([]byte, error)
	powerShellBinary string
	become           *BecomeOptions
}

func (b *posixTaskBackend) RunPOSIXCommand(ctx context.Context, command string, stdin []byte) (string, string, int, error) {
	command, stdin = wrapPOSIXBecome(command, stdin, b.become)
	return b.run(ctx, command, stdin)
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

func (b *posixTaskBackend) RunPowerShellScript(ctx context.Context, script string) (string, error) {
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
	return stdout, nil
}

func wrapPOSIXBecome(command string, stdin []byte, become *BecomeOptions) (string, []byte) {
	if become == nil {
		return command, stdin
	}
	wrapped := fmt.Sprintf("sudo -u %s /bin/sh -lc %s", shellQuote(become.User), shellQuote(command))
	if strings.TrimSpace(become.Password) == "" {
		return wrapped, stdin
	}
	wrapped = fmt.Sprintf("sudo -S -p '' -u %s /bin/sh -lc %s", shellQuote(become.User), shellQuote(command))
	withPassword := append([]byte(become.Password+"\n"), stdin...)
	return wrapped, withPassword
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
