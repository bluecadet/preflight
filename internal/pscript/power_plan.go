package pscript

const PowerPlanCheckScript = `
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

const PowerPlanApplyScript = `
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
