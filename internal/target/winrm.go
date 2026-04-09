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

const winRMMaxInlinePowerShellCommandLen = 7000

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

func (t *WinRMTarget) Execute(ctx context.Context, taskID string, module string, params map[string]any, opts ExecutionOptions, dryRun bool, onOutput OutputFunc) (Result, error) {
	become, err := effectiveBecome(RuntimeKindWindowsPowerShell, opts)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}
	backend := &windowsTaskBackend{
		run:       t.runPS,
		copyPlain: t.CopyFile,
		tempDir:   t.RemoteTempDir(),
		become:    become,
	}
	registry := newWindowsPowerShellRegistry(backend)
	return executeRemoteModule(
		ctx,
		taskID,
		module,
		params,
		dryRun,
		onOutput,
		registry,
		func(module string) error {
			if _, ok := registry[module]; ok && become != nil {
				return fmt.Errorf("winrm: module %q does not support become", module)
			}
			return unsupportedRuntimeModuleError(RuntimeKindWindowsPowerShell, module)
		},
	)
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
	$arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
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

func (t *WinRMTarget) RunPowerShellScript(ctx context.Context, script string) (string, error) {
	return t.runPS(ctx, script)
}

func (t *WinRMTarget) RemoteTempDir() string {
	return `C:\Windows\Temp\preflight`
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
	if shouldStageWinRMPowerShellScript(script) {
		return t.runPSViaTempFile(ctx, script)
	}
	client, err := t.clientForUse()
	if err != nil {
		return "", err
	}
	stdout, stderr, code, err := client.RunPSWithContext(ctx, script)
	if err != nil {
		return "", fmt.Errorf("winrm powershell failed: %w", err)
	}
	if code != 0 {
		if isWinRMCommandLineTooLong(stderr) {
			return t.runPSViaTempFile(ctx, script)
		}
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
	if err != nil {
		return "", fmt.Errorf("winrm command failed: %w", err)
	}
	if code != 0 {
		return "", fmt.Errorf("winrm command exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

func (t *WinRMTarget) runPSViaTempFile(ctx context.Context, script string) (string, error) {
	remotePath := fmt.Sprintf(`%s\run-%d.ps1`, strings.TrimRight(t.RemoteTempDir(), `\/`), time.Now().UnixNano())
	if err := t.copyBytes(ctx, []byte(script), remotePath); err != nil {
		return "", fmt.Errorf("winrm powershell stage oversized script: %w", err)
	}
	defer func() {
		cleanupScript, cleanupErr := powershellJSONVar("path", remotePath)
		if cleanupErr != nil {
			return
		}
		_, _ = t.runPSDirect(ctx, cleanupScript+`
Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue
`)
	}()

	command := fmt.Sprintf(`powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%s"`, remotePath)
	out, err := t.runCmd(ctx, command)
	if err != nil {
		return "", fmt.Errorf("winrm powershell oversized script fallback: %w", err)
	}
	return out, nil
}

func (t *WinRMTarget) runPSDirect(ctx context.Context, script string) (string, error) {
	client, err := t.clientForUse()
	if err != nil {
		return "", err
	}
	stdout, stderr, code, err := client.RunPSWithContext(ctx, script)
	if err != nil {
		return "", fmt.Errorf("winrm powershell failed: %w", err)
	}
	if code != 0 {
		return "", fmt.Errorf("winrm powershell exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

func isWinRMCommandLineTooLong(stderr string) bool {
	return strings.Contains(strings.ToLower(stderr), "command line is too long")
}

func shouldStageWinRMPowerShellScript(script string) bool {
	encoded := encodePowerShellScript(script)
	commandLen := len("powershell.exe -NoProfile -NonInteractive -EncodedCommand ") + len(encoded)
	return commandLen >= winRMMaxInlinePowerShellCommandLen
}

// copyBytesChunkSize is the maximum raw bytes per upload round trip. Each
// chunk is base64-encoded and inlined into a PowerShell script which is then
// UTF-16LE + base64 encoded for -EncodedCommand. The WinRM shell (cmd.exe)
// enforces an ~8 KB command-line limit, so payloads above ~1.5 KB trigger
// "command line is too long". 1536 bytes leaves a comfortable margin.
const copyBytesChunkSize = 1536

func (t *WinRMTarget) copyBytes(ctx context.Context, data []byte, dst string) error {
	pathVar, err := powershellJSONVar("path", dst)
	if err != nil {
		return err
	}

	if len(data) <= copyBytesChunkSize {
		// Single round trip: create parent directory and write all bytes at once.
		// base64 uses only A-Za-z0-9+/= which cannot contain the ' delimiter.
		encoded := base64.StdEncoding.EncodeToString(data)
		_, err = t.runPSDirect(ctx, pathVar+fmt.Sprintf(`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($path, [Convert]::FromBase64String('%s'))
`, encoded))
		return err
	}

	if _, err := t.runPSDirect(ctx, pathVar+`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($path, @())
`); err != nil {
		return err
	}

	for start := 0; start < len(data); start += copyBytesChunkSize {
		end := min(start+copyBytesChunkSize, len(data))
		encoded := base64.StdEncoding.EncodeToString(data[start:end])
		appendScript, err := powershellJSONVar("path", dst)
		if err != nil {
			return err
		}
		// encoded is safe to interpolate directly into a single-quoted PS string:
		// base64 uses only A-Za-z0-9+/= which cannot contain the ' delimiter.
		// All other parameters use powershellJSONVar for injection safety.
		if _, err := t.runPSDirect(ctx, appendScript+fmt.Sprintf(`
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
	case "x64", "amd64", "64-bit":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	case "x86", "386", "32-bit":
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

func normalizeFirewallRuleParams(params map[string]any) (map[string]any, error) {
	normalized := cloneParams(params)
	ports, err := winutil.NormalizeFirewallPorts(normalized["ports"])
	if err != nil {
		return nil, fmt.Errorf("firewall_rule: %w", err)
	}
	normalized["ports"] = ports
	return normalized, nil
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

func paramString(params map[string]any, key, defaultVal string) (string, error) {
	value, ok := params[key]
	if !ok || value == nil {
		return defaultVal, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", key, value)
	}
	if text == "" {
		return defaultVal, nil
	}
	return text, nil
}

func paramStringRequired(params map[string]any, key string) (string, error) {
	value, ok := params[key]
	if !ok || value == nil {
		return "", fmt.Errorf("required param %q is missing", key)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", key, value)
	}
	if text == "" {
		return "", fmt.Errorf("required param %q must not be empty", key)
	}
	return text, nil
}

func cloneParams(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	cloned := make(map[string]any, len(params))
	maps.Copy(cloned, params)
	return cloned
}

func winRMPackageRemotePath(index int, source string) string {
	return fmt.Sprintf(`C:\Windows\Temp\preflight\%03d-%s`, index, filepath.Base(source))
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

const packageApplyScript = `
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

const shortcutCheckScript = `
$destination = [string]$params.destination
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Write-Output (Test-Path -LiteralPath $destination)
  exit 0
}
if (-not (Test-Path -LiteralPath $destination)) {
  Write-Output 'true'
  exit 0
}
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($destination)
$args = if ($params.args) { [string]$params.args } else { '' }
$icon = if ($params.icon) { [string]$params.icon } else { '' }
$needs = $shortcut.TargetPath -ne [string]$params.target -or $shortcut.Arguments -ne $args -or $shortcut.IconLocation -ne $icon
Write-Output $needs
`

const shortcutApplyScript = `
$destination = [string]$params.destination
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-Item -LiteralPath $destination -Force -ErrorAction SilentlyContinue
  exit 0
}
$parent = Split-Path -Parent $destination
if ($parent) {
  New-Item -ItemType Directory -Path $parent -Force | Out-Null
}
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($destination)
$shortcut.TargetPath = [string]$params.target
$shortcut.Arguments = if ($params.args) { [string]$params.args } else { '' }
$shortcut.IconLocation = if ($params.icon) { [string]$params.icon } else { '' }
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
$pkgs = @($params.packages)
Get-Command winget.exe -ErrorAction Stop | Out-Null
$tempPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".json")
try {
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity') -Wait -PassThru -NoNewWindow
  if ($process.ExitCode -ne 0) {
    throw "winget export failed with exit code $($process.ExitCode)"
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

const wingetPackageApplyScript = `
$pkgs = @($params.packages)
Get-Command winget.exe -ErrorAction Stop | Out-Null
$tempPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".json")
try {
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity') -Wait -PassThru -NoNewWindow
  if ($process.ExitCode -ne 0) {
    throw "winget export failed with exit code $($process.ExitCode)"
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

const removeAppxPackagesCheckScript = `
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
`

const removeAppxPackagesApplyScript = `
$pkgs = @($params.packages)
foreach ($spec in $pkgs) {
  $name = [string]$spec.name
  $scope = if ($spec.scope) { [string]$spec.scope } else { 'both' }
  $hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
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
  Write-Output ($null -ne $user)
  exit 0
}
if ($null -eq $user) {
  Write-Output 'true'
  exit 0
}
$needs = $false
if ($params.password) {
  $needs = $true
}
if ($params.groups) {
  foreach ($group in $params.groups) {
    $members = Get-LocalGroupMember -Group ([string]$group) -ErrorAction SilentlyContinue
    if (-not ($members | Where-Object { $_.Name -match ("(^|\\\\)" + [regex]::Escape($name) + "$") })) {
      $needs = $true
      break
    }
  }
}
Write-Output $needs
`

const userApplyScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-LocalUser -Name $name -ErrorAction SilentlyContinue
  exit 0
}
$passwordValue = if ($params.password) { [string]$params.password } else { '' }
$securePassword = $null
if ($passwordValue) {
  $securePassword = ConvertTo-SecureString $passwordValue -AsPlainText -Force
}
$user = Get-LocalUser -Name $name -ErrorAction SilentlyContinue
if ($null -eq $user) {
  if ($securePassword) {
    New-LocalUser -Name $name -Password $securePassword | Out-Null
  } else {
    New-LocalUser -Name $name -NoPassword | Out-Null
  }
} elseif ($securePassword) {
  $user | Set-LocalUser -Password $securePassword
}
if ($params.groups) {
  foreach ($group in $params.groups) {
    Add-LocalGroupMember -Group ([string]$group) -Member $name -ErrorAction SilentlyContinue
  }
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
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$rule = Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue | Select-Object -First 1
if ($ensure -eq 'absent') {
  Write-Output ($null -ne $rule)
  exit 0
}
if ($null -eq $rule) {
  Write-Output 'true'
  exit 0
}
$directionMap = @{ inbound = 'Inbound'; outbound = 'Outbound' }
$actionMap = @{ allow = 'Allow'; block = 'Block' }
$protocolMap = @{ tcp = 'TCP'; udp = 'UDP'; any = 'Any' }
$portFilter = $rule | Get-NetFirewallPortFilter
$needs = $rule.Direction -ne $directionMap[[string]$params.direction] -or $rule.Action -ne $actionMap[[string]$params.action]
if ([string]$params.protocol) {
  $needs = $needs -or $portFilter.Protocol -ne $protocolMap[[string]$params.protocol]
}
if ([string]$params.ports) {
  $needs = $needs -or [string]$portFilter.LocalPort -ne [string]$params.ports
}
Write-Output $needs
`

const firewallRuleApplyScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue
  exit 0
}
$directionMap = @{ inbound = 'Inbound'; outbound = 'Outbound' }
$actionMap = @{ allow = 'Allow'; block = 'Block' }
$protocolMap = @{ tcp = 'TCP'; udp = 'UDP'; any = 'Any' }
$existing = Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue | Select-Object -First 1
if ($null -eq $existing) {
  $newParams = @{
    DisplayName = $name
    Direction = $directionMap[[string]$params.direction]
    Action = $actionMap[[string]$params.action]
    Protocol = $protocolMap[[string]$params.protocol]
  }
  if ([string]$params.ports) {
    $newParams['LocalPort'] = [string]$params.ports
  }
  New-NetFirewallRule @newParams | Out-Null
  exit 0
}
Set-NetFirewallRule -DisplayName $name -Direction $directionMap[[string]$params.direction] -Action $actionMap[[string]$params.action] | Out-Null
if ([string]$params.protocol -or [string]$params.ports) {
  Set-NetFirewallRule -DisplayName $name -Protocol $protocolMap[[string]$params.protocol] -LocalPort ([string]$params.ports) | Out-Null
}
`
