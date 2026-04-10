//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/winutil"
)

type RemoveAppxPackagesModule struct{}

func (m *RemoveAppxPackagesModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return m.CheckWithOutput(ctx, params, nil)
}

func (m *RemoveAppxPackagesModule) CheckWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) (bool, error) {
	normalized, err := winutil.NormalizeRemoveAppxParams(params)
	if err != nil {
		return false, err
	}
	if onOutput == nil {
		return runWindowsPowerShellBool(ctx, normalized, `
$pkgs = @($params.packages)
foreach ($spec in $pkgs) {
  $name = [string]$spec.name
  $scope = if ($spec.scope) { [string]$spec.scope } else { 'both' }
  $hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
  $installed = @()
  switch ($scope) {
    'current_user' { $installed = @(Get-AppxPackage -Name $name -ErrorAction SilentlyContinue) }
    'all_users'    { $installed = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
    'provisioned'  { $installed = @() }
    'both'         { $installed = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
    default { throw "remove_appx_packages: unsupported scope $scope" }
  }
  $provisioned = @()
  if ($scope -eq 'provisioned' -or $scope -eq 'both') {
    $provisioned = @(Get-AppxProvisionedPackage -Online | Where-Object {
      if ($hasWildcard) { $_.DisplayName -like $name } else { $_.DisplayName -eq $name }
    })
  }
  if (($installed.Count + $provisioned.Count) -gt 0) { Write-Output 'true'; exit 0 }
}
Write-Output 'false'
`)
	}
	return runWindowsPowerShellBoolWithOutput(ctx, normalized, `
$pkgs = @($params.packages)
foreach ($spec in $pkgs) {
  $name = [string]$spec.name
  $scope = if ($spec.scope) { [string]$spec.scope } else { 'both' }
  $hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
  Write-Output ("checking appx package " + $name + " (" + $scope + ")")
  $installed = @()
  switch ($scope) {
    'current_user' { $installed = @(Get-AppxPackage -Name $name -ErrorAction SilentlyContinue) }
    'all_users'    { $installed = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
    'provisioned'  { $installed = @() }
    'both'         { $installed = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
    default { throw "remove_appx_packages: unsupported scope $scope" }
  }
  $provisioned = @()
  if ($scope -eq 'provisioned' -or $scope -eq 'both') {
    $provisioned = @(Get-AppxProvisionedPackage -Online | Where-Object {
      if ($hasWildcard) { $_.DisplayName -like $name } else { $_.DisplayName -eq $name }
    })
  }
  if (($installed.Count + $provisioned.Count) -gt 0) { Write-Output 'true'; exit 0 }
}
Write-Output 'false'
`, onOutput)
}

func (m *RemoveAppxPackagesModule) Apply(ctx context.Context, params map[string]any) error {
	return m.ApplyWithOutput(ctx, params, nil)
}

func (m *RemoveAppxPackagesModule) ApplyWithOutput(ctx context.Context, params map[string]any, onOutput target.OutputFunc) error {
	normalized, err := winutil.NormalizeRemoveAppxParams(params)
	if err != nil {
		return err
	}
	return runWindowsPowerShellWithParamsWithOutput(ctx, normalized, `
$pkgs = @($params.packages)
foreach ($spec in $pkgs) {
  $name = [string]$spec.name
  $scope = if ($spec.scope) { [string]$spec.scope } else { 'both' }
  $hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
  Write-Output ("processing appx package " + $name + " (" + $scope + ")")

  if ($scope -eq 'current_user') {
    Get-AppxPackage -Name $name -ErrorAction SilentlyContinue | ForEach-Object {
      Remove-AppxPackage -Package $_.PackageFullName -ErrorAction SilentlyContinue
    }
  } elseif ($scope -eq 'all_users' -or $scope -eq 'both') {
    Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue | ForEach-Object {
      try {
        Remove-AppxPackage -Package $_.PackageFullName -AllUsers -ErrorAction Stop
      } catch {
        Remove-AppxPackage -Package $_.PackageFullName -ErrorAction SilentlyContinue
      }
    }
  }

  if ($scope -eq 'provisioned' -or $scope -eq 'both') {
    Get-AppxProvisionedPackage -Online | Where-Object {
      if ($hasWildcard) { $_.DisplayName -like $name } else { $_.DisplayName -eq $name }
    } | ForEach-Object {
      Remove-AppxProvisionedPackage -Online -PackageName $_.PackageName -ErrorAction SilentlyContinue | Out-Null
    }
  }
}
`, onOutput)
}
