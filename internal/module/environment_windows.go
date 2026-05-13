//go:build windows

package module

import (
	"context"
	"fmt"

	"github.com/bluecadet/preflight/internal/target"
)

// EnvironmentModule manages environment variables on Windows.
type EnvironmentModule struct{}

func (m *EnvironmentModule) Check(ctx context.Context, params map[string]any, _ target.OutputFunc) (target.CheckResult, error) {
	var p EnvironmentParams
	if err := Decode(params, &p); err != nil {
		return target.CheckResult{}, err
	}
	needed, err := runWindowsPowerShellBool(ctx, params, `
$name   = [string]$params.name
$scope  = if ($params.scope)  { [string]$params.scope  } else { 'machine' }
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$target = if ($scope -eq 'user') { [System.EnvironmentVariableTarget]::User } else { [System.EnvironmentVariableTarget]::Machine }
$current = [System.Environment]::GetEnvironmentVariable($name, $target)
if ($ensure -eq 'absent') {
  Write-Output ($null -ne $current -and $current -ne '')
  exit 0
}
$value = [string]$params.value
Write-Output ($current -ne $value)
`)
	return target.CheckResult{NeedsChange: needed}, err
}

func (m *EnvironmentModule) Apply(ctx context.Context, params map[string]any, _ target.OutputFunc) (target.ApplyResult, error) {
	var p EnvironmentParams
	if err := Decode(params, &p); err != nil {
		return target.ApplyResult{}, err
	}
	_, err := runWindowsPowerShellWithParams(ctx, params, `
$name   = [string]$params.name
$scope  = if ($params.scope)  { [string]$params.scope  } else { 'machine' }
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$target = if ($scope -eq 'user') { [System.EnvironmentVariableTarget]::User } else { [System.EnvironmentVariableTarget]::Machine }
if ($ensure -eq 'absent') {
  [System.Environment]::SetEnvironmentVariable($name, $null, $target)
  exit 0
}
$value = [string]$params.value
[System.Environment]::SetEnvironmentVariable($name, $value, $target)
`)
	if err != nil {
		return target.ApplyResult{}, fmt.Errorf("environment: %w", err)
	}
	return target.ApplyResult{}, nil
}
