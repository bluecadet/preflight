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
`

const WingetPackageCheckScript = `
$pkgs = @($params.packages)
Get-Command winget.exe -ErrorAction Stop | Out-Null
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

const WingetPackageApplyScript = `
$pkgs = @($params.packages)
Get-Command winget.exe -ErrorAction Stop | Out-Null
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
