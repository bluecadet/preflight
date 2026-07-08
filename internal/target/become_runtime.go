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

	return tempVar + `
` + payloadVar + `
` + userVar + `
` + passwordVar + `
$ErrorActionPreference = 'Stop'

# Grant the target account the batch-logon right so a Password-logon scheduled
# task can start it. LsaAddAccountRights is additive and idempotent.
$lsaType = @'
using System;
using System.Runtime.InteropServices;
public static class PreflightLsa {
  [StructLayout(LayoutKind.Sequential)]
  public struct LSA_UNICODE_STRING { public ushort Length; public ushort MaximumLength; public IntPtr Buffer; }
  [StructLayout(LayoutKind.Sequential)]
  public struct LSA_OBJECT_ATTRIBUTES { public int Length; public IntPtr RootDirectory; public IntPtr ObjectName; public int Attributes; public IntPtr SecurityDescriptor; public IntPtr SecurityQualityOfService; }
  [DllImport("advapi32.dll", SetLastError=true)]
  public static extern uint LsaOpenPolicy(IntPtr sys, ref LSA_OBJECT_ATTRIBUTES oa, int access, out IntPtr handle);
  [DllImport("advapi32.dll", SetLastError=true)]
  public static extern uint LsaAddAccountRights(IntPtr policy, byte[] sid, LSA_UNICODE_STRING[] rights, int count);
  [DllImport("advapi32.dll")]
  public static extern uint LsaClose(IntPtr h);
  [DllImport("advapi32.dll")]
  public static extern int LsaNtStatusToWinError(uint status);
}
'@
Add-Type -TypeDefinition $lsaType | Out-Null

function Grant-PreflightBatchLogon($account) {
  $sid = (New-Object System.Security.Principal.NTAccount($account)).Translate([System.Security.Principal.SecurityIdentifier])
  $sidBytes = New-Object byte[] $sid.BinaryLength
  $sid.GetBinaryForm($sidBytes, 0)
  $oa = New-Object PreflightLsa+LSA_OBJECT_ATTRIBUTES
  $oa.Length = [System.Runtime.InteropServices.Marshal]::SizeOf($oa)
  $POLICY_ALL = 0x00000FFF
  $h = [IntPtr]::Zero
  $st = [PreflightLsa]::LsaOpenPolicy([IntPtr]::Zero, [ref]$oa, $POLICY_ALL, [ref]$h)
  if ($st -ne 0) { throw ('become: LsaOpenPolicy failed ' + [PreflightLsa]::LsaNtStatusToWinError($st)) }
  try {
    $right = 'SeBatchLogonRight'
    $lus = New-Object PreflightLsa+LSA_UNICODE_STRING
    $lus.Buffer = [System.Runtime.InteropServices.Marshal]::StringToHGlobalUni($right)
    $lus.Length = [uint16]($right.Length * 2)
    $lus.MaximumLength = [uint16](($right.Length + 1) * 2)
    $st = [PreflightLsa]::LsaAddAccountRights($h, $sidBytes, @($lus), 1)
    [System.Runtime.InteropServices.Marshal]::FreeHGlobal($lus.Buffer)
    if ($st -ne 0) { throw ('become: LsaAddAccountRights failed ' + [PreflightLsa]::LsaNtStatusToWinError($st)) }
  } finally { [void][PreflightLsa]::LsaClose($h) }
}
Grant-PreflightBatchLogon $becomeUser | Out-Null

$workDir = Join-Path $tempRoot ([guid]::NewGuid().ToString('N'))
$payloadPath = Join-Path $workDir 'payload.ps1'
$stdoutPath = Join-Path $workDir 'stdout.txt'
$stderrPath = Join-Path $workDir 'stderr.txt'
$taskName = 'Preflight-Become-' + [guid]::NewGuid().ToString('N')
try {
  New-Item -ItemType Directory -Path $workDir -Force | Out-Null

  # Grant the target user Modify on the work dir (inherited) so the task can
  # read the payload and write the captured stdout/stderr files.
  $acl = Get-Acl -LiteralPath $workDir
  $rule = New-Object System.Security.AccessControl.FileSystemAccessRule($becomeUser, 'Modify', 'ContainerInherit,ObjectInherit', 'None', 'Allow')
  $acl.AddAccessRule($rule)
  Set-Acl -LiteralPath $workDir -AclObject $acl

  Set-Content -LiteralPath $payloadPath -Value $payload -Encoding UTF8

  # cmd.exe redirection to files is reliable in the task's non-interactive
  # session 0, and its exit code (the payload's) surfaces as LastTaskResult.
  $cmdArgs = '/c powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + $payloadPath + '" > "' + $stdoutPath + '" 2> "' + $stderrPath + '"'
  $action = New-ScheduledTaskAction -Execute 'cmd.exe' -Argument $cmdArgs
  $principal = New-ScheduledTaskPrincipal -UserId $becomeUser -LogonType Password -RunLevel Highest
  $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Hours 1)
  $task = New-ScheduledTask -Action $action -Principal $principal -Settings $settings
  Register-ScheduledTask -TaskName $taskName -InputObject $task -User $becomeUser -Password $becomePassword -Force | Out-Null

  Start-ScheduledTask -TaskName $taskName
  # 267011 = SCHED_S_TASK_HAS_NOT_RUN, 267009 = SCHED_S_TASK_RUNNING
  $deadline = (Get-Date).AddMinutes(65)
  do {
    Start-Sleep -Milliseconds 500
    $info = Get-ScheduledTask -TaskName $taskName -ErrorAction Stop | Get-ScheduledTaskInfo
    $result = $info.LastTaskResult
  } while (($result -eq 267011 -or $result -eq 267009) -and (Get-Date) -lt $deadline)

  $stderrText = ''
  if (Test-Path -LiteralPath $stderrPath) { $stderrText = (Get-Content -LiteralPath $stderrPath -Raw) }

  if ($result -eq 267011 -or $result -eq 267009) {
    throw ('become: scheduled task did not complete within the time limit (last result ' + $result + ')')
  }
  if ($result -ne 0) {
    $message = if ($stderrText) { $stderrText } else { '(no stderr)' }
    throw ('runas exited with code ' + $result + ': ' + $message)
  }

  if (Test-Path -LiteralPath $stdoutPath) {
    $outText = Get-Content -LiteralPath $stdoutPath -Raw
    if ($null -ne $outText) { [Console]::Out.Write($outText) }
  }
} finally {
  Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue
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
