package pscript

// Package-manager modules (winget_package, ...) share one diff-and-act
// skeleton: setup + adapter + check or apply tail. Each adapter defines:
//
//	Get-PmInstalled                          returns manager state, read once
//	Test-PmPresent  -Spec $s -Installed $i   true when the entry is installed as desired
//	Invoke-PmChange -Spec $s -Ensure $e -Installed $i
//	                                         installs or removes one entry; throws on failure
//
// Adapters own version-comparison semantics: winget compares the exact
// version string, while choco normalizes versions before comparing. The
// engine only asks whether an entry is present as desired.
//
// Specs arrive as $params.packages after Go-side normalization.

const managerPackageSetup = `
$pkgs = @($params.packages)
function Get-PmEnsure {
  param($Spec)
  if ($Spec.ensure) { return [string]$Spec.ensure }
  return 'present'
}
function Test-PmNeedsChange {
  param($Spec, $Installed)
  $present = Test-PmPresent -Spec $Spec -Installed $Installed
  return ($present -eq ((Get-PmEnsure $Spec) -eq 'absent'))
}
`

const managerPackageCheckTail = `
$installed = Get-PmInstalled
foreach ($spec in $pkgs) {
  if (Test-PmNeedsChange -Spec $spec -Installed $installed) { Write-Output 'true'; exit 0 }
}
Write-Output 'false'
`

const managerPackageApplyTail = `
$installed = Get-PmInstalled
foreach ($spec in $pkgs) {
  if (-not (Test-PmNeedsChange -Spec $spec -Installed $installed)) { continue }
  Invoke-PmChange -Spec $spec -Ensure (Get-PmEnsure $spec) -Installed $installed
  # Later entries and tasks should see binaries the change put on PATH.
  $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  $env:Path = (@($machinePath, $userPath) | Where-Object { -not [string]::IsNullOrEmpty($_) }) -join ';'
}
`

// wingetAdapter reads installed packages from winget export. Without a
// registered winget source, export exits 0 with no Sources, which would make
// every package look absent.
const wingetAdapter = `
$wingetUser = "$([Environment]::UserDomainName)\$([Environment]::UserName)"
$wingetExe = (Get-Command winget.exe -ErrorAction SilentlyContinue).Source
if (-not $wingetExe) {
  # Remote sessions may lack the per-user WindowsApps alias directory on PATH.
  if ($env:LOCALAPPDATA) {
    $alias = Join-Path $env:LOCALAPPDATA 'Microsoft\WindowsApps\winget.exe'
    if (Test-Path -LiteralPath $alias) { $wingetExe = $alias }
  }
}
if (-not $wingetExe) {
  throw "winget.exe was not found for $wingetUser. Add a task with 'uses: preflight/winget' before this one."
}
function Invoke-Winget {
  param(
    [string[]]$Arguments
  )
  $output = & $wingetExe @Arguments 2>&1
  $exitCode = $LASTEXITCODE
  $details = @()
  if ($output) {
    $text = ($output | Out-String).Trim()
    if (-not [string]::IsNullOrWhiteSpace($text)) { $details += $text }
  }
  return [pscustomobject]@{
    ExitCode = $exitCode
    Details = [string[]]$details
  }
}
function Test-WingetPackageListedCurrent {
  param(
    [string]$Id,
    [string]$Source
  )
  $listArgs = @('list', '--id', $Id, '--exact', '--accept-source-agreements', '--disable-interactivity')
  if ($Source) { $listArgs += @('--source', $Source) }
  $result = Invoke-Winget -Arguments $listArgs
  if ($result.ExitCode -ne 0) { return $false }
  $stdout = [string]::Join([Environment]::NewLine, $result.Details)
  if ([string]::IsNullOrWhiteSpace($stdout)) { return $false }
  if ($stdout -notmatch [regex]::Escape($Id)) { return $false }
  # If WinGet prints an Available column, install would attempt an upgrade.
  # Treat that as needing change so check/apply stay aligned.
  return ($stdout -notmatch '(?m)^\s*Name\s+Id\s+Version\s+Available\s+Source\s*$')
}
function Get-PmInstalled {
  $tempPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".json")
  $doc = $null
  try {
    $result = Invoke-Winget -Arguments @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity')
    if ($result.ExitCode -ne 0) {
      if ($result.Details.Count -gt 0) {
        throw ("winget export failed with exit code $($result.ExitCode)" + [Environment]::NewLine + [string]::Join([Environment]::NewLine, $result.Details))
      }
      throw "winget export failed with exit code $($result.ExitCode)"
    }
    if (Test-Path -LiteralPath $tempPath) {
      $doc = Get-Content -LiteralPath $tempPath -Raw | ConvertFrom-Json
    }
  } finally {
    Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
  }
  if ($null -eq $doc.Sources -and $null -eq (Get-AppxPackage -Name Microsoft.Winget.Source)) {
    throw "the winget source is not registered for $wingetUser, so installed packages can't be read. Add a task with 'uses: preflight/winget' before this one."
  }
  $installedMap = @{}
  foreach ($src in @($doc.Sources | Where-Object { $null -ne $_ })) {
    foreach ($pkg in @($src.Packages | Where-Object { $_.PackageIdentifier })) {
      $installedMap[[string]$pkg.PackageIdentifier] = $pkg
    }
  }
  return $installedMap
}
function Test-PmPresent {
  param($Spec, $Installed)
  $id = [string]$Spec.id
  $version = if ($Spec.version) { [string]$Spec.version } else { '' }
  $source = if ($Spec.source) { [string]$Spec.source } else { '' }
  $match = $Installed[$id]
  if ($null -eq $match) {
    if (-not $version) { return (Test-WingetPackageListedCurrent -Id $id -Source $source) }
    return $false
  }
  if ($version) { return [string]$match.Version -eq $version }
  return $true
}
function Invoke-PmChange {
  param($Spec, [string]$Ensure, $Installed)
  $id = [string]$Spec.id
  $version = if ($Spec.version) { [string]$Spec.version } else { '' }
  $source = if ($Spec.source) { [string]$Spec.source } else { '' }
  $scope = if ($Spec.scope) { [string]$Spec.scope } else { 'machine' }
  if ($Ensure -eq 'absent') {
    $wingetArgs = @('uninstall', '--id', $id, '--exact', '--disable-interactivity', '--accept-source-agreements')
  } else {
    $wingetArgs = @('install', '--id', $id, '--exact', '--silent', '--disable-interactivity', '--accept-package-agreements', '--accept-source-agreements', '--scope', $scope)
  }
  if ($version) { $wingetArgs += @('--version', $version) }
  if ($source) { $wingetArgs += @('--source', $source) }
  foreach ($arg in @($Spec.args)) { if ($null -ne $arg) { $wingetArgs += [string]$arg } }
  $result = Invoke-Winget -Arguments $wingetArgs
  if ($result.ExitCode -eq 0) { return }
  $combinedDetails = [string]::Join([Environment]::NewLine, $result.Details)
  # WinGet can turn "install an already-present package" into an update path
  # and return UPDATE_NOT_APPLICABLE even when the unversioned desired state is satisfied.
  if ($Ensure -ne 'absent' -and $result.ExitCode -eq -1978335189) {
    if ((Test-PmPresent -Spec $Spec -Installed $Installed) -or ($combinedDetails -match 'No available upgrade found' -and $combinedDetails -match 'No newer package versions are available')) {
      return
    }
  }
  if ($result.Details.Count -gt 0) {
    throw ("winget command failed for '$id' with exit code $($result.ExitCode)" + [Environment]::NewLine + $combinedDetails)
  }
  throw "winget command failed for '$id' with exit code $($result.ExitCode)"
}
`

const (
	WingetPackageCheckScript = managerPackageSetup + wingetAdapter + managerPackageCheckTail
	WingetPackageApplyScript = managerPackageSetup + wingetAdapter + managerPackageApplyTail
)
