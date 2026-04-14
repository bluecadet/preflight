package pscript

// RemoveAppxHelperFunctions is shared preamble for all remove_appx_packages scripts.
// Get-AppxProvisionedPackage -Online is a slow DISM call; $allProvisioned caches
// it once per script invocation rather than once per package.
const RemoveAppxHelperFunctions = `
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

const RemoveAppxCheckScript = RemoveAppxHelperFunctions + `
foreach ($spec in $pkgs) {
  $name = [string]$spec.name
  $scope = if ($spec.scope) { [string]$spec.scope } else { 'both' }
  $hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
  $installed = Get-InstalledAppxMatches $scope $name
  $provisioned = Get-ProvisionedAppxMatches $scope $name $hasWildcard
  if (($installed.Count + $provisioned.Count) -gt 0) { Write-Output 'true'; exit 0 }
}
Write-Output 'false'
`

const RemoveAppxCheckScriptWithOutput = RemoveAppxHelperFunctions + `
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

// ModuleRemoveAppxApplyScript preserves the module-side apply flow, including
// iterating raw installed package results before skipping empty PackageFullName
// entries with output.
const ModuleRemoveAppxApplyScript = `
$pkgs = @($params.packages)
$needsProvisioned = @($pkgs | Where-Object { -not $_.scope -or [string]$_.scope -eq 'both' -or [string]$_.scope -eq 'provisioned' })
$allProvisioned = if ($needsProvisioned.Count -gt 0) {
  @(Get-AppxProvisionedPackage -Online -ErrorAction SilentlyContinue)
} else { @() }

foreach ($spec in $pkgs) {
  $name = [string]$spec.name
  $scope = if ($spec.scope) { [string]$spec.scope } else { 'both' }
  $hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
  Write-Output ("processing appx package " + $name + " (" + $scope + ")")

  if ($scope -ne 'provisioned') {
    $installed = @()
    switch ($scope) {
      'current_user' { $installed = @(Get-AppxPackage -Name $name -ErrorAction SilentlyContinue) }
      'all_users'    { $installed = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
      'both'         { $installed = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
      default { throw "remove_appx_packages: unsupported scope $scope" }
    }
    foreach ($pkg in $installed) {
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
    @($allProvisioned | Where-Object {
      $displayName = [string]$_.DisplayName
      $packageName = [string]$_.PackageName
      -not [string]::IsNullOrWhiteSpace($packageName) -and (
        ($hasWildcard -and $displayName -like $name) -or
        (-not $hasWildcard -and $displayName -eq $name)
      )
    }) | ForEach-Object {
      Remove-AppxProvisionedPackage -Online -PackageName $_.PackageName -ErrorAction SilentlyContinue | Out-Null
    }
  }
}
`

// RemoveAppxApplyScript calls Get-AppxProvisionedPackage -Online once before the
// loop and filters the cached list per package. Removed packages may still appear
// in the cached list but Remove-AppxProvisionedPackage with
// -ErrorAction SilentlyContinue handles that gracefully.
const RemoveAppxApplyScript = RemoveAppxHelperFunctions + `
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

// RemoveAppxEnsureScript combines check and apply in one invocation, calling
// Get-AppxProvisionedPackage -Online exactly once regardless of outcome.
// Outputs "ok", "would-change" (dry-run), or "changed". $__pf_dry_run must be
// set before $params by the caller.
const RemoveAppxEnsureScript = RemoveAppxHelperFunctions + `
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
