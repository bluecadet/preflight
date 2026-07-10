package target

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/bluecadet/preflight/internal/winutil"
)

type windowsPowerShellBackend interface {
	powerShellScriptBackend
	CopyFile(ctx context.Context, src, dst string) error
	RemoteTempDir() string
}

// windowsPowerShellModuleRequiresFreshSession reports whether a Windows
// PowerShell module runs unbounded user-authored scripts and must therefore
// bypass any long-lived powershell.exe session a transport keeps for
// performance.
//
// Only WinRM currently consults this; LocalTarget has no persistent session
// to bypass, and SSH's persistent session is used by user-script invocations
// without an equivalent guard. If SSH ever needs the same recycle behaviour,
// it should call this function rather than maintain its own list.
func windowsPowerShellModuleRequiresFreshSession(module string) bool {
	mod, ok := windowsPowerShellModuleCatalog[module]
	return ok && mod.freshSession
}

// windowsPowerShellModuleCatalog records per-module capability flags
// independent of any backend. It is the source of truth for properties like
// freshSession that callers may need to consult before constructing a backend.
var windowsPowerShellModuleCatalog = map[string]struct {
	freshSession bool
}{
	"powershell": {freshSession: true},
}

// newWindowsPowerShellRegistry builds a ModuleRegistry for remote Windows targets
// (WinRM, SSH-Windows-PS). LocalTarget no longer calls this function — local become
// uses subprocess elevation via newSubprocessBecomeRegistry instead.
func newWindowsPowerShellRegistry(backend windowsPowerShellBackend) ModuleRegistry {
	supported := ModuleRegistry{
		"directory": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsDirectory(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, _ OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, applyWindowsDirectory(ctx, backend, params)
			},
		},
		"file": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsFile(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, _ OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, applyWindowsFile(ctx, backend, params)
			},
		},
		"shell": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsCreates(ctx, backend, params, "shell")
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyWindowsShell(ctx, backend, params, out)
			},
		},
		"powershell": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, out OutputFunc) (CheckResult, error) {
				return checkPowerShellModuleWithOutput(ctx, backend, params, out)
			},
			// applyPowerShellModule streams lines through out during execution.
			// Pass nil to applyStreamed so it only extracts a single-line message
			// without re-emitting lines that were already forwarded.
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyPowerShellModule(ctx, backend, params, out)
			},
			ensure: func(ctx context.Context, params map[string]any, dryRun bool, out OutputFunc) (EnsureResult, error) {
				return ensurePowerShellModule(ctx, backend, params, dryRun, out)
			},
		},
		"environment": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsEnvironment(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, _ OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, applyWindowsEnvironment(ctx, backend, params)
			},
			ensure: func(ctx context.Context, params map[string]any, dryRun bool, _ OutputFunc) (EnsureResult, error) {
				return ensureWindowsEnvironment(ctx, backend, params, dryRun)
			},
		},
		"wait": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsWait(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, _ OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, applyWindowsWait(ctx, backend, params)
			},
		},
		"reboot": moduleFuncs{
			check: func(context.Context, map[string]any, OutputFunc) (CheckResult, error) {
				return CheckResult{NeedsChange: true}, nil
			},
			apply: func(ctx context.Context, params map[string]any, _ OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, applyWindowsReboot(ctx, backend, params)
			},
		},
		"registry": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsRegistry(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyWindowsRegistry(ctx, backend, params, out)
			},
			ensure: func(ctx context.Context, params map[string]any, dryRun bool, _ OutputFunc) (EnsureResult, error) {
				return ensureWindowsRegistry(ctx, backend, params, dryRun)
			},
		},
		"service": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsBooleanScript(ctx, backend, params, serviceCheckScript)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				output, err := windowsRunScript(ctx, backend, params, serviceApplyScript)
				return applyStreamed(output, out), err
			},
		},
		"package": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				normalized, err := winutil.NormalizePackageParams(params)
				if err != nil {
					return CheckResult{}, err
				}
				return checkWindowsBooleanScript(ctx, backend, normalized, packageCheckScript)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyWindowsPackage(ctx, backend, params, out)
			},
		},
		"shortcut": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsShortcut(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyWindowsShortcut(ctx, backend, params, out)
			},
		},
		"scheduled_task": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsScheduledTask(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyWindowsScheduledTask(ctx, backend, params, out)
			},
		},
		"user": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsUser(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyWindowsUser(ctx, backend, params, out)
			},
		},
		"winget_package": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsWingetPackage(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyWindowsWingetPackage(ctx, backend, params, out)
			},
		},
		"remove_appx_packages": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, out OutputFunc) (CheckResult, error) {
				return checkWindowsRemoveAppxPackagesWithOutput(ctx, backend, params, out)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyWindowsRemoveAppxPackages(ctx, backend, params, out)
			},
			ensure: func(ctx context.Context, params map[string]any, dryRun bool, out OutputFunc) (EnsureResult, error) {
				return ensureWindowsRemoveAppxPackages(ctx, backend, params, dryRun, out)
			},
		},
		"power_plan": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsBooleanScript(ctx, backend, params, powerPlanCheckScript)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				output, err := windowsRunScript(ctx, backend, params, powerPlanApplyScript)
				return applyStreamed(output, out), err
			},
		},
		"windows_feature": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsBooleanScript(ctx, backend, params, windowsFeatureCheckScript)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				output, err := windowsRunScript(ctx, backend, params, windowsFeatureApplyScript)
				return applyStreamed(output, out), err
			},
		},
		"firewall_rule": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkWindowsFirewallRule(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyWindowsFirewallRule(ctx, backend, params, out)
			},
		},
	}
	return buildRemoteModuleRegistry(RuntimeKindWindowsPowerShell, supported, func(module string) error {
		return NewUnsupportedOnRuntimeError(module, RuntimeKindWindowsPowerShell)
	})
}

func windowsRunScript(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, body string) (string, error) {
	script, err := powershellJSONVar("params", params)
	if err != nil {
		return "", err
	}
	return backend.RunPowerShellScript(ctx, script+"\n"+body, nil)
}

func checkWindowsBooleanScript(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, body string) (CheckResult, error) {
	out, err := windowsRunScript(ctx, backend, params, body)
	if err != nil {
		return CheckResult{}, err
	}
	value, err := parseWindowsBool(out)
	return CheckResult{NeedsChange: value}, err
}

func checkWindowsCreates(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, label string) (CheckResult, error) {
	creates, _ := params["creates"].(string)
	if creates == "" {
		return CheckResult{NeedsChange: true}, nil
	}
	out, err := windowsRunScript(ctx, backend, map[string]any{
		"creates":     creates,
		"working_dir": params["working_dir"],
	}, `
$__pf_working_dir = if ($params.working_dir) { [string]$params.working_dir } else { '' }
$__pf_working_dir_pushed = $false
try {
  if ($__pf_working_dir) {
    Push-Location -LiteralPath $__pf_working_dir
    $__pf_working_dir_pushed = $true
  }
Write-Output ([bool](Test-Path -LiteralPath $params.creates))
} finally {
  if ($__pf_working_dir_pushed) {
    Pop-Location
  }
}
`)
	if err != nil {
		return CheckResult{}, fmt.Errorf("%s: %w", label, err)
	}
	ok, err := parseWindowsBool(out)
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{NeedsChange: !ok}, nil
}

func checkWindowsDirectory(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (CheckResult, error) {
	script, err := powershellJSONVar("params", params)
	if err != nil {
		return CheckResult{}, err
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
`, nil)
	if err != nil {
		return CheckResult{}, err
	}
	value, err := parseWindowsBool(out)
	return CheckResult{NeedsChange: value}, err
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

func checkWindowsFile(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (CheckResult, error) {
	dest, ok := params["dest"].(string)
	if !ok || dest == "" {
		return CheckResult{}, fmt.Errorf("windows file: required param %q is missing", "dest")
	}
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}
	src, _ := params["src"].(string)
	content, hasContent, err := fileContentParam(params, "windows file", src)
	if err != nil {
		return CheckResult{}, err
	}

	script, err := powershellJSONVar("dest", dest)
	if err != nil {
		return CheckResult{}, err
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
`, nil)
	if err != nil {
		if ensure == "absent" && strings.Contains(err.Error(), "missing") {
			return CheckResult{}, nil
		}
		return CheckResult{}, err
	}
	trimmed := strings.TrimSpace(out)
	switch ensure {
	case "absent":
		return CheckResult{NeedsChange: trimmed != "missing"}, nil
	case "present":
		if trimmed == "missing" {
			return CheckResult{NeedsChange: true}, nil
		}
		if src == "" {
			if !hasContent {
				return CheckResult{}, nil
			}
		}
		if hasContent {
			remoteHash := strings.TrimPrefix(trimmed, "present:")
			return CheckResult{NeedsChange: hashBytes([]byte(content)) != remoteHash}, nil
		}
		localHash, err := hashLocalFile(src)
		if err != nil {
			return CheckResult{}, err
		}
		remoteHash := strings.TrimPrefix(trimmed, "present:")
		return CheckResult{NeedsChange: localHash != remoteHash}, nil
	default:
		return CheckResult{}, fmt.Errorf("windows file: unknown ensure value %q", ensure)
	}
}

func applyWindowsFile(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) error {
	dest, ok := params["dest"].(string)
	if !ok || dest == "" {
		return fmt.Errorf("windows file: required param %q is missing", "dest")
	}
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}
	src, _ := params["src"].(string)
	content, hasContent, err := fileContentParam(params, "windows file", src)
	if err != nil {
		return err
	}

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
		if hasContent {
			_, err := windowsRunScript(ctx, backend, map[string]any{"dest": dest, "content": content}, `
$dir = Split-Path -Parent $params.dest
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($params.dest, [Text.Encoding]::UTF8.GetBytes([string]$params.content))
`)
			return err
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
		return fmt.Errorf("windows file: unknown ensure value %q", ensure)
	}
}

func applyWindowsShell(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, out OutputFunc) (ApplyResult, error) {
	cmd, ok := params["cmd"].(string)
	if !ok || cmd == "" {
		return ApplyResult{}, fmt.Errorf("shell: required param %q is missing", "cmd")
	}
	args, err := paramStringSlice(params, "args")
	if err != nil {
		return ApplyResult{}, err
	}
	workingDir, _ := params["working_dir"].(string)

	script, err := powershellJSONVar("cmd", cmd)
	if err != nil {
		return ApplyResult{}, err
	}
	psArgs, err := powershellJSONVar("args", args)
	if err != nil {
		return ApplyResult{}, err
	}
	wd, err := powershellJSONVar("workingDir", workingDir)
	if err != nil {
		return ApplyResult{}, err
	}
	if workingDir == "" {
		output, err := backend.RunPowerShellScript(ctx, script+`
`+psArgs+`
`+wd+`
if ($workingDir) {
  Set-Location -LiteralPath $workingDir
}
& $cmd @args
`, nil)
		return applyStreamed(output, out), err
	}
	taskScript := script + `
` + psArgs + `
& $cmd @args
`
	taskScript, err = wrapPowerShellWorkingDir(map[string]any{"working_dir": workingDir}, taskScript)
	if err != nil {
		return ApplyResult{}, err
	}
	output, err := backend.RunPowerShellScript(ctx, taskScript, nil)
	return applyStreamed(output, out), err
}

func checkWindowsEnvironment(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (CheckResult, error) {
	name, ok := params["name"].(string)
	if !ok || name == "" {
		return CheckResult{}, fmt.Errorf("environment: required param %q is missing", "name")
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
		return CheckResult{}, err
	}
	psScope, err := powershellJSONVar("scope", normalizeEnvScope(scope))
	if err != nil {
		return CheckResult{}, err
	}
	psValue, err := powershellJSONVar("value", value)
	if err != nil {
		return CheckResult{}, err
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
`, nil)
	if err != nil {
		return CheckResult{}, err
	}
	needs, err := parseWindowsBool(out)
	return CheckResult{NeedsChange: needs}, err
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

func ensureWindowsEnvironment(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, dryRun bool) (EnsureResult, error) {
	name, ok := params["name"].(string)
	if !ok || name == "" {
		return EnsureResult{}, fmt.Errorf("environment: required param %q is missing", "name")
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

	preamble := powerShellDryRunPreamble(dryRun)
	out, err := windowsRunScript(ctx, backend, map[string]any{
		"name":   name,
		"value":  value,
		"scope":  normalizeEnvScope(scope),
		"ensure": ensure,
	}, preamble+`
$current = [System.Environment]::GetEnvironmentVariable($params.name, $params.scope)
if ($params.ensure -eq 'absent') {
  if ($null -eq $current -or $current -eq '') { Write-Output 'ok'; exit 0 }
  if ($__pf_dry_run) { Write-Output 'would-change'; exit 0 }
  [System.Environment]::SetEnvironmentVariable($params.name, $null, $params.scope)
} else {
  if ($current -eq $params.value) { Write-Output 'ok'; exit 0 }
  if ($__pf_dry_run) { Write-Output 'would-change'; exit 0 }
  [System.Environment]::SetEnvironmentVariable($params.name, [string]$params.value, $params.scope)
}
Write-Output 'changed'
`)
	if err != nil {
		return EnsureResult{}, err
	}
	return parseEnsureMarkerResult("environment", out)
}

func checkWindowsWait(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (CheckResult, error) {
	condition, _ := params["condition"].(string)
	targetValue, _ := params["target"].(string)
	met, err := windowsWaitCondition(ctx, backend, condition, targetValue)
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{NeedsChange: !met}, nil
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
	_, err := backend.RunPowerShellScript(ctx, fmt.Sprintf("shutdown /r /t %d", timeout), nil)
	return err
}

func checkWindowsWingetPackage(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (CheckResult, error) {
	normalized, err := winutil.NormalizeWingetParams(params)
	if err != nil {
		return CheckResult{}, err
	}
	return checkWindowsBooleanScript(ctx, backend, normalized, wingetPackageCheckScript)
}

func applyWindowsWingetPackage(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, out OutputFunc) (ApplyResult, error) {
	normalized, err := winutil.NormalizeWingetParams(params)
	if err != nil {
		return ApplyResult{}, err
	}
	output, err := windowsRunScript(ctx, backend, normalized, wingetPackageApplyScript)
	return applyStreamed(output, out), err
}

func checkWindowsRemoveAppxPackagesWithOutput(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, onOutput OutputFunc) (CheckResult, error) {
	normalized, err := winutil.NormalizeRemoveAppxParams(params)
	if err != nil {
		return CheckResult{}, err
	}
	out, err := windowsRunScript(ctx, backend, normalized, removeAppxPackagesCheckScript)
	if err != nil {
		return CheckResult{}, err
	}
	needs, outputLines, err := parseWindowsBoolOutput(out)
	if err != nil {
		return CheckResult{}, err
	}
	if onOutput != nil {
		for _, line := range outputLines {
			onOutput(line)
		}
	}
	return CheckResult{NeedsChange: needs}, nil
}

func applyWindowsRemoveAppxPackages(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, out OutputFunc) (ApplyResult, error) {
	normalized, err := winutil.NormalizeRemoveAppxParams(params)
	if err != nil {
		return ApplyResult{}, err
	}
	output, err := windowsRunScript(ctx, backend, normalized, removeAppxPackagesApplyScript)
	return applyStreamed(output, out), err
}

func ensureWindowsRemoveAppxPackages(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, dryRun bool, onOutput OutputFunc) (EnsureResult, error) {
	normalized, err := winutil.NormalizeRemoveAppxParams(params)
	if err != nil {
		return EnsureResult{}, err
	}
	paramsScript, err := powershellJSONVar("params", normalized)
	if err != nil {
		return EnsureResult{}, err
	}
	preamble := powerShellDryRunPreamble(dryRun) + paramsScript + "\n"
	out, err := backend.RunPowerShellScript(ctx, preamble+removeAppxPackagesEnsureScript, nil)
	if err != nil {
		return EnsureResult{}, err
	}
	lines := splitOutputLines(out)
	if len(lines) == 0 {
		return EnsureResult{}, fmt.Errorf("remove_appx_packages ensure: empty output")
	}
	marker := lines[len(lines)-1]
	if onOutput != nil {
		for _, line := range lines[:len(lines)-1] {
			onOutput(line)
		}
	}
	return parseEnsureMarkerResult("remove_appx_packages", marker)
}

func applyWindowsPackage(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, out OutputFunc) (ApplyResult, error) {
	normalized, err := winutil.NormalizePackageParams(params)
	if err != nil {
		return ApplyResult{}, err
	}
	list := normalized["packages"].([]any)
	for i, item := range list {
		spec := item.(map[string]any)
		source, _ := spec["source"].(string)
		ensure, _ := spec["ensure"].(string)
		if source == "" || ensure == "absent" {
			continue
		}
		remotePath := winRMPackageRemotePath(i, source)
		if err := backend.CopyFile(ctx, source, remotePath); err != nil {
			return ApplyResult{}, err
		}
		newSpec := make(map[string]any, len(spec))
		maps.Copy(newSpec, spec)
		newSpec["source"] = remotePath
		list[i] = newSpec
	}
	normalized["packages"] = list
	output, err := windowsRunScript(ctx, backend, normalized, packageApplyScript)
	return applyStreamed(output, out), err
}

func checkWindowsShortcut(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (CheckResult, error) {
	if _, err := paramStringRequired(params, "destination"); err != nil {
		return CheckResult{}, err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return CheckResult{}, err
	}
	return checkWindowsBooleanScript(ctx, backend, params, shortcutCheckScript)
}

func applyWindowsShortcut(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, out OutputFunc) (ApplyResult, error) {
	if _, err := paramStringRequired(params, "destination"); err != nil {
		return ApplyResult{}, err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return ApplyResult{}, err
	}
	output, err := windowsRunScript(ctx, backend, params, shortcutApplyScript)
	return applyStreamed(output, out), err
}

func checkWindowsUser(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (CheckResult, error) {
	if _, err := paramStringRequired(params, "name"); err != nil {
		return CheckResult{}, err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return CheckResult{}, err
	}
	return checkWindowsBooleanScript(ctx, backend, params, userCheckScript)
}

func applyWindowsUser(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, out OutputFunc) (ApplyResult, error) {
	if _, err := paramStringRequired(params, "name"); err != nil {
		return ApplyResult{}, err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return ApplyResult{}, err
	}
	output, err := windowsRunScript(ctx, backend, params, userApplyScript)
	return applyStreamed(output, out), err
}

func checkWindowsFirewallRule(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (CheckResult, error) {
	if _, err := paramStringRequired(params, "name"); err != nil {
		return CheckResult{}, err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return CheckResult{}, err
	}
	normalized, err := normalizeFirewallRuleParams(params)
	if err != nil {
		return CheckResult{}, err
	}
	return checkWindowsBooleanScript(ctx, backend, normalized, firewallRuleCheckScript)
}

func applyWindowsFirewallRule(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, out OutputFunc) (ApplyResult, error) {
	if _, err := paramStringRequired(params, "name"); err != nil {
		return ApplyResult{}, err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return ApplyResult{}, err
	}
	normalized, err := normalizeFirewallRuleParams(params)
	if err != nil {
		return ApplyResult{}, err
	}
	output, err := windowsRunScript(ctx, backend, normalized, firewallRuleApplyScript)
	return applyStreamed(output, out), err
}

func checkWindowsRegistry(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (CheckResult, error) {
	normalized, err := winutil.NormalizeRegistryParams(params)
	if err != nil {
		return CheckResult{}, err
	}
	return checkWindowsBooleanScript(ctx, backend, normalized, registryCheckScript)
}

func applyWindowsRegistry(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, out OutputFunc) (ApplyResult, error) {
	normalized, err := winutil.NormalizeRegistryParams(params)
	if err != nil {
		return ApplyResult{}, err
	}
	output, err := windowsRunScript(ctx, backend, normalized, registryApplyScript)
	return applyStreamed(output, out), err
}

// ensureWindowsRegistry combines check and apply into a single PowerShell
// invocation, halving WinRM round trips for tasks that need applying.
func ensureWindowsRegistry(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, dryRun bool) (EnsureResult, error) {
	normalized, err := winutil.NormalizeRegistryParams(params)
	if err != nil {
		return EnsureResult{}, err
	}
	paramsScript, err := powershellJSONVar("params", normalized)
	if err != nil {
		return EnsureResult{}, err
	}
	preamble := powerShellDryRunPreamble(dryRun) + paramsScript + "\n"
	out, err := backend.RunPowerShellScript(ctx, preamble+registryEnsureScript, nil)
	if err != nil {
		return EnsureResult{}, err
	}
	return parseEnsureMarkerResult("registry", out)
}

func checkWindowsScheduledTask(ctx context.Context, backend windowsPowerShellBackend, params map[string]any) (CheckResult, error) {
	normalized, err := winutil.NormalizeScheduledTaskParams(params)
	if err != nil {
		return CheckResult{}, err
	}
	if err := winutil.ValidateScheduledTaskParams(normalized); err != nil {
		return CheckResult{}, err
	}
	return checkWindowsBooleanScript(ctx, backend, normalized, scheduledTaskCheckScript)
}

func applyWindowsScheduledTask(ctx context.Context, backend windowsPowerShellBackend, params map[string]any, out OutputFunc) (ApplyResult, error) {
	normalized, err := winutil.NormalizeScheduledTaskParams(params)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := winutil.ValidateScheduledTaskParams(normalized); err != nil {
		return ApplyResult{}, err
	}
	output, err := windowsRunScript(ctx, backend, normalized, scheduledTaskApplyScript)
	return applyStreamed(output, out), err
}
