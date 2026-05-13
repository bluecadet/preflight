package pscript

const PackageCheckScript = `
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

const PackageApplyScript = `
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
$machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$env:Path = (@($machinePath, $userPath) | Where-Object { -not [string]::IsNullOrEmpty($_) }) -join ';'
`

const wingetPackagePresenceScript = `
function Invoke-Winget {
  param(
    [string[]]$Arguments
  )
  $output = & winget.exe @Arguments 2>&1
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
function Test-WingetDesiredPresent {
  param(
    $Spec,
    $InstalledMap
  )
  $id = [string]$Spec.id
  $version = if ($Spec.version) { [string]$Spec.version } else { '' }
  $source = if ($Spec.source) { [string]$Spec.source } else { '' }
  $match = $InstalledMap[$id]
  if ($null -eq $match) {
    if (-not $version) { return (Test-WingetPackageListedCurrent -Id $id -Source $source) }
    return $false
  }
  if ($version) { return [string]$match.Version -eq $version }
  return $true
}
`

const WingetPackageCheckScript = `
$pkgs = @($params.packages)
Get-Command winget.exe -ErrorAction Stop | Out-Null
` + wingetPackagePresenceScript + `
$tempPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".json")
try {
  $result = Invoke-Winget -Arguments @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity')
  if ($result.ExitCode -ne 0) {
    if ($result.Details.Count -gt 0) {
      throw ("winget export failed with exit code $($result.ExitCode)" + [Environment]::NewLine + [string]::Join([Environment]::NewLine, $result.Details))
    }
    throw "winget export failed with exit code $($result.ExitCode)"
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
  $ensure = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
  $isPresent = Test-WingetDesiredPresent -Spec $spec -InstalledMap $installedMap
  if ($ensure -eq 'absent') {
    if ($isPresent) { Write-Output 'true'; exit 0 }
  } else {
    if (-not $isPresent) { Write-Output 'true'; exit 0 }
  }
}
Write-Output 'false'
`

const WingetPackageApplyScript = `
$pkgs = @($params.packages)
Get-Command winget.exe -ErrorAction Stop | Out-Null
` + wingetPackagePresenceScript + `
$tempPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".json")
try {
  $result = Invoke-Winget -Arguments @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity')
  if ($result.ExitCode -ne 0) {
    if ($result.Details.Count -gt 0) {
      throw ("winget export failed with exit code $($result.ExitCode)" + [Environment]::NewLine + [string]::Join([Environment]::NewLine, $result.Details))
    }
    throw "winget export failed with exit code $($result.ExitCode)"
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
  $wingetArgs = @()
  if ($spec.args) {
    foreach ($arg in $spec.args) { $wingetArgs += [string]$arg }
  }
  $isPresent = Test-WingetDesiredPresent -Spec $spec -InstalledMap $installedMap
  if ($ensure -eq 'absent' -and -not $isPresent) { continue }
  if ($ensure -ne 'absent' -and $isPresent) { continue }
  $args = @()
  if ($ensure -eq 'absent') {
    $args = @('uninstall', '--id', $id, '--exact', '--disable-interactivity', '--accept-source-agreements')
  } else {
    $args = @('install', '--id', $id, '--exact', '--silent', '--disable-interactivity', '--accept-package-agreements', '--accept-source-agreements', '--scope', $scope)
  }
  if ($version) { $args += @('--version', $version) }
  if ($source) { $args += @('--source', $source) }
  $args += $wingetArgs
  $result = Invoke-Winget -Arguments $args
  if ($result.ExitCode -ne 0) {
    $combinedDetails = [string]::Join([Environment]::NewLine, $result.Details)
    # WinGet can turn "install an already-present package" into an update path
    # and return UPDATE_NOT_APPLICABLE even when the unversioned desired state is satisfied.
    if ($ensure -ne 'absent' -and $result.ExitCode -eq -1978335189) {
      if ((Test-WingetDesiredPresent -Spec $spec -InstalledMap $installedMap) -or ($combinedDetails -match 'No available upgrade found' -and $combinedDetails -match 'No newer package versions are available')) {
        continue
      }
    }
    if ($result.Details.Count -gt 0) {
      throw ("winget command failed for '$id' with exit code $($result.ExitCode)" + [Environment]::NewLine + $combinedDetails)
    }
    throw "winget command failed for '$id' with exit code $($result.ExitCode)"
  }
  $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  $env:Path = (@($machinePath, $userPath) | Where-Object { -not [string]::IsNullOrEmpty($_) }) -join ';'
}
`
