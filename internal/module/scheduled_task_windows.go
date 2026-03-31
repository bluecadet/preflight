//go:build windows

package module

import "context"

type ScheduledTaskModule struct{}

func (m *ScheduledTaskModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	if _, err := paramStringRequired(params, "name"); err != nil {
		return false, err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return false, err
	}

	return runWindowsPowerShellBool(ctx, params, `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$task = Get-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue
if ($ensure -eq 'absent') {
  Write-Output ($null -ne $task)
  exit 0
}
if ($null -eq $task) {
  Write-Output 'true'
  exit 0
}
$command = [string]$params.command
$trigger = [string]$params.trigger
$user = if ($params.user) { [string]$params.user } else { '' }
$action = $task.Actions | Select-Object -First 1
$currentTrigger = ''
if ($task.Triggers | Where-Object { $_.AtLogOn }) { $currentTrigger = 'onlogon' }
elseif ($task.Triggers | Where-Object { $_.CimClass.CimClassName -eq 'MSFT_TaskBootTrigger' }) { $currentTrigger = 'startup' }
elseif ($task.Triggers | Where-Object { $_.CimClass.CimClassName -eq 'MSFT_TaskDailyTrigger' }) { $currentTrigger = 'daily' }
$needs = $action.Execute -ne 'cmd.exe' -or $action.Arguments -ne ("/c " + $command) -or $currentTrigger -ne $trigger
if ($user -and $task.Principal.UserId -ne $user) {
  $needs = $true
}
Write-Output $needs
`)
}

func (m *ScheduledTaskModule) Apply(ctx context.Context, params map[string]any) error {
	if _, err := paramStringRequired(params, "name"); err != nil {
		return err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return err
	}

	_, err := runWindowsPowerShellWithParams(ctx, params, `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Unregister-ScheduledTask -TaskName $name -Confirm:$false -ErrorAction SilentlyContinue
  exit 0
}
$triggerName = [string]$params.trigger
switch ($triggerName) {
  'daily' { $trigger = New-ScheduledTaskTrigger -Daily -At 3am }
  'onlogon' { $trigger = New-ScheduledTaskTrigger -AtLogOn }
  'startup' { $trigger = New-ScheduledTaskTrigger -AtStartup }
  default { throw "scheduled_task: unsupported trigger $triggerName" }
}
$action = New-ScheduledTaskAction -Execute 'cmd.exe' -Argument ("/c " + [string]$params.command)
if ($params.user) {
  Register-ScheduledTask -TaskName $name -Action $action -Trigger $trigger -User ([string]$params.user) -Force | Out-Null
} else {
  Register-ScheduledTask -TaskName $name -Action $action -Trigger $trigger -Force | Out-Null
}
`)
	return err
}
