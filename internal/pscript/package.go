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
function Test-WingetPackageListed {
  param(
    [string]$Id,
    [string]$Source
  )
  $listStdoutPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".stdout.log")
  $listStderrPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".stderr.log")
  try {
    $listArgs = @('list', '--id', $Id, '--exact', '--accept-source-agreements', '--disable-interactivity')
    if ($Source) { $listArgs += @('--source', $Source) }
    $process = Start-Process -FilePath 'winget.exe' -ArgumentList $listArgs -Wait -PassThru -NoNewWindow -RedirectStandardOutput $listStdoutPath -RedirectStandardError $listStderrPath
    return $process.ExitCode -eq 0
  } finally {
    Remove-Item -LiteralPath $listStdoutPath -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $listStderrPath -Force -ErrorAction SilentlyContinue
  }
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
    if (-not $version) { return (Test-WingetPackageListed -Id $id -Source $source) }
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
$stdoutPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".stdout.log")
$stderrPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".stderr.log")
try {
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity') -Wait -PassThru -NoNewWindow -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
  if ($process.ExitCode -ne 0) {
    $details = @()
    if (Test-Path -LiteralPath $stdoutPath) {
      $stdout = Get-Content -LiteralPath $stdoutPath -Raw
      if (-not [string]::IsNullOrWhiteSpace($stdout)) { $details += $stdout.Trim() }
    }
    if (Test-Path -LiteralPath $stderrPath) {
      $stderr = Get-Content -LiteralPath $stderrPath -Raw
      if (-not [string]::IsNullOrWhiteSpace($stderr)) { $details += $stderr.Trim() }
    }
    if ($details.Count -gt 0) {
      throw ("winget export failed with exit code $($process.ExitCode)" + [Environment]::NewLine + [string]::Join([Environment]::NewLine, $details))
    }
    throw "winget export failed with exit code $($process.ExitCode)"
  }
  $doc = Get-Content -LiteralPath $tempPath -Raw | ConvertFrom-Json
} finally {
  Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath $stdoutPath -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath $stderrPath -Force -ErrorAction SilentlyContinue
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
$stdoutPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".stdout.log")
$stderrPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".stderr.log")
try {
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity') -Wait -PassThru -NoNewWindow -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
  if ($process.ExitCode -ne 0) {
    $details = @()
    if (Test-Path -LiteralPath $stdoutPath) {
      $stdout = Get-Content -LiteralPath $stdoutPath -Raw
      if (-not [string]::IsNullOrWhiteSpace($stdout)) { $details += $stdout.Trim() }
    }
    if (Test-Path -LiteralPath $stderrPath) {
      $stderr = Get-Content -LiteralPath $stderrPath -Raw
      if (-not [string]::IsNullOrWhiteSpace($stderr)) { $details += $stderr.Trim() }
    }
    if ($details.Count -gt 0) {
      throw ("winget export failed with exit code $($process.ExitCode)" + [Environment]::NewLine + [string]::Join([Environment]::NewLine, $details))
    }
    throw "winget export failed with exit code $($process.ExitCode)"
  }
  $doc = Get-Content -LiteralPath $tempPath -Raw | ConvertFrom-Json
} finally {
  Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath $stdoutPath -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath $stderrPath -Force -ErrorAction SilentlyContinue
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
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList $args -Wait -PassThru -NoNewWindow
  if ($process.ExitCode -ne 0) {
    # WinGet can turn "install an already-present package" into an update path
    # and return UPDATE_NOT_APPLICABLE even when the unversioned desired state is satisfied.
    if ($ensure -ne 'absent' -and $process.ExitCode -eq -1978335189 -and (Test-WingetDesiredPresent -Spec $spec -InstalledMap $installedMap)) {
      continue
    }
    throw "winget command failed for '$id' with exit code $($process.ExitCode)"
  }
  $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  $env:Path = (@($machinePath, $userPath) | Where-Object { -not [string]::IsNullOrEmpty($_) }) -join ';'
}
`
