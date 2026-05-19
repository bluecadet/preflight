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
try {
  New-Item -ItemType Directory -Path $workDir -Force | Out-Null
  Set-Content -LiteralPath $payloadPath -Value $payload -Encoding UTF8
  $psi = New-Object System.Diagnostics.ProcessStartInfo
  $psi.FileName = 'powershell.exe'
  $psi.Arguments = '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + $payloadPath + '"'
  $psi.UseShellExecute = $false
  $psi.RedirectStandardOutput = $true
  $psi.RedirectStandardError = $true
  if ($becomeUser -like '*\*') {
    $parts = $becomeUser -split '\\', 2
    $psi.Domain = $parts[0]
    $psi.UserName = $parts[1]
  } else {
    $psi.UserName = $becomeUser
  }
  $psi.Password = ConvertTo-SecureString $becomePassword -AsPlainText -Force
  if ($loadProfile) { $psi.LoadUserProfile = $true }
  $proc = New-Object System.Diagnostics.Process
  $proc.StartInfo = $psi
  [void]$proc.Start()
  $stderrTask = $proc.StandardError.ReadToEndAsync()
  while ($true) {
    $line = $proc.StandardOutput.ReadLine()
    if ($null -eq $line) { break }
    Write-Output $line
    [Console]::Out.Flush()
  }
  $proc.WaitForExit()
  $stderrText = $stderrTask.GetAwaiter().GetResult()
  if ($proc.ExitCode -ne 0) {
    $message = if ($stderrText) { $stderrText } else { '(no stderr)' }
    throw ('runas exited with code ' + $proc.ExitCode + ': ' + $message)
  }
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
