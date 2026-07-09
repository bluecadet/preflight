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
