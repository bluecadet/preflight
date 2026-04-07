package target

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"time"

	"github.com/bluecadet/preflight/internal/winutil"
)

type windowsPowerShellBackend interface {
	powerShellScriptBackend
	CopyFile(ctx context.Context, src, dst string) error
	RemoteTempDir() string
}

func newWindowsPowerShellRegistry(backend windowsPowerShellBackend) remoteModuleRegistry {
	return remoteModuleRegistry{
		"directory": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsDirectory(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return "", applyWindowsDirectory(ctx, backend, params)
			},
		},
		"file": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsFile(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return "", applyWindowsFile(ctx, backend, params)
			},
		},
		"shell": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsCreates(ctx, backend, params, "shell")
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return applyWindowsShell(ctx, backend, params)
			},
		},
		"powershell": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkPowerShellModule(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return applyPowerShellModule(ctx, backend, params)
			},
		},
		"environment": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsEnvironment(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return "", applyWindowsEnvironment(ctx, backend, params)
			},
		},
		"wait": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsWait(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return "", applyWindowsWait(ctx, backend, params)
			},
		},
		"reboot": remoteModuleFuncs{
			check: func(context.Context, map[string]any) (bool, string, error) {
				return true, "", nil
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return "", applyWindowsReboot(ctx, backend, params)
			},
		},
		"registry": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsRegistry(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return applyWindowsRegistry(ctx, backend, params)
			},
		},
		"service": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsBooleanScript(ctx, backend, params, serviceCheckScript)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return windowsRunScript(ctx, backend, params, serviceApplyScript)
			},
		},
		"package": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				normalized, err := winutil.NormalizePackageParams(params)
				if err != nil {
					return false, "", err
				}
				return checkWindowsBooleanScript(ctx, backend, normalized, packageCheckScript)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return applyWindowsPackage(ctx, backend, params)
			},
		},
		"shortcut": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsBooleanScript(ctx, backend, params, shortcutCheckScript)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return windowsRunScript(ctx, backend, params, shortcutApplyScript)
			},
		},
		"scheduled_task": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsScheduledTask(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return applyWindowsScheduledTask(ctx, backend, params)
			},
		},
		"user": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsBooleanScript(ctx, backend, params, userCheckScript)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return windowsRunScript(ctx, backend, params, userApplyScript)
			},
		},
		"winget_package": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsWingetPackage(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return applyWindowsWingetPackage(ctx, backend, params)
			},
		},
		"remove_appx_packages": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsRemoveAppxPackages(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return applyWindowsRemoveAppxPackages(ctx, backend, params)
			},
		},
		"power_plan": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsBooleanScript(ctx, backend, params, powerPlanCheckScript)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return windowsRunScript(ctx, backend, params, powerPlanApplyScript)
			},
		},
		"windows_feature": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsBooleanScript(ctx, backend, params, windowsFeatureCheckScript)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return windowsRunScript(ctx, backend, params, windowsFeatureApplyScript)
			},
		},
		"firewall_rule": remoteModuleFuncs{
			check: func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkWindowsBooleanScript(ctx, backend, params, firewallRuleCheckScript)
			},
			apply: func(ctx context.Context, params map[string]any) (string, error) {
				return windowsRunScript(ctx, backend, params, firewallRuleApplyScript)
			},
		},
	}
}

func windowsRunScript(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, body string) (string, error) {
	script, err := powershellJSONVar("params", params)
	if err != nil {
		return "", err
	}
	return backend.RunPowerShellScript(ctx, script+"\n"+body)
}

func checkWindowsBooleanScript(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, body string) (bool, string, error) {
	out, err := windowsRunScript(ctx, backend, params, body)
	if err != nil {
		return false, "", err
	}
	value, err := parseWindowsBool(out)
	return value, "", err
}

func checkWindowsCreates(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, label string) (bool, string, error) {
	creates, _ := params["creates"].(string)
	if creates == "" {
		return true, "", nil
	}
	out, err := windowsRunScript(ctx, backend, map[string]any{"creates": creates}, `
Write-Output ([bool](Test-Path -LiteralPath $params.creates))
`)
	if err != nil {
		return false, "", fmt.Errorf("%s: %w", label, err)
	}
	ok, err := parseWindowsBool(out)
	if err != nil {
		return false, "", err
	}
	return !ok, "", nil
}

func checkWindowsDirectory(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (bool, string, error) {
	script, err := powershellJSONVar("params", params)
	if err != nil {
		return false, "", err
	}
	out, err := backend.RunPowerShellScript(ctx, script+`
$path = [string]$params.path
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Write-Output ([bool](Test-Path -LiteralPath $path))
  exit 0
}
if (-not (Test-Path -LiteralPath $path)) {
  Write-Output 'true'
  exit 0
}
$item = Get-Item -LiteralPath $path
Write-Output ([bool](-not $item.PSIsContainer))
`)
	if err != nil {
		return false, "", err
	}
	value, err := parseWindowsBool(out)
	return value, "", err
}

func applyWindowsDirectory(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) error {
	_, err := windowsRunScript(ctx, backend, params, `
$path = [string]$params.path
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-Item -LiteralPath $path -Force -Recurse -ErrorAction SilentlyContinue
  exit 0
}
New-Item -ItemType Directory -Path $path -Force | Out-Null
`)
	return err
}

func checkWindowsFile(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (bool, string, error) {
	dest, ok := params["dest"].(string)
	if !ok || dest == "" {
		return false, "", fmt.Errorf("winrm file: required param %q is missing", "dest")
	}
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}
	src, _ := params["src"].(string)

	script, err := powershellJSONVar("dest", dest)
	if err != nil {
		return false, "", err
	}
	out, err := backend.RunPowerShellScript(ctx, script+`
if (-not (Test-Path -LiteralPath $dest)) {
  Write-Output 'missing'
  exit 0
}
$item = Get-Item -LiteralPath $dest
if ($item.PSIsContainer) {
  throw "destination is a directory: $dest"
}
$hash = (Get-FileHash -LiteralPath $dest -Algorithm SHA256).Hash.ToLowerInvariant()
Write-Output ("present:" + $hash)
`)
	if err != nil {
		if ensure == "absent" && strings.Contains(err.Error(), "missing") {
			return false, "", nil
		}
		return false, "", err
	}
	trimmed := strings.TrimSpace(out)
	switch ensure {
	case "absent":
		return trimmed != "missing", "", nil
	case "present":
		if trimmed == "missing" {
			return true, "", nil
		}
		if src == "" {
			return false, "", nil
		}
		localHash, err := hashLocalFile(src)
		if err != nil {
			return false, "", err
		}
		remoteHash := strings.TrimPrefix(trimmed, "present:")
		return localHash != remoteHash, "", nil
	default:
		return false, "", fmt.Errorf("winrm file: unknown ensure value %q", ensure)
	}
}

func applyWindowsFile(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) error {
	dest, ok := params["dest"].(string)
	if !ok || dest == "" {
		return fmt.Errorf("winrm file: required param %q is missing", "dest")
	}
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}
	src, _ := params["src"].(string)

	switch ensure {
	case "absent":
		_, err := windowsRunScript(ctx, backend, map[string]any{"dest": dest}, `
Remove-Item -LiteralPath $params.dest -Force -ErrorAction SilentlyContinue
`)
		return err
	case "present":
		if src != "" {
			return backend.CopyFile(ctx, src, dest)
		}
		_, err := windowsRunScript(ctx, backend, map[string]any{"dest": dest}, `
$dir = Split-Path -Parent $params.dest
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($params.dest, @())
`)
		return err
	default:
		return fmt.Errorf("winrm file: unknown ensure value %q", ensure)
	}
}

func applyWindowsShell(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (string, error) {
	cmd, ok := params["cmd"].(string)
	if !ok || cmd == "" {
		return "", fmt.Errorf("shell: required param %q is missing", "cmd")
	}
	args, err := paramStringSlice(params, "args")
	if err != nil {
		return "", err
	}
	workingDir, _ := params["working_dir"].(string)

	script, err := powershellJSONVar("cmd", cmd)
	if err != nil {
		return "", err
	}
	psArgs, err := powershellJSONVar("args", args)
	if err != nil {
		return "", err
	}
	wd, err := powershellJSONVar("workingDir", workingDir)
	if err != nil {
		return "", err
	}
	return backend.RunPowerShellScript(ctx, script+`
`+psArgs+`
`+wd+`
if ($workingDir) {
  Set-Location -LiteralPath $workingDir
}
& $cmd @args
`)
}

func checkWindowsEnvironment(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (bool, string, error) {
	name, ok := params["name"].(string)
	if !ok || name == "" {
		return false, "", fmt.Errorf("environment: required param %q is missing", "name")
	}
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}
	scope, _ := params["scope"].(string)
	if scope == "" {
		scope = "Machine"
	}
	value, _ := params["value"].(string)

	script, err := powershellJSONVar("name", name)
	if err != nil {
		return false, "", err
	}
	psScope, err := powershellJSONVar("scope", normalizeEnvScope(scope))
	if err != nil {
		return false, "", err
	}
	psValue, err := powershellJSONVar("value", value)
	if err != nil {
		return false, "", err
	}
	out, err := backend.RunPowerShellScript(ctx, script+`
`+psScope+`
`+psValue+`
$current = [System.Environment]::GetEnvironmentVariable($name, $scope)
if (`+fmt.Sprintf("%q", ensure)+` -eq 'absent') {
  Write-Output ([bool]($current -ne $null -and $current -ne ''))
} else {
  Write-Output ([bool]($current -ne $value))
}
`)
	if err != nil {
		return false, "", err
	}
	needs, err := parseWindowsBool(out)
	return needs, "", err
}

func applyWindowsEnvironment(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) error {
	name, _ := params["name"].(string)
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}
	scope, _ := params["scope"].(string)
	if scope == "" {
		scope = "Machine"
	}
	value, _ := params["value"].(string)

	_, err := windowsRunScript(ctx, backend, map[string]any{
		"name":   name,
		"value":  value,
		"scope":  normalizeEnvScope(scope),
		"ensure": ensure,
	}, `
if ($params.ensure -eq 'absent') {
  [System.Environment]::SetEnvironmentVariable($params.name, $null, $params.scope)
  exit 0
}
[System.Environment]::SetEnvironmentVariable($params.name, [string]$params.value, $params.scope)
`)
	return err
}

func checkWindowsWait(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (bool, string, error) {
	condition, _ := params["condition"].(string)
	targetValue, _ := params["target"].(string)
	met, err := windowsWaitCondition(ctx, backend, condition, targetValue)
	if err != nil {
		return false, "", err
	}
	return !met, "", nil
}

func applyWindowsWait(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) error {
	condition, _ := params["condition"].(string)
	targetValue, _ := params["target"].(string)
	timeoutStr, _ := params["timeout"].(string)
	if timeoutStr == "" {
		timeoutStr = "5m"
	}
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return fmt.Errorf("wait: invalid timeout %q: %w", timeoutStr, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		met, err := windowsWaitCondition(ctx, backend, condition, targetValue)
		if err != nil {
			return err
		}
		if met {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait: timeout after %s waiting for condition %q on %q", timeoutStr, condition, targetValue)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func windowsWaitCondition(ctx context.Context, backend windowsPowerShellBackend, condition, targetValue string) (bool, error) {
	out, err := windowsRunScript(ctx, backend, map[string]any{
		"condition": condition,
		"target":    targetValue,
	}, `
switch ($params.condition) {
  'file_exists' {
    Write-Output ([bool](Test-Path -LiteralPath $params.target))
  }
  'port_open' {
    $parts = $params.target.Split(':')
    if ($parts.Length -lt 2) { throw "wait: port_open target must be host:port" }
    $host = $parts[0]
    $port = [int]$parts[1]
    $client = New-Object System.Net.Sockets.TcpClient
    try {
      $async = $client.BeginConnect($host, $port, $null, $null)
      $connected = $async.AsyncWaitHandle.WaitOne(2000, $false)
      if ($connected -and $client.Connected) {
        $client.EndConnect($async) | Out-Null
        Write-Output 'true'
      } else {
        Write-Output 'false'
      }
    } finally {
      $client.Close()
    }
  }
  'service_running' {
    $svc = Get-Service -Name $params.target -ErrorAction SilentlyContinue
    Write-Output ([bool]($svc -and $svc.Status -eq 'Running'))
  }
  default {
    throw "wait: unknown condition $($params.condition)"
  }
}
`)
	if err != nil {
		return false, err
	}
	return parseWindowsBool(out)
}

func applyWindowsReboot(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) error {
	timeout := 300
	switch raw := params["timeout"].(type) {
	case int:
		timeout = raw
	case int64:
		timeout = int(raw)
	case float64:
		timeout = int(raw)
	}
	_, err := backend.RunPowerShellScript(ctx, fmt.Sprintf("shutdown /r /t %d", timeout))
	return err
}

func checkWindowsWingetPackage(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (bool, string, error) {
	normalized, err := winutil.NormalizeWingetParams(params)
	if err != nil {
		return false, "", err
	}
	return checkWindowsBooleanScript(ctx, backend, normalized, wingetPackageCheckScript)
}

func applyWindowsWingetPackage(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (string, error) {
	normalized, err := winutil.NormalizeWingetParams(params)
	if err != nil {
		return "", err
	}
	return windowsRunScript(ctx, backend, normalized, wingetPackageApplyScript)
}

func checkWindowsRemoveAppxPackages(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (bool, string, error) {
	normalized, err := winutil.NormalizeRemoveAppxParams(params)
	if err != nil {
		return false, "", err
	}
	return checkWindowsBooleanScript(ctx, backend, normalized, removeAppxPackagesCheckScript)
}

func applyWindowsRemoveAppxPackages(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (string, error) {
	normalized, err := winutil.NormalizeRemoveAppxParams(params)
	if err != nil {
		return "", err
	}
	return windowsRunScript(ctx, backend, normalized, removeAppxPackagesApplyScript)
}

func applyWindowsPackage(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (string, error) {
	normalized, err := winutil.NormalizePackageParams(params)
	if err != nil {
		return "", err
	}
	list := normalized["packages"].([]any)
	for i, item := range list {
		spec := item.(map[string]any)
		source, _ := spec["source"].(string)
		ensure, _ := spec["ensure"].(string)
		if source == "" || ensure == "absent" {
			continue
		}
		tempName := filepath.Base(source)
		remotePath := joinWindowsPath(backend.RemoteTempDir(), tempName)
		if err := backend.CopyFile(ctx, source, remotePath); err != nil {
			return "", err
		}
		newSpec := make(map[string]any, len(spec))
		maps.Copy(newSpec, spec)
		newSpec["source"] = remotePath
		list[i] = newSpec
	}
	normalized["packages"] = list
	return windowsRunScript(ctx, backend, normalized, packageApplyScript)
}

func checkWindowsRegistry(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (bool, string, error) {
	normalized, err := winutil.NormalizeRegistryParams(params)
	if err != nil {
		return false, "", err
	}
	return checkWindowsBooleanScript(ctx, backend, normalized, registryCheckScript)
}

func applyWindowsRegistry(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (string, error) {
	normalized, err := winutil.NormalizeRegistryParams(params)
	if err != nil {
		return "", err
	}
	return windowsRunScript(ctx, backend, normalized, registryApplyScript)
}

func checkWindowsScheduledTask(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (bool, string, error) {
	normalized, err := winutil.NormalizeScheduledTaskParams(params)
	if err != nil {
		return false, "", err
	}
	if err := winutil.ValidateScheduledTaskParams(normalized); err != nil {
		return false, "", err
	}
	return checkWindowsBooleanScript(ctx, backend, normalized, scheduledTaskCheckScript)
}

func applyWindowsScheduledTask(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (string, error) {
	normalized, err := winutil.NormalizeScheduledTaskParams(params)
	if err != nil {
		return "", err
	}
	if err := winutil.ValidateScheduledTaskParams(normalized); err != nil {
		return "", err
	}
	return windowsRunScript(ctx, backend, normalized, scheduledTaskApplyScript)
}

func joinWindowsPath(base, elem string) string {
	if strings.HasSuffix(base, `\`) {
		return base + elem
	}
	return base + `\` + elem
}

type winRMWindowsBackend struct {
	target *WinRMTarget
}

func (b winRMWindowsBackend) RunPowerShellScript(ctx context.Context, script string) (string, error) {
	return b.target.runPS(ctx, script)
}

func (b winRMWindowsBackend) CopyFile(ctx context.Context, src, dst string) error {
	return b.target.CopyFile(ctx, src, dst)
}

func (b winRMWindowsBackend) RemoteTempDir() string {
	return `C:\Windows\Temp\preflight`
}
