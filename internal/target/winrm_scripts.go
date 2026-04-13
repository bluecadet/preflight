package target

const registryCheckScript = `
$path = [string]$params.path
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
function Normalize-RegistryKind([string]$kind) {
  switch ($kind.ToLowerInvariant()) {
    'expandstring' { return 'expand_string' }
    'multistring' { return 'multi_string' }
    default { return $kind.ToLowerInvariant() }
  }
}
if ($ensure -eq 'absent') {
  Write-Output (Test-Path -LiteralPath $path)
  exit 0
}
if (-not (Test-Path -LiteralPath $path)) {
  if (-not $params.values) {
    Write-Output 'true'
    exit 0
  }
  $presentSpecs = @($params.values | Where-Object { -not $_.ensure -or $_.ensure -eq 'present' })
  Write-Output ([bool]($presentSpecs.Count -gt 0))
  exit 0
}
$needs = $false
if ($params.values) {
  $item = Get-Item -LiteralPath $path
  $props = Get-ItemProperty -LiteralPath $path
  foreach ($spec in $params.values) {
    $name = [string]$spec.name
    $ensureValue = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
    $prop = $props.PSObject.Properties[$name]
    if ($ensureValue -eq 'absent') {
      if ($null -ne $prop) {
        $needs = $true
        break
      }
      continue
    }
    if ($null -eq $prop) {
      $needs = $true
      break
    }
    $currentKind = Normalize-RegistryKind($item.GetValueKind($name).ToString())
    $desiredKind = [string]$spec.type
    if ($currentKind -ne $desiredKind) {
      $needs = $true
      break
    }
    switch ($desiredKind) {
      'string' { if ([string]$prop.Value -ne [string]$spec.data) { $needs = $true } }
      'expand_string' { if ([string]$prop.Value -ne [string]$spec.data) { $needs = $true } }
      'dword' { if ([int64]$prop.Value -ne [int64]$spec.data) { $needs = $true } }
      'qword' { if ([int64]$prop.Value -ne [int64]$spec.data) { $needs = $true } }
      'multi_string' {
        $current = @($prop.Value | ForEach-Object { [string]$_ })
        $desired = @($spec.data | ForEach-Object { [string]$_ })
        if ($current.Count -ne $desired.Count) {
          $needs = $true
        } else {
          for ($i = 0; $i -lt $current.Count; $i++) {
            if ($current[$i] -ne $desired[$i]) {
              $needs = $true
              break
            }
          }
        }
      }
      'binary' {
        $current = @($prop.Value | ForEach-Object { [int]$_ })
        $desired = @($spec.data | ForEach-Object { [int]$_ })
        if ($current.Count -ne $desired.Count) {
          $needs = $true
        } else {
          for ($i = 0; $i -lt $current.Count; $i++) {
            if ($current[$i] -ne $desired[$i]) {
              $needs = $true
              break
            }
          }
        }
      }
      default { throw "registry: unsupported type $desiredKind" }
    }
    if ($needs) {
      break
    }
  }
}
Write-Output $needs
`

const registryApplyScript = `
$path = [string]$params.path
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-Item -LiteralPath $path -Recurse -Force -ErrorAction SilentlyContinue
  exit 0
}
if (-not (Test-Path -LiteralPath $path)) {
  New-Item -Path $path -Force | Out-Null
}
if ($params.values) {
  foreach ($spec in $params.values) {
    $name = [string]$spec.name
    $ensureValue = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
    if ($ensureValue -eq 'absent') {
      Remove-ItemProperty -LiteralPath $path -Name $name -Force -ErrorAction SilentlyContinue
      continue
    }
    $kindMap = @{
      string = 'String'
      expand_string = 'ExpandString'
      dword = 'DWord'
      qword = 'QWord'
      multi_string = 'MultiString'
      binary = 'Binary'
    }
    $value = switch ([string]$spec.type) {
      'multi_string' { @($spec.data | ForEach-Object { [string]$_ }) }
      'binary' { [byte[]]@($spec.data | ForEach-Object { [byte][int]$_ }) }
      'dword' { [int]$spec.data }
      'qword' { [int64]$spec.data }
      default { $spec.data }
    }
    Remove-ItemProperty -LiteralPath $path -Name $name -Force -ErrorAction SilentlyContinue
    New-ItemProperty -LiteralPath $path -Name $name -Value $value -PropertyType $kindMap[[string]$spec.type] -Force | Out-Null
  }
}
`

// registryEnsureScript combines check and apply in one PowerShell invocation.
// It outputs "ok", "changed", or "would-change" (dry-run). $__pf_dry_run must
// be injected by the caller before $params.
const registryEnsureScript = `
$path = [string]$params.path
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
function Normalize-RegistryKind([string]$kind) {
  switch ($kind.ToLowerInvariant()) {
    'expandstring' { return 'expand_string' }
    'multistring'  { return 'multi_string' }
    default        { return $kind.ToLowerInvariant() }
  }
}
$needs = $false
if ($ensure -eq 'absent') {
  $needs = Test-Path -LiteralPath $path
} elseif (-not (Test-Path -LiteralPath $path)) {
  if ($params.values) {
    $presentSpecs = @($params.values | Where-Object { -not $_.ensure -or $_.ensure -eq 'present' })
    $needs = $presentSpecs.Count -gt 0
  }
} elseif ($params.values) {
  $item = Get-Item -LiteralPath $path
  $props = Get-ItemProperty -LiteralPath $path
  foreach ($spec in $params.values) {
    $name = [string]$spec.name
    $ensureValue = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
    $prop = $props.PSObject.Properties[$name]
    if ($ensureValue -eq 'absent') {
      if ($null -ne $prop) { $needs = $true; break }
      continue
    }
    if ($null -eq $prop) { $needs = $true; break }
    $currentKind = Normalize-RegistryKind($item.GetValueKind($name).ToString())
    $desiredKind = [string]$spec.type
    if ($currentKind -ne $desiredKind) { $needs = $true; break }
    switch ($desiredKind) {
      'string'        { if ([string]$prop.Value -ne [string]$spec.data) { $needs = $true } }
      'expand_string' { if ([string]$prop.Value -ne [string]$spec.data) { $needs = $true } }
      'dword'         { if ([int64]$prop.Value -ne [int64]$spec.data) { $needs = $true } }
      'qword'         { if ([int64]$prop.Value -ne [int64]$spec.data) { $needs = $true } }
      'multi_string' {
        $current = @($prop.Value | ForEach-Object { [string]$_ })
        $desired = @($spec.data | ForEach-Object { [string]$_ })
        if ($current.Count -ne $desired.Count) { $needs = $true }
        else { for ($i = 0; $i -lt $current.Count; $i++) { if ($current[$i] -ne $desired[$i]) { $needs = $true; break } } }
      }
      'binary' {
        $current = @($prop.Value | ForEach-Object { [int]$_ })
        $desired = @($spec.data | ForEach-Object { [int]$_ })
        if ($current.Count -ne $desired.Count) { $needs = $true }
        else { for ($i = 0; $i -lt $current.Count; $i++) { if ($current[$i] -ne $desired[$i]) { $needs = $true; break } } }
      }
      default { throw "registry: unsupported type $desiredKind" }
    }
    if ($needs) { break }
  }
}
if (-not $needs) { Write-Output 'ok'; exit 0 }
if ($__pf_dry_run) { Write-Output 'would-change'; exit 0 }
if ($ensure -eq 'absent') {
  Remove-Item -LiteralPath $path -Recurse -Force -ErrorAction SilentlyContinue
} else {
  if (-not (Test-Path -LiteralPath $path)) {
    New-Item -Path $path -Force | Out-Null
  }
  if ($params.values) {
    foreach ($spec in $params.values) {
      $name = [string]$spec.name
      $ensureValue = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
      if ($ensureValue -eq 'absent') {
        Remove-ItemProperty -LiteralPath $path -Name $name -Force -ErrorAction SilentlyContinue
        continue
      }
      $kindMap = @{
        string = 'String'; expand_string = 'ExpandString'; dword = 'DWord'
        qword = 'QWord'; multi_string = 'MultiString'; binary = 'Binary'
      }
      $value = switch ([string]$spec.type) {
        'multi_string' { @($spec.data | ForEach-Object { [string]$_ }) }
        'binary'       { [byte[]]@($spec.data | ForEach-Object { [byte][int]$_ }) }
        'dword'        { [int]$spec.data }
        'qword'        { [int64]$spec.data }
        default        { $spec.data }
      }
      Remove-ItemProperty -LiteralPath $path -Name $name -Force -ErrorAction SilentlyContinue
      New-ItemProperty -LiteralPath $path -Name $name -Value $value -PropertyType $kindMap[[string]$spec.type] -Force | Out-Null
    }
  }
}
Write-Output 'changed'
`

const serviceCheckScript = `
$name = [string]$params.name
$desiredState = if ($params.state) { [string]$params.state } else { '' }
$desiredStartup = if ($params.startup_type) { [string]$params.startup_type } else { '' }
$filterName = $name.Replace("'", "''")
$service = Get-CimInstance Win32_Service -Filter ("Name='" + $filterName + "'")
if ($null -eq $service) {
  throw "service not found: $name"
}
$needs = $false
if ($desiredState -eq 'disabled') {
  if ($service.State -ne 'Stopped' -or $service.StartMode -ne 'Disabled') {
    $needs = $true
  }
} else {
  if ($desiredState -eq 'running' -and $service.State -ne 'Running') {
    $needs = $true
  }
  if ($desiredState -eq 'stopped' -and $service.State -ne 'Stopped') {
    $needs = $true
  }
  if ($desiredStartup) {
    $startupMap = @{ automatic = 'Auto'; manual = 'Manual'; disabled = 'Disabled' }
    if ($startupMap[$desiredStartup] -ne $service.StartMode) {
      $needs = $true
    }
  }
}
Write-Output $needs
`

const serviceApplyScript = `
$name = [string]$params.name
$desiredState = if ($params.state) { [string]$params.state } else { '' }
$desiredStartup = if ($params.startup_type) { [string]$params.startup_type } else { '' }
if ($desiredState -eq 'disabled') {
  Stop-Service -Name $name -Force -ErrorAction SilentlyContinue
  Set-Service -Name $name -StartupType Disabled
  exit 0
}
if ($desiredStartup) {
  $startupMap = @{ automatic = 'Automatic'; manual = 'Manual'; disabled = 'Disabled' }
  Set-Service -Name $name -StartupType $startupMap[$desiredStartup]
}
if ($desiredState -eq 'running') {
  Start-Service -Name $name
}
if ($desiredState -eq 'stopped') {
  Stop-Service -Name $name -Force
}
`

const packageCheckScript = `
$pkgs = @($params.packages)
$entries = Get-ItemProperty -Path @(
  'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*',
  'HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*',
  'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*'
) -ErrorAction SilentlyContinue
foreach ($spec in $pkgs) {
  $productId = [string]$spec.product_id
  $ensure = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
  $installed = $null -ne ($entries | Where-Object {
    $_.PSChildName -eq $productId -or $_.ProductID -eq $productId
  } | Select-Object -First 1)
  if ($ensure -eq 'absent' -and $installed) { Write-Output 'true'; exit 0 }
  if ($ensure -ne 'absent' -and -not $installed) { Write-Output 'true'; exit 0 }
}
Write-Output 'false'
`

const packageApplyScript = `
$pkgs = @($params.packages)
$entries = Get-ItemProperty -Path @(
  'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*',
  'HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*',
  'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*'
) -ErrorAction SilentlyContinue
foreach ($spec in $pkgs) {
  $productId = [string]$spec.product_id
  $ensure = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
  $installed = $null -ne ($entries | Where-Object {
    $_.PSChildName -eq $productId -or $_.ProductID -eq $productId
  } | Select-Object -First 1)
  if ($ensure -eq 'absent' -and -not $installed) { continue }
  if ($ensure -ne 'absent' -and $installed) { continue }
  $argsList = @()
  if ($spec.args) {
    foreach ($arg in $spec.args) { $argsList += [string]$arg }
  }
  if ($ensure -eq 'absent') {
    $process = Start-Process -FilePath 'msiexec.exe' -ArgumentList @('/x', $productId, '/qn', '/norestart') -Wait -PassThru
    if ($process.ExitCode -ne 0) {
      throw "package uninstall failed for '$productId' with exit code $($process.ExitCode)"
    }
  } else {
    $source = [string]$spec.source
    if ($source.ToLower().EndsWith('.msi')) {
      $process = Start-Process -FilePath 'msiexec.exe' -ArgumentList (@('/i', $source, '/qn', '/norestart') + $argsList) -Wait -PassThru
    } else {
      $process = Start-Process -FilePath $source -ArgumentList $argsList -Wait -PassThru
    }
    if ($process.ExitCode -ne 0) {
      throw "package install failed for '$productId' with exit code $($process.ExitCode)"
    }
  }
}
`

const shortcutCheckScript = `
$destination = [string]$params.destination
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Write-Output (Test-Path -LiteralPath $destination)
  exit 0
}
if (-not (Test-Path -LiteralPath $destination)) {
  Write-Output 'true'
  exit 0
}
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($destination)
$args = if ($params.args) { [string]$params.args } else { '' }
$icon = if ($params.icon) { [string]$params.icon } else { '' }
$needs = $shortcut.TargetPath -ne [string]$params.target -or $shortcut.Arguments -ne $args -or $shortcut.IconLocation -ne $icon
Write-Output $needs
`

const shortcutApplyScript = `
$destination = [string]$params.destination
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-Item -LiteralPath $destination -Force -ErrorAction SilentlyContinue
  exit 0
}
$parent = Split-Path -Parent $destination
if ($parent) {
  New-Item -ItemType Directory -Path $parent -Force | Out-Null
}
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($destination)
$shortcut.TargetPath = [string]$params.target
$shortcut.Arguments = if ($params.args) { [string]$params.args } else { '' }
$shortcut.IconLocation = if ($params.icon) { [string]$params.icon } else { '' }
$shortcut.Save()
`

const scheduledTaskCheckScript = `
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
Write-Output $needs
`

const scheduledTaskApplyScript = `
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
      $null = $parent.CreateFolder($segment)
    }
    $currentPath = $nextPath
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
$action = New-ScheduledTaskAction -Execute ([string]$params.execute) -Argument $arguments -WorkingDirectory $workingDir
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
    default { $principalArgs.LogonType = 'S4U' }
  }
  $principal = New-ScheduledTaskPrincipal @principalArgs
  Register-ScheduledTask -TaskPath $path -TaskName $name -Action $action -Trigger $trigger -Principal $principal -Force | Out-Null
} else {
  Register-ScheduledTask -TaskPath $path -TaskName $name -Action $action -Trigger $trigger -RunLevel $runLevelMap[[string]$params.run_level] -Force | Out-Null
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

const wingetPackageCheckScript = `
$pkgs = @($params.packages)
Get-Command winget.exe -ErrorAction Stop | Out-Null
$tempPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".json")
try {
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity') -Wait -PassThru -NoNewWindow
  if ($process.ExitCode -ne 0) {
    throw "winget export failed with exit code $($process.ExitCode)"
  }
  $doc = Get-Content -LiteralPath $tempPath -Raw | ConvertFrom-Json
} finally {
  Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
}
$installedMap = @{}
foreach ($src in @($doc.Sources)) {
  foreach ($pkg in @($src.Packages)) {
    $installedMap[$pkg.PackageIdentifier] = $pkg
  }
}
foreach ($spec in $pkgs) {
  $id = [string]$spec.id
  $ensure = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
  $version = if ($spec.version) { [string]$spec.version } else { '' }
  $match = $installedMap[$id]
  $isInstalled = $null -ne $match
  if ($ensure -eq 'absent') {
    if ($isInstalled) { Write-Output 'true'; exit 0 }
  } else {
    if (-not $isInstalled) { Write-Output 'true'; exit 0 }
    if ($version -and [string]$match.Version -ne $version) { Write-Output 'true'; exit 0 }
  }
}
Write-Output 'false'
`

const wingetPackageApplyScript = `
$pkgs = @($params.packages)
Get-Command winget.exe -ErrorAction Stop | Out-Null
$tempPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".json")
try {
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity') -Wait -PassThru -NoNewWindow
  if ($process.ExitCode -ne 0) {
    throw "winget export failed with exit code $($process.ExitCode)"
  }
  $doc = Get-Content -LiteralPath $tempPath -Raw | ConvertFrom-Json
} finally {
  Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
}
$installedMap = @{}
foreach ($src in @($doc.Sources)) {
  foreach ($pkg in @($src.Packages)) {
    $installedMap[$pkg.PackageIdentifier] = $pkg
  }
}
foreach ($spec in $pkgs) {
  $id = [string]$spec.id
  $ensure = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
  $version = if ($spec.version) { [string]$spec.version } else { '' }
  $source = if ($spec.source) { [string]$spec.source } else { '' }
  $scope = if ($spec.scope) { [string]$spec.scope } else { 'machine' }
  $match = $installedMap[$id]
  $isInstalled = $null -ne $match
  if ($ensure -eq 'absent' -and -not $isInstalled) { continue }
  if ($ensure -ne 'absent' -and $isInstalled -and (-not $version -or [string]$match.Version -eq $version)) { continue }
  $args = @()
  if ($ensure -eq 'absent') {
    $args = @('uninstall', '--id', $id, '--exact', '--disable-interactivity', '--accept-source-agreements')
  } else {
    $args = @('install', '--id', $id, '--exact', '--silent', '--disable-interactivity', '--accept-package-agreements', '--accept-source-agreements', '--scope', $scope)
  }
  if ($version) { $args += @('--version', $version) }
  if ($source) { $args += @('--source', $source) }
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList $args -Wait -PassThru -NoNewWindow
  if ($process.ExitCode -ne 0) {
    throw "winget command failed for '$id' with exit code $($process.ExitCode)"
  }
}
`

// appxHelperFunctions is shared preamble for all remove_appx_packages scripts.
// Get-AppxProvisionedPackage -Online is a slow DISM call; $allProvisioned caches
// it once per script invocation rather than once per package.
const appxHelperFunctions = `
$pkgs = @($params.packages)
$needsProvisioned = @($pkgs | Where-Object { -not $_.scope -or [string]$_.scope -eq 'both' -or [string]$_.scope -eq 'provisioned' })
$allProvisioned = if ($needsProvisioned.Count -gt 0) {
  @(Get-AppxProvisionedPackage -Online -ErrorAction SilentlyContinue)
} else { @() }

function Get-InstalledAppxMatches([string]$scope, [string]$name) {
  $installed = @()
  switch ($scope) {
    'current_user' { $installed = @(Get-AppxPackage -Name $name -ErrorAction SilentlyContinue) }
    'all_users'    { $installed = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
    'provisioned'  { $installed = @() }
    'both'         { $installed = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
    default { throw "remove_appx_packages: unsupported scope $scope" }
  }
  return @($installed | Where-Object { $null -ne $_ -and -not [string]::IsNullOrWhiteSpace([string]$_.PackageFullName) })
}

function Get-ProvisionedAppxMatches([string]$scope, [string]$name, [bool]$hasWildcard) {
  if ($scope -ne 'provisioned' -and $scope -ne 'both') { return @() }
  return @($allProvisioned | Where-Object {
    $displayName = [string]$_.DisplayName
    $packageName = [string]$_.PackageName
    -not [string]::IsNullOrWhiteSpace($packageName) -and (
      ($hasWildcard -and $displayName -like $name) -or
      (-not $hasWildcard -and $displayName -eq $name)
    )
  })
}
`

const removeAppxPackagesCheckScript = appxHelperFunctions + `
foreach ($spec in $pkgs) {
  $name = [string]$spec.name
  $scope = if ($spec.scope) { [string]$spec.scope } else { 'both' }
  $hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
  Write-Output ("checking appx package " + $name + " (" + $scope + ")")
  $installed = Get-InstalledAppxMatches $scope $name
  $provisioned = Get-ProvisionedAppxMatches $scope $name $hasWildcard
  if (($installed.Count + $provisioned.Count) -gt 0) { Write-Output 'true'; exit 0 }
}
Write-Output 'false'
`

const removeAppxPackagesApplyScript = appxHelperFunctions + `
foreach ($spec in $pkgs) {
  $name = [string]$spec.name
  $scope = if ($spec.scope) { [string]$spec.scope } else { 'both' }
  $hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
  Write-Output ("processing appx package " + $name + " (" + $scope + ")")
  if ($scope -ne 'provisioned') {
    foreach ($pkg in (Get-InstalledAppxMatches $scope $name)) {
      if ($null -eq $pkg) { continue }
      $packageFullName = [string]$pkg.PackageFullName
      if ([string]::IsNullOrWhiteSpace($packageFullName)) {
        Write-Output ("skipping appx package " + $name + " because PackageFullName is empty")
        continue
      }
      if ($scope -eq 'current_user') {
        Remove-AppxPackage -Package $packageFullName -ErrorAction SilentlyContinue
      } else {
        try {
          Remove-AppxPackage -Package $packageFullName -AllUsers -ErrorAction Stop
        } catch {
          Remove-AppxPackage -Package $packageFullName -ErrorAction SilentlyContinue
        }
      }
    }
  }
  if ($scope -eq 'provisioned' -or $scope -eq 'both') {
    @(Get-ProvisionedAppxMatches $scope $name $hasWildcard) | ForEach-Object {
      Remove-AppxProvisionedPackage -Online -PackageName $_.PackageName -ErrorAction SilentlyContinue | Out-Null
    }
  }
}
`

// removeAppxPackagesEnsureScript combines check and apply in one invocation,
// calling Get-AppxProvisionedPackage -Online exactly once regardless of outcome.
// Outputs "ok", "would-change" (dry-run), or "changed". $__pf_dry_run must be
// set before $params by the caller.
const removeAppxPackagesEnsureScript = appxHelperFunctions + `
$needs = $false
foreach ($spec in $pkgs) {
  $name = [string]$spec.name
  $scope = if ($spec.scope) { [string]$spec.scope } else { 'both' }
  $hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
  if ((Get-InstalledAppxMatches $scope $name).Count -gt 0) { $needs = $true; break }
  if ((Get-ProvisionedAppxMatches $scope $name $hasWildcard).Count -gt 0) { $needs = $true; break }
}
if (-not $needs) { Write-Output 'ok'; exit 0 }
if ($__pf_dry_run) { Write-Output 'would-change'; exit 0 }
foreach ($spec in $pkgs) {
  $name = [string]$spec.name
  $scope = if ($spec.scope) { [string]$spec.scope } else { 'both' }
  $hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
  if ($scope -ne 'provisioned') {
    foreach ($pkg in (Get-InstalledAppxMatches $scope $name)) {
      if ($null -eq $pkg) { continue }
      $packageFullName = [string]$pkg.PackageFullName
      if ([string]::IsNullOrWhiteSpace($packageFullName)) { continue }
      if ($scope -eq 'current_user') {
        Remove-AppxPackage -Package $packageFullName -ErrorAction SilentlyContinue
      } else {
        try {
          Remove-AppxPackage -Package $packageFullName -AllUsers -ErrorAction Stop
        } catch {
          Remove-AppxPackage -Package $packageFullName -ErrorAction SilentlyContinue
        }
      }
    }
  }
  if ($scope -eq 'provisioned' -or $scope -eq 'both') {
    @(Get-ProvisionedAppxMatches $scope $name $hasWildcard) | ForEach-Object {
      Remove-AppxProvisionedPackage -Online -PackageName $_.PackageName -ErrorAction SilentlyContinue | Out-Null
    }
  }
}
Write-Output 'changed'
`

const powerPlanCheckScript = `
function Invoke-PowerCfg([string[]]$Arguments) {
  $output = & powercfg.exe @Arguments 2>&1
  if ($LASTEXITCODE -ne 0) {
    throw ($output -join [Environment]::NewLine)
  }
  return $output
}
function Get-Schemes() {
  $schemes = @()
  foreach ($line in Invoke-PowerCfg @('/list')) {
    if ($line -match 'Power Scheme GUID:\s*([A-Fa-f0-9-]{36})\s+\((.+?)\)\s*(\*)?') {
      $schemes += [pscustomobject]@{
        Guid = $matches[1]
        Name = $matches[2]
        Active = ($matches[3] -eq '*')
      }
    }
  }
  return $schemes
}
function Get-SettingValues([string]$SchemeGuid, [string]$Subgroup, [string]$Setting) {
  $ac = $null; $dc = $null
  foreach ($line in Invoke-PowerCfg @('/query', $SchemeGuid, $Subgroup, $Setting)) {
    if ($line -match 'Current AC Power Setting Index:\s*0x([0-9A-Fa-f]+)') { $ac = [Convert]::ToInt64($matches[1], 16) }
    elseif ($line -match 'Current DC Power Setting Index:\s*0x([0-9A-Fa-f]+)') { $dc = [Convert]::ToInt64($matches[1], 16) }
  }
  return @{ AC = $ac; DC = $dc }
}
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$activate = if ($null -ne $params.activate) { [bool]$params.activate } else { $true }
$scheme = Get-Schemes | Where-Object { $_.Name -eq $name } | Select-Object -First 1
if ($ensure -eq 'absent') {
  Write-Output ($null -ne $scheme)
  exit 0
}
if ($null -eq $scheme) {
  Write-Output 'true'
  exit 0
}
$needs = $false
if ($activate -and -not $scheme.Active) {
  $needs = $true
}
if ($params.settings) {
  foreach ($setting in $params.settings) {
    $vals = Get-SettingValues $scheme.Guid ([string]$setting.subgroup) ([string]$setting.setting)
    if ($null -ne $setting.ac_value -and $vals.AC -ne [int64]$setting.ac_value) { $needs = $true; break }
    if ($null -ne $setting.dc_value -and $vals.DC -ne [int64]$setting.dc_value) { $needs = $true; break }
  }
}
Write-Output $needs
`

const powerPlanApplyScript = `
function Invoke-PowerCfg([string[]]$Arguments) {
  $output = & powercfg.exe @Arguments 2>&1
  if ($LASTEXITCODE -ne 0) {
    throw ($output -join [Environment]::NewLine)
  }
  return $output
}
function Resolve-BaseScheme([string]$Value) {
  switch ($Value.ToLowerInvariant()) {
    'balanced' { return 'SCHEME_BALANCED' }
    'high_performance' { return 'SCHEME_MIN' }
    'high-performance' { return 'SCHEME_MIN' }
    'power_saver' { return 'SCHEME_MAX' }
    'power-saver' { return 'SCHEME_MAX' }
    default { return $Value }
  }
}
function Get-Schemes() {
  $schemes = @()
  foreach ($line in Invoke-PowerCfg @('/list')) {
    if ($line -match 'Power Scheme GUID:\s*([A-Fa-f0-9-]{36})\s+\((.+?)\)\s*(\*)?') {
      $schemes += [pscustomobject]@{
        Guid = $matches[1]
        Name = $matches[2]
        Active = ($matches[3] -eq '*')
      }
    }
  }
  return $schemes
}
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$activate = if ($null -ne $params.activate) { [bool]$params.activate } else { $true }
$base = if ($params.base) { Resolve-BaseScheme([string]$params.base) } else { 'SCHEME_BALANCED' }
$scheme = Get-Schemes | Where-Object { $_.Name -eq $name } | Select-Object -First 1
if ($ensure -eq 'absent') {
  if ($null -ne $scheme) {
    if ($scheme.Active) {
      Invoke-PowerCfg @('/setactive', 'SCHEME_BALANCED') | Out-Null
    }
    Invoke-PowerCfg @('/delete', $scheme.Guid) | Out-Null
  }
  exit 0
}
if ($null -eq $scheme) {
  $output = Invoke-PowerCfg @('/duplicatescheme', $base)
  $newGuid = ''
  foreach ($line in @($output)) {
    if ($line -match '([A-Fa-f0-9-]{36})') {
      $newGuid = $matches[1]
      break
    }
  }
  if (-not $newGuid) {
    throw "power_plan: unable to determine duplicated scheme GUID"
  }
  Invoke-PowerCfg @('/changename', $newGuid, $name) | Out-Null
  $scheme = Get-Schemes | Where-Object { $_.Guid -eq $newGuid } | Select-Object -First 1
}
if ($null -eq $scheme) {
  throw "power_plan: unable to resolve scheme $name"
}
Invoke-PowerCfg @('/changename', $scheme.Guid, $name) | Out-Null
if ($params.settings) {
  foreach ($setting in $params.settings) {
    if ($null -ne $setting.ac_value) {
      Invoke-PowerCfg @('/setacvalueindex', $scheme.Guid, [string]$setting.subgroup, [string]$setting.setting, [string]$setting.ac_value) | Out-Null
    }
    if ($null -ne $setting.dc_value) {
      Invoke-PowerCfg @('/setdcvalueindex', $scheme.Guid, [string]$setting.subgroup, [string]$setting.setting, [string]$setting.dc_value) | Out-Null
    }
  }
}
if ($activate) {
  Invoke-PowerCfg @('/setactive', $scheme.Guid) | Out-Null
}
`

const userCheckScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$user = Get-LocalUser -Name $name -ErrorAction SilentlyContinue
if ($ensure -eq 'absent') {
  Write-Output ($null -ne $user)
  exit 0
}
if ($null -eq $user) {
  Write-Output 'true'
  exit 0
}
$needs = $false
if ($params.password) {
  $needs = $true
}
if ($params.groups) {
  foreach ($group in $params.groups) {
    $members = Get-LocalGroupMember -Group ([string]$group) -ErrorAction SilentlyContinue
    if (-not ($members | Where-Object { $_.Name -match ("(^|\\\\)" + [regex]::Escape($name) + "$") })) {
      $needs = $true
      break
    }
  }
}
Write-Output $needs
`

const userApplyScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-LocalUser -Name $name -ErrorAction SilentlyContinue
  exit 0
}
$passwordValue = if ($params.password) { [string]$params.password } else { '' }
$securePassword = $null
if ($passwordValue) {
  $securePassword = ConvertTo-SecureString $passwordValue -AsPlainText -Force
}
$user = Get-LocalUser -Name $name -ErrorAction SilentlyContinue
if ($null -eq $user) {
  if ($securePassword) {
    New-LocalUser -Name $name -Password $securePassword | Out-Null
  } else {
    New-LocalUser -Name $name -NoPassword | Out-Null
  }
} elseif ($securePassword) {
  $user | Set-LocalUser -Password $securePassword
}
if ($params.groups) {
  foreach ($group in $params.groups) {
    Add-LocalGroupMember -Group ([string]$group) -Member $name -ErrorAction SilentlyContinue
  }
}
`

const windowsFeatureCheckScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$feature = Get-WindowsOptionalFeature -Online -FeatureName $name -ErrorAction SilentlyContinue
if (-not $feature) { throw "windows feature not found: $name" }
if ($ensure -eq 'absent') {
  Write-Output ([bool]($feature.State -ne 'Disabled'))
  exit 0
}
Write-Output ([bool]($feature.State -ne 'Enabled'))
`

const windowsFeatureApplyScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Disable-WindowsOptionalFeature -Online -FeatureName $name -NoRestart | Out-Null
  exit 0
}
Enable-WindowsOptionalFeature -Online -FeatureName $name -NoRestart | Out-Null
`

const firewallRuleCheckScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$rule = Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue | Select-Object -First 1
if ($ensure -eq 'absent') {
  Write-Output ($null -ne $rule)
  exit 0
}
if ($null -eq $rule) {
  Write-Output 'true'
  exit 0
}
$directionMap = @{ inbound = 'Inbound'; outbound = 'Outbound' }
$actionMap = @{ allow = 'Allow'; block = 'Block' }
$protocolMap = @{ tcp = 'TCP'; udp = 'UDP'; any = 'Any' }
$portFilter = $rule | Get-NetFirewallPortFilter
$needs = $rule.Direction -ne $directionMap[[string]$params.direction] -or $rule.Action -ne $actionMap[[string]$params.action]
if ([string]$params.protocol) {
  $needs = $needs -or $portFilter.Protocol -ne $protocolMap[[string]$params.protocol]
}
if ([string]$params.ports) {
  $needs = $needs -or [string]$portFilter.LocalPort -ne [string]$params.ports
}
Write-Output $needs
`

const firewallRuleApplyScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue
  exit 0
}
$directionMap = @{ inbound = 'Inbound'; outbound = 'Outbound' }
$actionMap = @{ allow = 'Allow'; block = 'Block' }
$protocolMap = @{ tcp = 'TCP'; udp = 'UDP'; any = 'Any' }
$existing = Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue | Select-Object -First 1
if ($null -eq $existing) {
  $newParams = @{
    DisplayName = $name
    Direction = $directionMap[[string]$params.direction]
    Action = $actionMap[[string]$params.action]
    Protocol = $protocolMap[[string]$params.protocol]
  }
  if ([string]$params.ports) {
    $newParams['LocalPort'] = [string]$params.ports
  }
  New-NetFirewallRule @newParams | Out-Null
  exit 0
}
Set-NetFirewallRule -DisplayName $name -Direction $directionMap[[string]$params.direction] -Action $actionMap[[string]$params.action] | Out-Null
if ([string]$params.protocol -or [string]$params.ports) {
  Set-NetFirewallRule -DisplayName $name -Protocol $protocolMap[[string]$params.protocol] -LocalPort ([string]$params.ports) | Out-Null
}
`
