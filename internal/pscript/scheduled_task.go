package pscript

const ScheduledTaskCheckScript = `
$path = [string]$params.path
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }

function Normalize-TaskFolderPathForCom([string]$taskPath) {
  if (-not $taskPath -or $taskPath -eq '\') {
    return '\'
  }
  return '\' + $taskPath.Trim('\')
}

function Get-TaskFromExactFolder([string]$taskPath, [string]$taskName) {
  $service = New-Object -ComObject 'Schedule.Service'
  $service.Connect()
  try {
    $folder = $service.GetFolder((Normalize-TaskFolderPathForCom $taskPath))
  } catch {
    return $null
  }
  try {
    return $folder.GetTask($taskName)
  } catch {
    return $null
  }
}

function Normalize-TaskText($value) {
  if ($null -eq $value) { return '' }
  return [string]$value
}

function Normalize-PrincipalUserId([string]$userId) {
  if (-not $userId) { return '' }
  switch ($userId.ToUpperInvariant()) {
    'SYSTEM' { return 'SYSTEM' }
    'NT AUTHORITY\SYSTEM' { return 'SYSTEM' }
    'LOCALSERVICE' { return 'LOCALSERVICE' }
    'NT AUTHORITY\LOCALSERVICE' { return 'LOCALSERVICE' }
    'LOCAL SERVICE' { return 'LOCALSERVICE' }
    'NETWORKSERVICE' { return 'NETWORKSERVICE' }
    'NT AUTHORITY\NETWORKSERVICE' { return 'NETWORKSERVICE' }
    'NETWORK SERVICE' { return 'NETWORKSERVICE' }
    default { return $userId }
  }
}

$registeredTask = Get-TaskFromExactFolder $path $name
if ($ensure -eq 'absent') {
  Write-Output ([bool]($registeredTask))
  exit 0
}
if ($null -eq $registeredTask) {
  Write-Output 'true'
  exit 0
}
$task = @(Get-ScheduledTask -TaskPath $path -TaskName $name -ErrorAction SilentlyContinue | Where-Object {
  [string]$_.TaskPath -eq $path -and [string]$_.TaskName -eq $name
}) | Select-Object -First 1
if ($null -eq $task) {
  Write-Output 'true'
  exit 0
}
$execute = [string]$params.execute
$arguments = if ($params.arguments) { [string]$params.arguments } else { '' }
$workingDir = if ($params.working_dir) { [string]$params.working_dir } else { '' }
$trigger = [string]$params.trigger
$runAs = if ($params.run_as) { [string]$params.run_as } else { '' }
$runLevel = if ($params.run_level) { [string]$params.run_level } else { 'least' }
$delay = if ($params.delay) { [string]$params.delay } else { '' }
$enabled = if ($null -ne $params.enabled) { [bool]$params.enabled } else { $true }
$desiredStartAt = if ($params.start_at) { [string]$params.start_at } else { '' }

function Normalize-StartBoundary([string]$triggerName, $value) {
  if (-not $value) { return '' }
  $dt = Get-Date $value
  if ($triggerName -eq 'daily') {
    return $dt.ToString('HH:mm:ss')
  }
  return $dt.ToString('s')
}

$action = $task.Actions | Select-Object -First 1
$currentTriggerObject = $task.Triggers | Select-Object -First 1
$currentTrigger = switch ($currentTriggerObject.CimClass.CimClassName) {
  'MSFT_TaskLogonTrigger' { 'onlogon' }
  'MSFT_TaskBootTrigger' { 'startup' }
  'MSFT_TaskDailyTrigger' { 'daily' }
  'MSFT_TaskTimeTrigger' { 'once' }
  default { '' }
}
$currentDelay = if ($null -ne $currentTriggerObject.Delay) { [string]$currentTriggerObject.Delay } else { '' }
$currentStartAt = Normalize-StartBoundary $currentTrigger $currentTriggerObject.StartBoundary
$desiredStartAt = Normalize-StartBoundary $trigger $desiredStartAt
$currentEnabled = [bool]$task.Settings.Enabled
$currentRunLevel = if ([string]$task.Principal.RunLevel -eq 'Highest') { 'highest' } else { 'least' }

$needs = (Normalize-TaskText $action.Execute) -ne $execute -or
  (Normalize-TaskText $action.Arguments) -ne $arguments -or
  (Normalize-TaskText $action.WorkingDirectory) -ne $workingDir -or
  $currentTrigger -ne $trigger -or
  $currentDelay -ne $delay -or
  $currentEnabled -ne $enabled -or
  $currentRunLevel -ne $runLevel
if ($trigger -eq 'daily' -or $trigger -eq 'once') {
  if ($currentStartAt -ne $desiredStartAt) {
    $needs = $true
  }
}
if ($runAs -and (Normalize-PrincipalUserId ([string]$task.Principal.UserId)) -ne (Normalize-PrincipalUserId $runAs)) {
  $needs = $true
}
$desiredLogonType = switch (Normalize-PrincipalUserId $runAs) {
  'SYSTEM' { 'ServiceAccount' }
  'LOCALSERVICE' { 'ServiceAccount' }
  'NETWORKSERVICE' { 'ServiceAccount' }
  default { 'Interactive' }
}
if ($runAs -and ([string]$task.Principal.LogonType) -ne $desiredLogonType) {
  $needs = $true
}
Write-Output $needs
`

const ScheduledTaskApplyScript = `
$path = [string]$params.path
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }

function Normalize-TaskFolderPathForCom([string]$taskPath) {
  if (-not $taskPath -or $taskPath -eq '\') {
    return '\'
  }
  return '\' + $taskPath.Trim('\')
}

function Ensure-TaskFolder([string]$taskPath) {
  if (-not $taskPath -or $taskPath -eq '\') {
    return
  }
  $service = New-Object -ComObject 'Schedule.Service'
  $service.Connect()
  $currentPath = '\'
  foreach ($segment in $taskPath.Trim('\').Split('\')) {
    if ([string]::IsNullOrWhiteSpace($segment)) {
      continue
    }
    $nextPath = if ($currentPath -eq '\') { '\' + $segment } else { $currentPath + '\' + $segment }
    try {
      $null = $service.GetFolder($nextPath)
    } catch {
      $parent = $service.GetFolder($currentPath)
      try {
        $null = $parent.CreateFolder($segment)
      } catch {
        # Task Scheduler can report "already exists" during fresh folder
        # creation if another process or stale scheduler state creates it first.
        try {
          $null = $service.GetFolder($nextPath)
        } catch {
          throw
        }
      }
    }
    $currentPath = $nextPath
  }
}

function Register-ManagedScheduledTask($taskPrincipal) {
  # Replace the exact managed task before registration. Some Windows builds
  # still throw HRESULT 0x800700B7 with -Force when the task file already exists.
  Unregister-ScheduledTask -TaskPath $path -TaskName $name -Confirm:$false -ErrorAction SilentlyContinue
  if ($null -ne $taskPrincipal) {
    Register-ScheduledTask -TaskPath $path -TaskName $name -Action $action -Trigger $trigger -Principal $taskPrincipal -Force -ErrorAction Stop | Out-Null
  } else {
    Register-ScheduledTask -TaskPath $path -TaskName $name -Action $action -Trigger $trigger -RunLevel $runLevelMap[[string]$params.run_level] -Force -ErrorAction Stop | Out-Null
  }
}

function Normalize-PrincipalUserId([string]$userId) {
  if (-not $userId) { return '' }
  switch ($userId.ToUpperInvariant()) {
    'SYSTEM' { return 'SYSTEM' }
    'NT AUTHORITY\SYSTEM' { return 'SYSTEM' }
    'LOCALSERVICE' { return 'LOCALSERVICE' }
    'NT AUTHORITY\LOCALSERVICE' { return 'LOCALSERVICE' }
    'LOCAL SERVICE' { return 'LOCALSERVICE' }
    'NETWORKSERVICE' { return 'NETWORKSERVICE' }
    'NT AUTHORITY\NETWORKSERVICE' { return 'NETWORKSERVICE' }
    'NETWORK SERVICE' { return 'NETWORKSERVICE' }
    default { return $userId }
  }
}

if ($ensure -eq 'absent') {
  Unregister-ScheduledTask -TaskPath $path -TaskName $name -Confirm:$false -ErrorAction SilentlyContinue
  exit 0
}
Ensure-TaskFolder $path
$triggerName = [string]$params.trigger
$startAt = if ($params.start_at) { [string]$params.start_at } else { '' }
switch ($triggerName) {
  'daily' { $trigger = New-ScheduledTaskTrigger -Daily -At (Get-Date $startAt) }
  'onlogon' { $trigger = New-ScheduledTaskTrigger -AtLogOn }
  'startup' { $trigger = New-ScheduledTaskTrigger -AtStartup }
  'once' { $trigger = New-ScheduledTaskTrigger -Once -At (Get-Date $startAt) }
  default { throw "scheduled_task: unsupported trigger $triggerName" }
}
$delay = if ($params.delay) { [string]$params.delay } else { '' }
if ($delay) {
  $trigger.Delay = $delay
}
$arguments = if ($params.arguments) { [string]$params.arguments } else { '' }
$workingDir = if ($params.working_dir) { [string]$params.working_dir } else { '' }
$actionArgs = @{ Execute = [string]$params.execute }
if ($arguments) { $actionArgs.Argument = $arguments }
if ($workingDir) { $actionArgs.WorkingDirectory = $workingDir }
$action = New-ScheduledTaskAction @actionArgs
$runLevelMap = @{ least = 'Limited'; highest = 'Highest' }
if ($params.run_as) {
  $principalArgs = @{
    UserId = [string]$params.run_as
    RunLevel = $runLevelMap[[string]$params.run_level]
  }
  switch (Normalize-PrincipalUserId $principalArgs.UserId) {
    'SYSTEM' { $principalArgs.LogonType = 'ServiceAccount' }
    'LOCALSERVICE' { $principalArgs.LogonType = 'ServiceAccount' }
    'NETWORKSERVICE' { $principalArgs.LogonType = 'ServiceAccount' }
    default { $principalArgs.LogonType = 'Interactive' }
  }
  $principal = New-ScheduledTaskPrincipal @principalArgs
  Register-ManagedScheduledTask $principal
} else {
  Register-ManagedScheduledTask $null
}
if ($null -ne $params.enabled -and -not [bool]$params.enabled) {
  Disable-ScheduledTask -TaskPath $path -TaskName $name | Out-Null
} else {
  Enable-ScheduledTask -TaskPath $path -TaskName $name | Out-Null
}
$registeredTask = Get-ScheduledTask -TaskPath $path -TaskName $name -ErrorAction SilentlyContinue | Where-Object {
  [string]$_.TaskPath -eq $path -and [string]$_.TaskName -eq $name
} | Select-Object -First 1
if ($null -eq $registeredTask) {
  throw ("scheduled_task: task '" + $name + "' was not registered in '" + $path + "'")
}
`
