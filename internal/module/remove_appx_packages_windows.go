//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/winutil"
)

type RemoveAppxPackagesModule struct{}

// checkScript is the PowerShell body for Check. Get-AppxProvisionedPackage -Online
// is called once before the loop (it is a slow DISM enumeration).
const removeAppxCheckScript = `
$pkgs = @($params.packages)
$needsProvisioned = @($pkgs | Where-Object { -not $_.scope -or [string]$_.scope -eq 'both' -or [string]$_.scope -eq 'provisioned' })
$allProvisioned = if ($needsProvisioned.Count -gt 0) {
  @(Get-AppxProvisionedPackage -Online -ErrorAction SilentlyContinue)
} else { @() }

function Get-InstalledAppxMatches([string]$scope, [string]$name) {
  $pkgs = @()
  switch ($scope) {
    'current_user' { $pkgs = @(Get-AppxPackage -Name $name -ErrorAction SilentlyContinue) }
    'all_users'    { $pkgs = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
    'provisioned'  { return @() }
    'both'         { $pkgs = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
    default { throw "remove_appx_packages: unsupported scope $scope" }
  }
  return @($pkgs | Where-Object { $null -ne $_ -and -not [string]::IsNullOrWhiteSpace([string]$_.PackageFullName) })
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

const removeAppxCheckScriptWithOutput = `
$pkgs = @($params.packages)
$needsProvisioned = @($pkgs | Where-Object { -not $_.scope -or [string]$_.scope -eq 'both' -or [string]$_.scope -eq 'provisioned' })
$allProvisioned = if ($needsProvisioned.Count -gt 0) {
  @(Get-AppxProvisionedPackage -Online -ErrorAction SilentlyContinue)
} else { @() }

function Get-InstalledAppxMatches([string]$scope, [string]$name) {
  $pkgs = @()
  switch ($scope) {
    'current_user' { $pkgs = @(Get-AppxPackage -Name $name -ErrorAction SilentlyContinue) }
    'all_users'    { $pkgs = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
    'provisioned'  { return @() }
    'both'         { $pkgs = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
    default { throw "remove_appx_packages: unsupported scope $scope" }
  }
  return @($pkgs | Where-Object { $null -ne $_ -and -not [string]::IsNullOrWhiteSpace([string]$_.PackageFullName) })
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

// applyScript: Get-AppxProvisionedPackage -Online is called once before the loop
// and the cached list is filtered per package. Removed packages may still appear
// in the cached list but Remove-AppxProvisionedPackage with -ErrorAction SilentlyContinue
// handles that gracefully.
const removeAppxApplyScript = `
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

func (m *RemoveAppxPackagesModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return m.CheckWithOutput(ctx, params, nil)
}

func (m *RemoveAppxPackagesModule) CheckWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) (bool, error) {
	normalized, err := winutil.NormalizeRemoveAppxParams(params)
	if err != nil {
		return false, err
	}
	if onOutput == nil {
		return runWindowsPowerShellBool(ctx, normalized, removeAppxCheckScript)
	}
	return runWindowsPowerShellBoolWithOutput(ctx, normalized, removeAppxCheckScriptWithOutput, onOutput)
}

func (m *RemoveAppxPackagesModule) Apply(ctx context.Context, params map[string]any) error {
	return m.ApplyWithOutput(ctx, params, nil)
}

func (m *RemoveAppxPackagesModule) ApplyWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) error {
	normalized, err := winutil.NormalizeRemoveAppxParams(params)
	if err != nil {
		return err
	}
	return runWindowsPowerShellWithParamsWithOutput(ctx, normalized, removeAppxApplyScript, onOutput)
}
