package pscript

// Package-manager modules (winget_package, choco_package, scoop_package) share one diff-and-act
// skeleton: setup + adapter + check or apply tail. Each adapter defines:
//
//	Get-PmInstalled                          returns manager state, read once
//	Test-PmPresent  -Spec $s -Installed $i   true when the entry is installed as desired
//	Invoke-PmChange -Spec $s -Ensure $e -Installed $i
//	                                         installs or removes one entry; throws on failure
//
// Apply calls Get-PmInstalled again after making changes to verify them.
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

// managerPackageApplyTail re-reads state after changing anything, because
// package managers can report success without converging.
const managerPackageApplyTail = `
$installed = Get-PmInstalled
$changed = @()
foreach ($spec in $pkgs) {
  if (-not (Test-PmNeedsChange -Spec $spec -Installed $installed)) { continue }
  Invoke-PmChange -Spec $spec -Ensure (Get-PmEnsure $spec) -Installed $installed
  $changed += $spec
  # Later entries and tasks should see binaries the change put on PATH.
  $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  $env:Path = (@($machinePath, $userPath) | Where-Object { -not [string]::IsNullOrEmpty($_) }) -join ';'
}
if ($changed.Count -gt 0) {
  $installed = Get-PmInstalled
  $stuck = @(foreach ($spec in $changed) {
    if (Test-PmNeedsChange -Spec $spec -Installed $installed) { "$($spec.id) ($(Get-PmEnsure $spec))" }
  })
  if ($stuck.Count -gt 0) {
    throw ("the package manager reported success, but these packages are not in the desired state: " + ($stuck -join ', '))
  }
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

// chocoAdapter reads installed packages from choco list. Chocolatey keeps one
// version per package, so a pinned change goes through upgrade
// --allow-downgrade, which also installs a missing package. choco.exe is
// resolved from the machine environment because a session opened before
// Chocolatey was installed has a stale PATH and ChocolateyInstall.
const chocoAdapter = `
$chocoRoot = [Environment]::GetEnvironmentVariable('ChocolateyInstall', 'Machine')
if (-not $chocoRoot) { $chocoRoot = Join-Path $env:ProgramData 'chocolatey' }
$chocoExe = Join-Path $chocoRoot 'bin\choco.exe'
if (-not (Test-Path -LiteralPath $chocoExe)) {
  throw "choco.exe was not found at $chocoExe. Add a task with 'uses: preflight/chocolatey' before this one."
}
# Chocolatey 1.x choco list searches the remote source, which would make every
# package look installed, so an unreadable version fails closed too.
$chocoVersion = (& $chocoExe --version 2>&1 | Out-String).Trim()
if ($LASTEXITCODE -ne 0 -or $chocoVersion -notmatch '^(\d+)\.') {
  throw "could not read the Chocolatey version from $chocoExe (got '$chocoVersion'); choco_package requires Chocolatey 2.0 or later."
}
if ([int]$Matches[1] -lt 2) {
  throw "choco_package requires Chocolatey 2.0 or later, found $chocoVersion."
}
# Exit codes Chocolatey treats as success; 1641 and 3010 also mean a reboot is pending.
$chocoSuccessCodes = @(0, 1605, 1614, 1641, 3010)
function Invoke-Choco {
  param(
    [string[]]$Arguments
  )
  $output = & $chocoExe @Arguments 2>&1
  return [pscustomobject]@{
    ExitCode = $LASTEXITCODE
    Text = ($output | Out-String).Trim()
  }
}
# ConvertTo-ChocoVersion makes 1.6, 1.6.0 and 01.6.0.0 compare equal.
function ConvertTo-ChocoVersion {
  param([string]$Version)
  $numeric, $suffix = $Version -split '-', 2
  $parts = [System.Collections.Generic.List[string]]@(foreach ($part in $numeric.Split('.')) {
    $trimmed = $part.TrimStart('0')
    if ($trimmed) { $trimmed } else { '0' }
  })
  while ($parts.Count -gt 1 -and $parts[$parts.Count - 1] -eq '0') { $parts.RemoveAt($parts.Count - 1) }
  $normalized = $parts -join '.'
  if ($suffix) { $normalized += '-' + $suffix }
  return $normalized.ToLowerInvariant()
}
function Get-PmInstalled {
  $result = Invoke-Choco -Arguments @('list', '--limit-output')
  if ($result.ExitCode -ne 0) {
    throw ("choco list failed with exit code $($result.ExitCode)" + [Environment]::NewLine + $result.Text)
  }
  $installedMap = @{}
  foreach ($line in ($result.Text -split '\r?\n')) {
    $id, $version = $line.Split('|')
    if ($id -and $version) { $installedMap[$id.ToLowerInvariant()] = $version.Trim() }
  }
  return $installedMap
}
function Test-PmPresent {
  param($Spec, $Installed)
  $current = $Installed[([string]$Spec.id).ToLowerInvariant()]
  if ($null -eq $current) { return $false }
  if ($Spec.version) { return (ConvertTo-ChocoVersion $current) -eq (ConvertTo-ChocoVersion ([string]$Spec.version)) }
  return $true
}
function Invoke-PmChange {
  param($Spec, [string]$Ensure, $Installed)
  $id = [string]$Spec.id
  if ($Ensure -eq 'absent') {
    $chocoArgs = @('uninstall', $id, '-y', '--no-progress')
  } else {
    if ($Spec.version) {
      $chocoArgs = @('upgrade', $id, '--version', [string]$Spec.version, '--allow-downgrade', '-y', '--no-progress')
    } else {
      $chocoArgs = @('install', $id, '-y', '--no-progress')
    }
    if ($Spec.source) { $chocoArgs += @('--source', [string]$Spec.source) }
    if ($Spec.params) { $chocoArgs += ('--params=' + [string]$Spec.params) }
  }
  foreach ($arg in @($Spec.args)) { if ($null -ne $arg) { $chocoArgs += [string]$arg } }
  $result = Invoke-Choco -Arguments $chocoArgs
  if ($chocoSuccessCodes -notcontains $result.ExitCode) {
    throw ("choco $($chocoArgs[0]) failed for '$id' with exit code $($result.ExitCode)" + [Environment]::NewLine + $result.Text)
  }
  if ($result.ExitCode -eq 1641 -or $result.ExitCode -eq 3010) {
    Write-Output "choco $($chocoArgs[0]) $id succeeded; a reboot is required to finish it (exit code $($result.ExitCode))"
  }
}
`

// scoopAdapter manages Scoop apps for the transport account. State comes from
// the apps directory, because Scoop exits 0 for unknown apps and its export
// lists only the current version. Each app keeps version directories plus a
// current junction; specs arrive with active set and ordered by
// NormalizeScoopPackages. Invoke-PmChange updates $Installed so a later
// active entry sees which version an earlier install made current.
const scoopAdapter = `
$scoopDir = [Environment]::GetEnvironmentVariable('SCOOP', 'User')
if (-not $scoopDir) { $scoopDir = Join-Path $env:USERPROFILE 'scoop' }
$scoopCmd = Join-Path $scoopDir 'shims\scoop.cmd'
if (-not (Test-Path -LiteralPath $scoopCmd)) {
  throw "Scoop was not found for $env:USERNAME at $scoopDir. Add a task with 'uses: preflight/scoop' before this one."
}
function Invoke-Scoop {
  param(
    [string[]]$Arguments
  )
  # scoop.cmd, not scoop.ps1, which the execution policy may block.
  $output = & $scoopCmd @Arguments 2>&1
  $text = ($output | Out-String).Trim()
  if ($LASTEXITCODE -ne 0 -or $text -match "(?m)^ERROR|Couldn't find manifest") {
    throw ("scoop $($Arguments -join ' ') failed with exit code $LASTEXITCODE" + [Environment]::NewLine + $text)
  }
}
function Get-ScoopApp {
  param($Spec)
  return (([string]$Spec.id) -split '/')[-1].ToLowerInvariant()
}
function Get-PmInstalled {
  $installedMap = @{}
  $appsDir = Join-Path $scoopDir 'apps'
  if (-not (Test-Path -LiteralPath $appsDir)) { return $installedMap }
  foreach ($app in @(Get-ChildItem -LiteralPath $appsDir -Directory)) {
    $versions = [System.Collections.Generic.List[string]]::new()
    foreach ($dir in @(Get-ChildItem -LiteralPath $app.FullName -Directory)) {
      if ($dir.Name -eq 'current' -or $dir.Name.StartsWith('_')) { continue }
      # A version is installed once Scoop wrote its install record.
      if ((Test-Path -LiteralPath (Join-Path $dir.FullName 'scoop-install.json')) -or (Test-Path -LiteralPath (Join-Path $dir.FullName 'install.json'))) {
        $versions.Add($dir.Name.ToLowerInvariant())
      }
    }
    if ($versions.Count -eq 0) { continue }
    $current = $null
    $link = Get-Item -LiteralPath (Join-Path $app.FullName 'current') -ErrorAction SilentlyContinue
    if ($link -and $link.Target) { $current = (Split-Path -Leaf @($link.Target)[0]).ToLowerInvariant() }
    $installedMap[$app.Name.ToLowerInvariant()] = [pscustomobject]@{ Versions = $versions; Current = $current }
  }
  return $installedMap
}
function Test-PmPresent {
  param($Spec, $Installed)
  $state = $Installed[(Get-ScoopApp $Spec)]
  if ($null -eq $state) { return $false }
  if (-not $Spec.version) { return $true }
  $version = ([string]$Spec.version).ToLowerInvariant()
  if (-not $state.Versions.Contains($version)) { return $false }
  if ($Spec.active) { return $state.Current -eq $version }
  return $true
}
function Invoke-PmChange {
  param($Spec, [string]$Ensure, $Installed)
  $app = Get-ScoopApp $Spec
  $extra = @(@($Spec.args) | Where-Object { $null -ne $_ } | ForEach-Object { [string]$_ })
  if ($Ensure -eq 'absent') {
    Invoke-Scoop -Arguments (@('uninstall', $app) + $extra)
    $Installed.Remove($app)
    return
  }
  if (-not $Spec.version) {
    Invoke-Scoop -Arguments (@('install', [string]$Spec.id) + $extra)
    return
  }
  $version = ([string]$Spec.version).ToLowerInvariant()
  $state = $Installed[$app]
  $installedNow = $null -eq $state -or -not $state.Versions.Contains($version)
  if ($installedNow) {
    # Scoop installs a pinned version beside the ones already installed.
    Invoke-Scoop -Arguments (@('install', "$($Spec.id)@$($Spec.version)") + $extra)
  }
  # An install makes its version current, so only an already installed version needs a reset.
  if ($Spec.active -and -not $installedNow -and $state.Current -ne $version) {
    Invoke-Scoop -Arguments @('reset', "$app@$($Spec.version)")
  }
  if ($null -eq $state) {
    $state = [pscustomobject]@{ Versions = [System.Collections.Generic.List[string]]::new(); Current = $null }
    $Installed[$app] = $state
  }
  if ($installedNow) { $state.Versions.Add($version) }
  if ($installedNow -or $Spec.active) { $state.Current = $version }
}
`

const (
	WingetPackageCheckScript = managerPackageSetup + wingetAdapter + managerPackageCheckTail
	WingetPackageApplyScript = managerPackageSetup + wingetAdapter + managerPackageApplyTail
	ChocoPackageCheckScript  = managerPackageSetup + chocoAdapter + managerPackageCheckTail
	ChocoPackageApplyScript  = managerPackageSetup + chocoAdapter + managerPackageApplyTail
	ScoopPackageCheckScript  = managerPackageSetup + scoopAdapter + managerPackageCheckTail
	ScoopPackageApplyScript  = managerPackageSetup + scoopAdapter + managerPackageApplyTail
)
