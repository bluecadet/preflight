package target

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/masterzen/winrm"

	"github.com/bluecadet/preflight/internal/tasklog"
	"github.com/bluecadet/preflight/internal/winutil"
)

type WinRMConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	HTTPS    bool
	Insecure bool
	Timeout  time.Duration
}

type winRMClient interface {
	RunPSWithContext(ctx context.Context, command string) (string, string, int, error)
	RunCmdWithContext(ctx context.Context, command string) (string, string, int, error)
}

type winRMClientFactory func(WinRMConfig) (winRMClient, error)

var defaultWinRMClientFactory winRMClientFactory = func(cfg WinRMConfig) (winRMClient, error) {
	endpoint := winrm.NewEndpoint(cfg.Host, cfg.Port, cfg.HTTPS, cfg.Insecure, nil, nil, nil, cfg.Timeout)
	return winrm.NewClient(endpoint, cfg.Username, cfg.Password)
}

// WinRMTarget communicates with a remote Windows machine via WinRM.
type WinRMTarget struct {
	config        WinRMConfig
	clientFactory winRMClientFactory
	mu            sync.Mutex
	client        winRMClient
}

func NewWinRMTarget(cfg WinRMConfig) *WinRMTarget {
	if cfg.Port == 0 {
		if cfg.HTTPS {
			cfg.Port = 5986
		} else {
			cfg.Port = 5985
		}
	}
	return &WinRMTarget{
		config:        cfg,
		clientFactory: defaultWinRMClientFactory,
	}
}

func (t *WinRMTarget) Execute(ctx context.Context, taskID string, module string, params map[string]any, dryRun bool) (Result, error) {
	needsChange, checkOutput, err := t.checkModule(ctx, module, params)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}
	if !needsChange {
		msg := "already in desired state"
		if checkOutput != "" {
			msg = checkOutput
		}
		return Result{TaskID: taskID, Status: StatusOK, Message: msg}, nil
	}
	if dryRun {
		return Result{TaskID: taskID, Status: StatusChanged, Message: "would apply change (dry-run)"}, nil
	}
	applyOutput, err := t.applyModule(ctx, module, params)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}
	msg := "change applied"
	if strings.TrimSpace(applyOutput) != "" {
		msg = strings.TrimSpace(applyOutput)
	}
	return Result{TaskID: taskID, Status: StatusChanged, Message: msg}, nil
}

func (t *WinRMTarget) CopyFile(ctx context.Context, src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("winrm: read src %q: %w", src, err)
	}
	if err := t.copyBytes(ctx, data, dst); err != nil {
		return fmt.Errorf("winrm: copy %q -> %q: %w", src, dst, err)
	}
	return nil
}

func (t *WinRMTarget) ReadFile(ctx context.Context, path string) ([]byte, error) {
	script, err := powershellJSONVar("path", path)
	if err != nil {
		return nil, err
	}
	stdout, err := t.runPS(ctx, script+`
if (-not (Test-Path -LiteralPath $path)) {
  throw "file not found: $path"
}
[Convert]::ToBase64String([IO.File]::ReadAllBytes($path))
`)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(stdout))
	if err != nil {
		return nil, fmt.Errorf("winrm: decode remote file %q: %w", path, err)
	}
	return decoded, nil
}

func (t *WinRMTarget) Reachable(ctx context.Context) (bool, error) {
	_, err := t.runCmd(ctx, "echo preflight")
	if err != nil {
		return false, err
	}
	return true, nil
}

func (t *WinRMTarget) Info(ctx context.Context) (TargetInfo, error) {
	stdout, err := t.runPS(ctx, `
$os = Get-CimInstance Win32_OperatingSystem
$arch = (Get-CimInstance Win32_OperatingSystem).OSArchitecture
[pscustomobject]@{
  hostname = $env:COMPUTERNAME
  version  = [string]$os.Version
  build    = [string]$os.BuildNumber
  arch     = $arch
} | ConvertTo-Json -Compress
`)
	if err != nil {
		return TargetInfo{}, err
	}

	var payload struct {
		Hostname string `json:"hostname"`
		Version  string `json:"version"`
		Build    string `json:"build"`
		Arch     string `json:"arch"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); err != nil {
		return TargetInfo{}, fmt.Errorf("winrm: parse target info: %w", err)
	}

	return TargetInfo{
		Hostname:  payload.Hostname,
		OSVersion: payload.Version,
		OSBuild:   payload.Build,
		Arch:      normalizeWindowsArch(payload.Arch),
	}, nil
}

func (t *WinRMTarget) RunPowerShell(ctx context.Context, script string) (string, error) {
	return t.runPS(ctx, script)
}

func (t *WinRMTarget) clientForUse() (winRMClient, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.client != nil {
		return t.client, nil
	}
	if t.clientFactory == nil {
		t.clientFactory = defaultWinRMClientFactory
	}
	client, err := t.clientFactory(t.config)
	if err != nil {
		return nil, fmt.Errorf("winrm: create client: %w", err)
	}
	t.client = client
	return client, nil
}

func (t *WinRMTarget) runPS(ctx context.Context, script string) (string, error) {
	client, err := t.clientForUse()
	if err != nil {
		return "", err
	}
	stdout, stderr, code, err := client.RunPSWithContext(ctx, script)
	tasklog.EmitLines(ctx, "stdout", stdout)
	tasklog.EmitLines(ctx, "stderr", stderr)
	if err != nil {
		return "", fmt.Errorf("winrm powershell failed: %w", err)
	}
	if code != 0 {
		return "", fmt.Errorf("winrm powershell exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

func (t *WinRMTarget) runCmd(ctx context.Context, command string) (string, error) {
	client, err := t.clientForUse()
	if err != nil {
		return "", err
	}
	stdout, stderr, code, err := client.RunCmdWithContext(ctx, command)
	tasklog.EmitLines(ctx, "stdout", stdout)
	tasklog.EmitLines(ctx, "stderr", stderr)
	if err != nil {
		return "", fmt.Errorf("winrm command failed: %w", err)
	}
	if code != 0 {
		return "", fmt.Errorf("winrm command exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

func (t *WinRMTarget) copyBytes(ctx context.Context, data []byte, dst string) error {
	script, err := powershellJSONVar("path", dst)
	if err != nil {
		return err
	}
	if _, err := t.runPS(ctx, script+`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($path, @())
`); err != nil {
		return err
	}

	const chunkSize = 24 * 1024
	for start := 0; start < len(data); start += chunkSize {
		end := min(start+chunkSize, len(data))
		encoded := base64.StdEncoding.EncodeToString(data[start:end])
		appendScript, err := powershellJSONVar("path", dst)
		if err != nil {
			return err
		}
		if _, err := t.runPS(ctx, appendScript+fmt.Sprintf(`
$bytes = [Convert]::FromBase64String('%s')
$stream = [IO.File]::Open($path, [IO.FileMode]::Append, [IO.FileAccess]::Write, [IO.FileShare]::Read)
try {
  $stream.Write($bytes, 0, $bytes.Length)
} finally {
  $stream.Dispose()
}
`, encoded)); err != nil {
			return err
		}
	}
	return nil
}

func (t *WinRMTarget) checkModule(ctx context.Context, module string, params map[string]any) (bool, string, error) {
	switch module {
	case "directory":
		return t.checkDirectory(ctx, params)
	case "file":
		return t.checkFile(ctx, params)
	case "shell":
		return t.checkCreates(ctx, params, "shell")
	case "powershell":
		return t.checkPowershell(ctx, params)
	case "environment":
		return t.checkEnvironment(ctx, params)
	case "wait":
		return t.checkWait(ctx, params)
	case "reboot":
		return t.checkReboot(ctx, params)
	case "registry":
		return t.checkRegistry(ctx, params)
	case "service":
		return t.checkBooleanScript(ctx, params, serviceCheckScript)
	case "package":
		return t.checkBooleanScript(ctx, params, packageCheckScript)
	case "shortcut":
		return t.checkBooleanScript(ctx, params, shortcutCheckScript)
	case "scheduled_task":
		return t.checkScheduledTask(ctx, params)
	case "user":
		return t.checkBooleanScript(ctx, params, userCheckScript)
	case "winget_package":
		return t.checkBooleanScript(ctx, params, wingetPackageCheckScript)
	case "appx_package":
		return t.checkBooleanScript(ctx, params, appxPackageCheckScript)
	case "power_plan":
		return t.checkBooleanScript(ctx, params, powerPlanCheckScript)
	case "windows_feature":
		return t.checkBooleanScript(ctx, params, windowsFeatureCheckScript)
	case "firewall_rule":
		return t.checkBooleanScript(ctx, params, firewallRuleCheckScript)
	default:
		return false, "", fmt.Errorf("winrm: unknown module %q", module)
	}
}

func (t *WinRMTarget) applyModule(ctx context.Context, module string, params map[string]any) (string, error) {
	switch module {
	case "directory":
		return "", t.applyDirectory(ctx, params)
	case "file":
		return "", t.applyFile(ctx, params)
	case "shell":
		return t.applyShell(ctx, params)
	case "powershell":
		return t.applyPowershell(ctx, params)
	case "environment":
		return "", t.applyEnvironment(ctx, params)
	case "wait":
		return "", t.applyWait(ctx, params)
	case "reboot":
		return "", t.applyReboot(ctx, params)
	case "registry":
		return t.applyRegistry(ctx, params)
	case "service":
		return t.runScript(ctx, params, serviceApplyScript)
	case "package":
		return t.applyPackage(ctx, params)
	case "shortcut":
		return t.runScript(ctx, params, shortcutApplyScript)
	case "scheduled_task":
		return t.applyScheduledTask(ctx, params)
	case "user":
		return t.runScript(ctx, params, userApplyScript)
	case "winget_package":
		return t.runScript(ctx, params, wingetPackageApplyScript)
	case "appx_package":
		return t.runScript(ctx, params, appxPackageApplyScript)
	case "power_plan":
		return t.runScript(ctx, params, powerPlanApplyScript)
	case "windows_feature":
		return t.runScript(ctx, params, windowsFeatureApplyScript)
	case "firewall_rule":
		return t.runScript(ctx, params, firewallRuleApplyScript)
	default:
		return "", fmt.Errorf("winrm: unknown module %q", module)
	}
}

func (t *WinRMTarget) checkDirectory(ctx context.Context, params map[string]any) (bool, string, error) {
	script, err := powershellJSONVar("params", params)
	if err != nil {
		return false, "", err
	}
	out, err := t.runPS(ctx, script+`
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

func (t *WinRMTarget) applyDirectory(ctx context.Context, params map[string]any) error {
	_, err := t.runScript(ctx, params, `
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

func (t *WinRMTarget) checkFile(ctx context.Context, params map[string]any) (bool, string, error) {
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
	out, err := t.runPS(ctx, script+`
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

func (t *WinRMTarget) applyFile(ctx context.Context, params map[string]any) error {
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
		_, err := t.runScript(ctx, map[string]any{"dest": dest}, `
Remove-Item -LiteralPath $params.dest -Force -ErrorAction SilentlyContinue
`)
		return err
	case "present":
		if src != "" {
			return t.CopyFile(ctx, src, dest)
		}
		_, err := t.runScript(ctx, map[string]any{"dest": dest}, `
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

func (t *WinRMTarget) checkCreates(ctx context.Context, params map[string]any, label string) (bool, string, error) {
	creates, _ := params["creates"].(string)
	if creates == "" {
		return true, "", nil
	}
	out, err := t.runScript(ctx, map[string]any{"creates": creates}, `
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

func (t *WinRMTarget) checkPowershell(ctx context.Context, params map[string]any) (bool, string, error) {
	if checkScript, _ := params["check_script"].(string); strings.TrimSpace(checkScript) != "" {
		script, err := winutil.BuildPowerShellCheckScript(checkScript)
		if err != nil {
			return false, "", err
		}
		out, err := t.runPS(ctx, script)
		if err != nil {
			return false, "", err
		}
		result, err := winutil.ParsePowerShellCheckResult([]byte(out))
		if err != nil {
			return false, "", err
		}
		return result.NeedsChange, result.Message, nil
	}
	return t.checkCreates(ctx, params, "powershell")
}

func (t *WinRMTarget) applyPowershell(ctx context.Context, params map[string]any) (string, error) {
	if script, _ := params["script"].(string); script != "" {
		return t.runPS(ctx, script)
	}
	file, _ := params["file"].(string)
	if file == "" {
		return "", fmt.Errorf("powershell: one of 'script' or 'file' is required")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("powershell: read %q: %w", file, err)
	}
	args, err := paramStringSlice(params, "args")
	if err != nil {
		return "", err
	}
	script, err := powershellJSONVar("script", string(data))
	if err != nil {
		return "", err
	}
	scriptArgs, err := powershellJSONVar("scriptArgs", args)
	if err != nil {
		return "", err
	}
	return t.runPS(ctx, script+`
`+scriptArgs+`
$block = [ScriptBlock]::Create($script)
& $block @scriptArgs
`)
}

func (t *WinRMTarget) applyShell(ctx context.Context, params map[string]any) (string, error) {
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
	return t.runPS(ctx, script+`
`+psArgs+`
`+wd+`
if ($workingDir) {
  Set-Location -LiteralPath $workingDir
}
& $cmd @args
`)
}

func (t *WinRMTarget) checkEnvironment(ctx context.Context, params map[string]any) (bool, string, error) {
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
	out, err := t.runPS(ctx, script+`
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

func (t *WinRMTarget) applyEnvironment(ctx context.Context, params map[string]any) error {
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

	_, err := t.runScript(ctx, map[string]any{
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

func (t *WinRMTarget) checkWait(ctx context.Context, params map[string]any) (bool, string, error) {
	condition, _ := params["condition"].(string)
	targetValue, _ := params["target"].(string)
	met, err := t.waitCondition(ctx, condition, targetValue)
	if err != nil {
		return false, "", err
	}
	return !met, "", nil
}

func (t *WinRMTarget) applyWait(ctx context.Context, params map[string]any) error {
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
		met, err := t.waitCondition(ctx, condition, targetValue)
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

func (t *WinRMTarget) waitCondition(ctx context.Context, condition, targetValue string) (bool, error) {
	out, err := t.runScript(ctx, map[string]any{
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

func (t *WinRMTarget) checkReboot(_ context.Context, _ map[string]any) (bool, string, error) {
	return true, "", nil
}

func (t *WinRMTarget) applyReboot(ctx context.Context, params map[string]any) error {
	timeout := 300
	switch raw := params["timeout"].(type) {
	case int:
		timeout = raw
	case int64:
		timeout = int(raw)
	case float64:
		timeout = int(raw)
	}
	_, err := t.runCmd(ctx, fmt.Sprintf("shutdown /r /t %d", timeout))
	return err
}

func (t *WinRMTarget) checkBooleanScript(ctx context.Context, params map[string]any, body string) (bool, string, error) {
	out, err := t.runScript(ctx, params, body)
	if err != nil {
		return false, "", err
	}
	value, err := parseWindowsBool(out)
	return value, "", err
}

func (t *WinRMTarget) applyPackage(ctx context.Context, params map[string]any) (string, error) {
	source, _ := params["source"].(string)
	if source != "" {
		tempName := filepath.Base(source)
		remotePath := filepath.Join(os.TempDir(), "preflight", tempName)
		if err := t.CopyFile(ctx, source, remotePath); err != nil {
			return "", err
		}
		params = cloneParams(params)
		params["source"] = remotePath
	}
	return t.runScript(ctx, params, packageApplyScript)
}

func (t *WinRMTarget) checkRegistry(ctx context.Context, params map[string]any) (bool, string, error) {
	normalized, err := winutil.NormalizeRegistryParams(params)
	if err != nil {
		return false, "", err
	}
	return t.checkBooleanScript(ctx, normalized, registryCheckScript)
}

func (t *WinRMTarget) applyRegistry(ctx context.Context, params map[string]any) (string, error) {
	normalized, err := winutil.NormalizeRegistryParams(params)
	if err != nil {
		return "", err
	}
	return t.runScript(ctx, normalized, registryApplyScript)
}

func (t *WinRMTarget) checkScheduledTask(ctx context.Context, params map[string]any) (bool, string, error) {
	normalized, err := winutil.NormalizeScheduledTaskParams(params)
	if err != nil {
		return false, "", err
	}
	if err := winutil.ValidateScheduledTaskParams(normalized); err != nil {
		return false, "", err
	}
	return t.checkBooleanScript(ctx, normalized, scheduledTaskCheckScript)
}

func (t *WinRMTarget) applyScheduledTask(ctx context.Context, params map[string]any) (string, error) {
	normalized, err := winutil.NormalizeScheduledTaskParams(params)
	if err != nil {
		return "", err
	}
	if err := winutil.ValidateScheduledTaskParams(normalized); err != nil {
		return "", err
	}
	return t.runScript(ctx, normalized, scheduledTaskApplyScript)
}

func (t *WinRMTarget) runScript(ctx context.Context, params map[string]any, body string) (string, error) {
	script, err := powershellJSONVar("params", params)
	if err != nil {
		return "", err
	}
	return t.runPS(ctx, script+"\n"+body)
}

func powershellJSONVar(name string, value any) (string, error) {
	return winutil.JSONVarScript(name, value)
}

func parseWindowsBool(out string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(out)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected boolean output %q", strings.TrimSpace(out))
	}
}

func normalizeWindowsArch(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "64-bit":
		return "amd64"
	case "32-bit":
		return "386"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func normalizeEnvScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "user":
		return "User"
	default:
		return "Machine"
	}
}

func hashLocalFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("hash %q: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}

func paramStringSlice(params map[string]any, key string) ([]string, error) {
	value, ok := params[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []string:
		return typed, nil
	case []any:
		result := make([]string, 0, len(typed))
		for i, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d] must be a string, got %T", key, i, item)
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("%s must be a string list, got %T", key, value)
	}
}

func cloneParams(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	cloned := make(map[string]any, len(params))
	maps.Copy(cloned, params)
	return cloned
}

const registryCheckScript = `
$path = [string]$params.path
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
function Normalize-RegistryKind([string]$kind) {
  switch ($kind.ToLowerInvariant()) {
    'expandstring' { return 'expand_string' }
    'multistring' { return 'multi_string' }
    default { return $kind.ToLowerInvariant() }
  }
}
if ($ensure -eq 'absent') {
  Write-Output (Test-Path -LiteralPath $path)
  exit 0
}
if (-not (Test-Path -LiteralPath $path)) {
  if (-not $params.values) {
    Write-Output 'true'
    exit 0
  }
  $presentSpecs = @($params.values | Where-Object { -not $_.ensure -or $_.ensure -eq 'present' })
  Write-Output ([bool]($presentSpecs.Count -gt 0))
  exit 0
}
$needs = $false
if ($params.values) {
  $item = Get-Item -LiteralPath $path
  $props = Get-ItemProperty -LiteralPath $path
  foreach ($spec in $params.values) {
    $name = [string]$spec.name
    $ensureValue = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
    $prop = $props.PSObject.Properties[$name]
    if ($ensureValue -eq 'absent') {
      if ($null -ne $prop) {
        $needs = $true
        break
      }
      continue
    }
    if ($null -eq $prop) {
      $needs = $true
      break
    }
    $currentKind = Normalize-RegistryKind($item.GetValueKind($name).ToString())
    $desiredKind = [string]$spec.type
    if ($currentKind -ne $desiredKind) {
      $needs = $true
      break
    }
    switch ($desiredKind) {
      'string' { if ([string]$prop.Value -ne [string]$spec.data) { $needs = $true } }
      'expand_string' { if ([string]$prop.Value -ne [string]$spec.data) { $needs = $true } }
      'dword' { if ([int64]$prop.Value -ne [int64]$spec.data) { $needs = $true } }
      'qword' { if ([int64]$prop.Value -ne [int64]$spec.data) { $needs = $true } }
      'multi_string' {
        $current = @($prop.Value | ForEach-Object { [string]$_ })
        $desired = @($spec.data | ForEach-Object { [string]$_ })
        if ($current.Count -ne $desired.Count) {
          $needs = $true
        } else {
          for ($i = 0; $i -lt $current.Count; $i++) {
            if ($current[$i] -ne $desired[$i]) {
              $needs = $true
              break
            }
          }
        }
      }
      'binary' {
        $current = @($prop.Value | ForEach-Object { [int]$_ })
        $desired = @($spec.data | ForEach-Object { [int]$_ })
        if ($current.Count -ne $desired.Count) {
          $needs = $true
        } else {
          for ($i = 0; $i -lt $current.Count; $i++) {
            if ($current[$i] -ne $desired[$i]) {
              $needs = $true
              break
            }
          }
        }
      }
      default { throw "registry: unsupported type $desiredKind" }
    }
    if ($needs) {
      break
    }
  }
}
Write-Output $needs
`

const registryApplyScript = `
$path = [string]$params.path
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-Item -LiteralPath $path -Recurse -Force -ErrorAction SilentlyContinue
  exit 0
}
New-Item -Path $path -Force | Out-Null
if ($params.values) {
  foreach ($spec in $params.values) {
    $name = [string]$spec.name
    $ensureValue = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
    if ($ensureValue -eq 'absent') {
      Remove-ItemProperty -LiteralPath $path -Name $name -Force -ErrorAction SilentlyContinue
      continue
    }
    $kindMap = @{
      string = 'String'
      expand_string = 'ExpandString'
      dword = 'DWord'
      qword = 'QWord'
      multi_string = 'MultiString'
      binary = 'Binary'
    }
    $value = switch ([string]$spec.type) {
      'multi_string' { @($spec.data | ForEach-Object { [string]$_ }) }
      'binary' { [byte[]]@($spec.data | ForEach-Object { [byte][int]$_ }) }
      'dword' { [int]$spec.data }
      'qword' { [int64]$spec.data }
      default { $spec.data }
    }
    Remove-ItemProperty -LiteralPath $path -Name $name -Force -ErrorAction SilentlyContinue
    New-ItemProperty -LiteralPath $path -Name $name -Value $value -PropertyType $kindMap[[string]$spec.type] -Force | Out-Null
  }
}
`

const serviceCheckScript = `
$name = [string]$params.name
$desiredState = if ($params.state) { [string]$params.state } else { '' }
$desiredStartup = if ($params.startup_type) { [string]$params.startup_type } else { '' }
$filterName = $name.Replace("'", "''")
$service = Get-CimInstance Win32_Service -Filter ("Name='" + $filterName + "'")
if ($null -eq $service) {
  throw "service not found: $name"
}
$needs = $false
if ($desiredState -eq 'disabled') {
  if ($service.State -ne 'Stopped' -or $service.StartMode -ne 'Disabled') {
    $needs = $true
  }
} else {
  if ($desiredState -eq 'running' -and $service.State -ne 'Running') {
    $needs = $true
  }
  if ($desiredState -eq 'stopped' -and $service.State -ne 'Stopped') {
    $needs = $true
  }
  if ($desiredStartup) {
    $startupMap = @{ automatic = 'Auto'; manual = 'Manual'; disabled = 'Disabled' }
    if ($startupMap[$desiredStartup] -ne $service.StartMode) {
      $needs = $true
    }
  }
}
Write-Output $needs
`

const serviceApplyScript = `
$name = [string]$params.name
$desiredState = if ($params.state) { [string]$params.state } else { '' }
$desiredStartup = if ($params.startup_type) { [string]$params.startup_type } else { '' }
if ($desiredState -eq 'disabled') {
  Stop-Service -Name $name -Force -ErrorAction SilentlyContinue
  Set-Service -Name $name -StartupType Disabled
  exit 0
}
if ($desiredStartup) {
  $startupMap = @{ automatic = 'Automatic'; manual = 'Manual'; disabled = 'Disabled' }
  Set-Service -Name $name -StartupType $startupMap[$desiredStartup]
}
if ($desiredState -eq 'running') {
  Start-Service -Name $name
}
if ($desiredState -eq 'stopped') {
  Stop-Service -Name $name -Force
}
`

const packageCheckScript = `
$productId = if ($params.product_id) { [string]$params.product_id } else { '' }
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if (-not $productId) {
  Write-Output 'true'
  exit 0
}
$installed = Get-ItemProperty HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*, HKLM:\Software\Wow6432Node\Microsoft\Windows\CurrentVersion\Uninstall\* -ErrorAction SilentlyContinue | Where-Object { $_.PSChildName -eq $productId }
if ($ensure -eq 'absent') {
  Write-Output ([bool]($installed))
  exit 0
}
Write-Output ([bool](-not $installed))
`

const packageApplyScript = `
$source = [string]$params.source
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$args = @()
if ($params.args) {
  foreach ($item in $params.args) {
    $args += [string]$item
  }
}
if ($ensure -eq 'absent') {
  if ($params.product_id) {
    Start-Process msiexec.exe -ArgumentList @('/x', [string]$params.product_id, '/qn') -Wait -NoNewWindow
  }
  exit 0
}
if (-not $source) { throw "package source is required" }
$extension = [IO.Path]::GetExtension($source).ToLowerInvariant()
if ($extension -eq '.msi') {
  Start-Process msiexec.exe -ArgumentList (@('/i', $source, '/qn') + $args) -Wait -NoNewWindow
} else {
  Start-Process -FilePath $source -ArgumentList $args -Wait -NoNewWindow
}
`

const shortcutCheckScript = `
$destination = [string]$params.destination
Write-Output ([bool](-not (Test-Path -LiteralPath $destination)))
`

const shortcutApplyScript = `
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut([string]$params.destination)
$shortcut.TargetPath = [string]$params.target
if ($params.args) { $shortcut.Arguments = [string]$params.args }
if ($params.icon) { $shortcut.IconLocation = [string]$params.icon }
$shortcut.Save()
`

const scheduledTaskCheckScript = `
$path = [string]$params.path
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$task = Get-ScheduledTask -TaskPath $path -TaskName $name -ErrorAction SilentlyContinue
if ($ensure -eq 'absent') {
  Write-Output ([bool]($task))
  exit 0
}
if ($null -eq $task) {
  Write-Output 'true'
  exit 0
}
$execute = [string]$params.execute
$arguments = if ($params.arguments) { [string]$params.arguments } else { '' }
$workingDir = if ($params.working_dir) { [string]$params.working_dir } else { '' }
$trigger = [string]$params.trigger
$runAs = if ($params.run_as) { [string]$params.run_as } else { '' }
$runLevel = if ($params.run_level) { [string]$params.run_level } else { 'least' }
$delay = if ($params.delay) { [string]$params.delay } else { '' }
$enabled = if ($null -ne $params.enabled) { [bool]$params.enabled } else { $true }
$desiredStartAt = if ($params.start_at) { [string]$params.start_at } else { '' }

function Normalize-StartBoundary([string]$triggerName, $value) {
  if (-not $value) { return '' }
  $dt = Get-Date $value
  if ($triggerName -eq 'daily') {
    return $dt.ToString('HH:mm:ss')
  }
  return $dt.ToString('s')
}

$action = $task.Actions | Select-Object -First 1
$currentTriggerObject = $task.Triggers | Select-Object -First 1
$currentTrigger = switch ($currentTriggerObject.CimClass.CimClassName) {
  'MSFT_TaskLogonTrigger' { 'onlogon' }
  'MSFT_TaskBootTrigger' { 'startup' }
  'MSFT_TaskDailyTrigger' { 'daily' }
  'MSFT_TaskTimeTrigger' { 'once' }
  default { '' }
}
$currentDelay = if ($null -ne $currentTriggerObject.Delay) { [string]$currentTriggerObject.Delay } else { '' }
$currentStartAt = Normalize-StartBoundary $currentTrigger $currentTriggerObject.StartBoundary
$desiredStartAt = Normalize-StartBoundary $trigger $desiredStartAt
$currentEnabled = [bool]$task.Settings.Enabled
$currentRunLevel = if ([string]$task.Principal.RunLevel -eq 'Highest') { 'highest' } else { 'least' }

$needs = $action.Execute -ne $execute -or
  $action.Arguments -ne $arguments -or
  $action.WorkingDirectory -ne $workingDir -or
  $currentTrigger -ne $trigger -or
  $currentDelay -ne $delay -or
  $currentEnabled -ne $enabled -or
  $currentRunLevel -ne $runLevel
if ($trigger -eq 'daily' -or $trigger -eq 'once') {
  if ($currentStartAt -ne $desiredStartAt) {
    $needs = $true
  }
}
if ($runAs -and $task.Principal.UserId -ne $runAs) {
  $needs = $true
}
Write-Output $needs
`

const scheduledTaskApplyScript = `
$path = [string]$params.path
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Unregister-ScheduledTask -TaskPath $path -TaskName $name -Confirm:$false -ErrorAction SilentlyContinue
  exit 0
}
$triggerName = [string]$params.trigger
$startAt = if ($params.start_at) { [string]$params.start_at } else { '' }
switch ($triggerName) {
  'daily' { $trigger = New-ScheduledTaskTrigger -Daily -At (Get-Date $startAt) }
  'onlogon' { $trigger = New-ScheduledTaskTrigger -AtLogOn }
  'startup' { $trigger = New-ScheduledTaskTrigger -AtStartup }
  'once' { $trigger = New-ScheduledTaskTrigger -Once -At (Get-Date $startAt) }
  default { throw "scheduled_task: unsupported trigger $triggerName" }
}
$delay = if ($params.delay) { [string]$params.delay } else { '' }
if ($delay) {
  $trigger.Delay = $delay
}
$arguments = if ($params.arguments) { [string]$params.arguments } else { '' }
$workingDir = if ($params.working_dir) { [string]$params.working_dir } else { '' }
$action = New-ScheduledTaskAction -Execute ([string]$params.execute) -Argument $arguments -WorkingDirectory $workingDir
$runLevelMap = @{ least = 'Limited'; highest = 'Highest' }
if ($params.run_as) {
  Register-ScheduledTask -TaskPath $path -TaskName $name -Action $action -Trigger $trigger -User ([string]$params.run_as) -RunLevel $runLevelMap[[string]$params.run_level] -Force | Out-Null
} else {
  Register-ScheduledTask -TaskPath $path -TaskName $name -Action $action -Trigger $trigger -RunLevel $runLevelMap[[string]$params.run_level] -Force | Out-Null
}
if ($null -ne $params.enabled -and -not [bool]$params.enabled) {
  Disable-ScheduledTask -TaskPath $path -TaskName $name | Out-Null
} else {
  Enable-ScheduledTask -TaskPath $path -TaskName $name | Out-Null
}
`

const wingetPackageCheckScript = `
$id = [string]$params.id
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$source = if ($params.source) { [string]$params.source } else { '' }
$version = if ($params.version) { [string]$params.version } else { '' }
Get-Command winget.exe -ErrorAction Stop | Out-Null
$tempPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".json")
try {
  $args = @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity')
  if ($source) {
    $args += @('--source', $source)
  }
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList $args -Wait -PassThru -NoNewWindow
  if ($process.ExitCode -ne 0) {
    throw "winget export failed with exit code $($process.ExitCode)"
  }
  $doc = Get-Content -LiteralPath $tempPath -Raw | ConvertFrom-Json
} finally {
  Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
}
$packages = @()
foreach ($src in @($doc.Sources)) {
  foreach ($pkg in @($src.Packages)) {
    $packages += $pkg
  }
}
$match = $packages | Where-Object { $_.PackageIdentifier -eq $id } | Select-Object -First 1
$installed = $null -ne $match
if ($ensure -eq 'absent') {
  Write-Output $installed
  exit 0
}
if (-not $installed) {
  Write-Output 'true'
  exit 0
}
if ($version -and [string]$match.Version -ne $version) {
  Write-Output 'true'
  exit 0
}
Write-Output 'false'
`

const wingetPackageApplyScript = `
$id = [string]$params.id
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$source = if ($params.source) { [string]$params.source } else { '' }
$version = if ($params.version) { [string]$params.version } else { '' }
$scope = if ($params.scope) { [string]$params.scope } else { 'machine' }
Get-Command winget.exe -ErrorAction Stop | Out-Null
$args = @()
if ($ensure -eq 'absent') {
  $args = @('uninstall', '--id', $id, '--exact', '--disable-interactivity', '--accept-source-agreements')
} else {
  $args = @('install', '--id', $id, '--exact', '--silent', '--disable-interactivity', '--accept-package-agreements', '--accept-source-agreements', '--scope', $scope)
}
if ($version) {
  $args += @('--version', $version)
}
if ($source) {
  $args += @('--source', $source)
}
$process = Start-Process -FilePath 'winget.exe' -ArgumentList $args -Wait -PassThru -NoNewWindow
if ($process.ExitCode -ne 0) {
  throw "winget command failed with exit code $($process.ExitCode)"
}
`

const appxPackageCheckScript = `
$name = [string]$params.name
$scope = if ($params.scope) { [string]$params.scope } else { 'both' }
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'absent' }
if ($ensure -ne 'absent') {
  throw "appx_package: only ensure=absent is supported"
}
$hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
$installed = @()
switch ($scope) {
  'current_user' { $installed = @(Get-AppxPackage -Name $name -ErrorAction SilentlyContinue) }
  'all_users' { $installed = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
  'provisioned' { $installed = @() }
  'both' { $installed = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
  default { throw "appx_package: unsupported scope $scope" }
}
$provisioned = @()
if ($scope -eq 'provisioned' -or $scope -eq 'both') {
  $provisioned = @(Get-AppxProvisionedPackage -Online | Where-Object {
    if ($hasWildcard) { $_.DisplayName -like $name } else { $_.DisplayName -eq $name }
  })
}
Write-Output ([bool](($installed.Count + $provisioned.Count) -gt 0))
`

const appxPackageApplyScript = `
$name = [string]$params.name
$scope = if ($params.scope) { [string]$params.scope } else { 'both' }
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'absent' }
if ($ensure -ne 'absent') {
  throw "appx_package: only ensure=absent is supported"
}
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
  $hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
  Get-AppxProvisionedPackage -Online | Where-Object {
    if ($hasWildcard) { $_.DisplayName -like $name } else { $_.DisplayName -eq $name }
  } | ForEach-Object {
    Remove-AppxProvisionedPackage -Online -PackageName $_.PackageName -ErrorAction SilentlyContinue | Out-Null
  }
}
`

const powerPlanCheckScript = `
function Invoke-PowerCfg([string[]]$Arguments) {
  $output = & powercfg.exe @Arguments 2>&1
  if ($LASTEXITCODE -ne 0) {
    throw ($output -join [Environment]::NewLine)
  }
  return $output
}
function Get-Schemes() {
  $schemes = @()
  foreach ($line in Invoke-PowerCfg @('/list')) {
    if ($line -match 'Power Scheme GUID:\s*([A-Fa-f0-9-]{36})\s+\((.+?)\)\s*(\*)?') {
      $schemes += [pscustomobject]@{
        Guid = $matches[1]
        Name = $matches[2]
        Active = ($matches[3] -eq '*')
      }
    }
  }
  return $schemes
}
function Get-CurrentValue([string]$SchemeGuid, [string]$Subgroup, [string]$Setting, [string]$Kind) {
  $pattern = if ($Kind -eq 'ac') { 'Current AC Power Setting Index:\s*0x([0-9A-Fa-f]+)' } else { 'Current DC Power Setting Index:\s*0x([0-9A-Fa-f]+)' }
  foreach ($line in Invoke-PowerCfg @('/query', $SchemeGuid, $Subgroup, $Setting)) {
    if ($line -match $pattern) {
      return [Convert]::ToInt64($matches[1], 16)
    }
  }
  throw "power_plan: unable to read $Kind value for $Subgroup/$Setting"
}
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$activate = if ($null -ne $params.activate) { [bool]$params.activate } else { $true }
$scheme = Get-Schemes | Where-Object { $_.Name -eq $name } | Select-Object -First 1
if ($ensure -eq 'absent') {
  Write-Output ($null -ne $scheme)
  exit 0
}
if ($null -eq $scheme) {
  Write-Output 'true'
  exit 0
}
$needs = $false
if ($activate -and -not $scheme.Active) {
  $needs = $true
}
if ($params.settings) {
  foreach ($setting in $params.settings) {
    if ($null -ne $setting.ac_value) {
      if ((Get-CurrentValue $scheme.Guid ([string]$setting.subgroup) ([string]$setting.setting) 'ac') -ne [int64]$setting.ac_value) {
        $needs = $true
        break
      }
    }
    if ($null -ne $setting.dc_value) {
      if ((Get-CurrentValue $scheme.Guid ([string]$setting.subgroup) ([string]$setting.setting) 'dc') -ne [int64]$setting.dc_value) {
        $needs = $true
        break
      }
    }
  }
}
Write-Output $needs
`

const powerPlanApplyScript = `
function Invoke-PowerCfg([string[]]$Arguments) {
  $output = & powercfg.exe @Arguments 2>&1
  if ($LASTEXITCODE -ne 0) {
    throw ($output -join [Environment]::NewLine)
  }
  return $output
}
function Resolve-BaseScheme([string]$Value) {
  switch ($Value.ToLowerInvariant()) {
    'balanced' { return 'SCHEME_BALANCED' }
    'high_performance' { return 'SCHEME_MIN' }
    'high-performance' { return 'SCHEME_MIN' }
    'power_saver' { return 'SCHEME_MAX' }
    'power-saver' { return 'SCHEME_MAX' }
    default { return $Value }
  }
}
function Get-Schemes() {
  $schemes = @()
  foreach ($line in Invoke-PowerCfg @('/list')) {
    if ($line -match 'Power Scheme GUID:\s*([A-Fa-f0-9-]{36})\s+\((.+?)\)\s*(\*)?') {
      $schemes += [pscustomobject]@{
        Guid = $matches[1]
        Name = $matches[2]
        Active = ($matches[3] -eq '*')
      }
    }
  }
  return $schemes
}
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$activate = if ($null -ne $params.activate) { [bool]$params.activate } else { $true }
$base = if ($params.base) { Resolve-BaseScheme([string]$params.base) } else { 'SCHEME_BALANCED' }
$scheme = Get-Schemes | Where-Object { $_.Name -eq $name } | Select-Object -First 1
if ($ensure -eq 'absent') {
  if ($null -ne $scheme) {
    if ($scheme.Active) {
      Invoke-PowerCfg @('/setactive', 'SCHEME_BALANCED') | Out-Null
    }
    Invoke-PowerCfg @('/delete', $scheme.Guid) | Out-Null
  }
  exit 0
}
if ($null -eq $scheme) {
  $output = Invoke-PowerCfg @('/duplicatescheme', $base)
  $newGuid = ''
  foreach ($line in @($output)) {
    if ($line -match '([A-Fa-f0-9-]{36})') {
      $newGuid = $matches[1]
      break
    }
  }
  if (-not $newGuid) {
    throw "power_plan: unable to determine duplicated scheme GUID"
  }
  Invoke-PowerCfg @('/changename', $newGuid, $name) | Out-Null
  $scheme = Get-Schemes | Where-Object { $_.Guid -eq $newGuid } | Select-Object -First 1
}
if ($null -eq $scheme) {
  throw "power_plan: unable to resolve scheme $name"
}
Invoke-PowerCfg @('/changename', $scheme.Guid, $name) | Out-Null
if ($params.settings) {
  foreach ($setting in $params.settings) {
    if ($null -ne $setting.ac_value) {
      Invoke-PowerCfg @('/setacvalueindex', $scheme.Guid, [string]$setting.subgroup, [string]$setting.setting, [string]$setting.ac_value) | Out-Null
    }
    if ($null -ne $setting.dc_value) {
      Invoke-PowerCfg @('/setdcvalueindex', $scheme.Guid, [string]$setting.subgroup, [string]$setting.setting, [string]$setting.dc_value) | Out-Null
    }
  }
}
if ($activate) {
  Invoke-PowerCfg @('/setactive', $scheme.Guid) | Out-Null
}
`

const userCheckScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$user = Get-LocalUser -Name $name -ErrorAction SilentlyContinue
if ($ensure -eq 'absent') {
  Write-Output ([bool]($user))
  exit 0
}
Write-Output ([bool](-not $user))
`

const userApplyScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-LocalUser -Name $name -ErrorAction SilentlyContinue
  exit 0
}
if (-not (Get-LocalUser -Name $name -ErrorAction SilentlyContinue)) {
  $secure = ConvertTo-SecureString ([string]$params.password) -AsPlainText -Force
  New-LocalUser -Name $name -Password $secure | Out-Null
}
`

const windowsFeatureCheckScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$feature = Get-WindowsOptionalFeature -Online -FeatureName $name -ErrorAction SilentlyContinue
if (-not $feature) { throw "windows feature not found: $name" }
if ($ensure -eq 'absent') {
  Write-Output ([bool]($feature.State -ne 'Disabled'))
  exit 0
}
Write-Output ([bool]($feature.State -ne 'Enabled'))
`

const windowsFeatureApplyScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Disable-WindowsOptionalFeature -Online -FeatureName $name -NoRestart | Out-Null
  exit 0
}
Enable-WindowsOptionalFeature -Online -FeatureName $name -NoRestart | Out-Null
`

const firewallRuleCheckScript = `
$name = [string]$params.name
$rule = Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Write-Output ([bool]($rule))
  exit 0
}
Write-Output ([bool](-not $rule))
`

const firewallRuleApplyScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue
  exit 0
}
$ports = $null
if ($params.ports) {
  if ($params.ports -is [System.Array]) {
    $ports = (($params.ports | ForEach-Object { [string]$_ }) -join ',')
  } else {
    $ports = [string]$params.ports
  }
}
$protocol = if ($params.protocol) { [string]$params.protocol } else { 'tcp' }
New-NetFirewallRule -DisplayName $name -Direction ([string]$params.direction) -Action ([string]$params.action) -Protocol $protocol -LocalPort $ports | Out-Null
`
