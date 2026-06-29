package target

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// getWinRMConfigFromEnv parses PREFLIGHT_TEST_WINRM as a JSON blob with
// host, port, user, and pass fields. Returns nil + false when unset so
// callers can t.Skip cleanly.
func getWinRMConfigFromEnv() (*WinRMConfig, bool) {
	raw := os.Getenv("PREFLIGHT_TEST_WINRM")
	if raw == "" {
		return nil, false
	}
	var cfg struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"user"`
		Password string `json:"pass"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, false
	}
	if cfg.Host == "" {
		return nil, false
	}
	if cfg.Port == 0 {
		cfg.Port = 5985
	}
	return &WinRMConfig{
		Host:     cfg.Host,
		Port:     cfg.Port,
		Username: cfg.Username,
		Password: cfg.Password,
		Timeout:  60 * time.Second,
	}, true
}

// TestWinRMIntegration_Registry exercises the registry module against a real
// Windows endpoint over WinRM. It is gated by PREFLIGHT_TEST_WINRM and the
// sacrificial sentinel, so it is inert on CI and on any dev machine without
// a configured VM.
//
// The test uses an independent PowerShell oracle (Get-ItemProperty) to assert
// correctness rather than relying on the module's own Check().
func TestWinRMIntegration_Registry(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM is not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	// The namespace under which all test mutations happen. It sits under the
	// sacrificial parent tree (HKLM\SOFTWARE\PreflightTest) so cleanup is
	// scoped and the sentinel at IsSacrificial is never touched.
	ns := `HKLM\SOFTWARE\PreflightTest\IntegrationTest`
	nsProvider := `Registry::HKEY_LOCAL_MACHINE\SOFTWARE\PreflightTest\IntegrationTest`

	// ---- Cleanup: remove the entire test namespace ----
	t.Cleanup(func() {
		_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
			`Remove-Item -LiteralPath "%s" -Recurse -Force -ErrorAction SilentlyContinue`,
			nsProvider,
		))
		if err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	// ---- Step 1: Apply a DWORD value ----
	params := map[string]any{
		"path": ns,
		"values": []any{
			map[string]any{
				"name": "TestDword",
				"type": "dword",
				"data": 42,
			},
		},
	}

	result, err := tgt.Execute(ctx, "registry-apply", "registry", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("registry apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("registry apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 2: Verify via independent oracle ----
	got := readRegistryOracle(t, tgt, nsProvider, "TestDword")
	if got != "42" {
		t.Fatalf("independent oracle: expected 42, got %q", got)
	}

	// ---- Step 3: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "registry-recheck", "registry", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("registry re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("registry re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 4: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "registry-reapply", "registry", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("registry re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("registry re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Apply a string value to the same key, verify ----
	params = map[string]any{
		"path": ns,
		"values": []any{
			map[string]any{
				"name": "TestDword",
				"type": "dword",
				"data": 42,
			},
			map[string]any{
				"name": "TestString",
				"type": "string",
				"data": "hello-preflight",
			},
		},
	}

	result, err = tgt.Execute(ctx, "registry-apply-string", "registry", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("registry apply (string): %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("registry apply (string): expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	gotStr := readRegistryOracle(t, tgt, nsProvider, "TestString")
	if gotStr != "hello-preflight" {
		t.Fatalf("independent oracle (string): expected 'hello-preflight', got %q", gotStr)
	}

	// ---- Step 6: Remove a value ----
	params = map[string]any{
		"path": ns,
		"values": []any{
			map[string]any{
				"name":   "TestString",
				"ensure": "absent",
			},
		},
	}

	result, err = tgt.Execute(ctx, "registry-remove-value", "registry", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("registry remove value: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("registry remove value: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	oracleOut := readRegistryOracle(t, tgt, nsProvider, "TestString")
	if oracleOut != "" {
		t.Fatalf("independent oracle: expected TestString to be absent, got %q", oracleOut)
	}

	// ---- Step 7: Ensure absent removes the entire key ----
	absentParams := map[string]any{
		"path":   ns,
		"ensure": "absent",
	}

	result, err = tgt.Execute(ctx, "registry-absent", "registry", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("registry absent: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("registry absent: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// Verify via oracle that path no longer exists
	absentOracle := readRegistryOracle(t, tgt, nsProvider, "TestDword")
	if absentOracle != "missing" {
		t.Fatalf("independent oracle: expected path to be absent, oracle reports %q", absentOracle)
	}
}

// assertSacrificialSentinel checks that the target has the sacrificial sentinel
// at HKLM\SOFTWARE\PreflightTest\IsSacrificial (DWORD=1). Without this marker
// the test refuses to mutate the target, preventing accidental changes to a
// non-sacrificial machine.
func assertSacrificialSentinel(t *testing.T, tgt *WinRMTarget) {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, `
$path = 'Registry::HKEY_LOCAL_MACHINE\SOFTWARE\PreflightTest'
$props = Get-ItemProperty -LiteralPath $path -Name IsSacrificial -ErrorAction SilentlyContinue
if ($null -eq $props -or $null -eq $props.IsSacrificial) {
  Write-Output 'absent'
  exit 0
}
if ($props.IsSacrificial -eq 1) { Write-Output 'present'; exit 0 }
Write-Output ('unexpected:' + $props.IsSacrificial)
`)
	if err != nil {
		t.Fatalf("sentinel check failed: %v — cannot proceed", err)
	}
	out = strings.TrimSpace(out)
	if out != "present" {
		t.Skipf("sacrificial sentinel not found on target (response: %q). "+
			"Run scripts/bootstrap-winrm-vm.ps1 on the VM, or point PREFLIGHT_TEST_WINRM "+
			"at a properly bootstrapped VM.", out)
	}
}

// TestWinRMIntegration_FirewallRule exercises the firewall_rule module against a real
// Windows endpoint over WinRM. It creates, checks, updates, and removes firewall
// rules using the pf-test-* naming convention and verifies correctness via an
// independent Get-NetFirewallRule oracle.
//
// Gated by PREFLIGHT_TEST_WINRM and the sacrificial sentinel.
func TestWinRMIntegration_FirewallRule(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM is not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	// All test rules use the pf-test-* naming convention so they are easy to
	// identify and never conflict with real rules.
	ruleName := "pf-test-integration-tcp443"

	// ---- Cleanup: remove all pf-test-* rules ----
	t.Cleanup(func() {
		_, err := tgt.RunPowerShell(ctx, `Get-NetFirewallRule -DisplayName "pf-test-*" -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue`)
		if err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	// ---- Step 1: Apply a TCP allow inbound rule on port 443 ----
	params := map[string]any{
		"name":      ruleName,
		"direction": "inbound",
		"action":    "allow",
		"protocol":  "tcp",
		"ports":     "443",
	}

	result, err := tgt.Execute(ctx, "fwrule-apply", "firewall_rule", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("firewall_rule apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("firewall_rule apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 2: Verify via independent oracle ----
	r := readFirewallRuleOracle(t, tgt, ruleName)
	if r.Direction != "Inbound" {
		t.Fatalf("independent oracle: expected Direction=Inbound, got %q", r.Direction)
	}
	if r.Action != "Allow" {
		t.Fatalf("independent oracle: expected Action=Allow, got %q", r.Action)
	}
	if r.Protocol != "TCP" {
		t.Fatalf("independent oracle: expected Protocol=TCP, got %q", r.Protocol)
	}
	if r.LocalPort != "443" {
		t.Fatalf("independent oracle: expected LocalPort=443, got %q", r.LocalPort)
	}

	// ---- Step 3: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "fwrule-recheck", "firewall_rule", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("firewall_rule re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("firewall_rule re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 4: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "fwrule-reapply", "firewall_rule", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("firewall_rule re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("firewall_rule re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Update the rule — change port from 443 to 8443 ----
	updateParams := map[string]any{
		"name":      ruleName,
		"direction": "inbound",
		"action":    "allow",
		"protocol":  "tcp",
		"ports":     "8443",
	}

	result, err = tgt.Execute(ctx, "fwrule-update", "firewall_rule", updateParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("firewall_rule update: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("firewall_rule update: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// Verify via oracle
	r = readFirewallRuleOracle(t, tgt, ruleName)
	if r.LocalPort != "8443" {
		t.Fatalf("independent oracle after update: expected LocalPort=8443, got %q", r.LocalPort)
	}

	// ---- Step 6: Ensure absent removes the rule ----
	absentParams := map[string]any{
		"name":   ruleName,
		"ensure": "absent",
	}

	result, err = tgt.Execute(ctx, "fwrule-absent", "firewall_rule", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("firewall_rule absent: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("firewall_rule absent: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 7: Verify oracle confirms absence ----
	r = readFirewallRuleOracle(t, tgt, ruleName)
	if r.Present {
		t.Fatalf("independent oracle after absent: expected rule to be removed, but it still exists")
	}

	// ---- Step 8: Ensure absent is idempotent ----
	result, err = tgt.Execute(ctx, "fwrule-absent-idempotent", "firewall_rule", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("firewall_rule absent idempotent: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("firewall_rule absent idempotent: expected StatusOK, got %q: %s", result.Status, result.Message)
	}
}

// firewallRuleOracleResult holds the fields read by the independent
// Get-NetFirewallRule oracle.
type firewallRuleOracleResult struct {
	Present   bool
	Direction string
	Action    string
	Protocol  string
	LocalPort string
}

// readFirewallRuleOracle is an independent PowerShell oracle that reads a
// firewall rule's properties directly via Get-NetFirewallRule, without using
// the module's Check or Apply scripts. This provides a truthful assertion
// source independent of the module implementation.
func readFirewallRuleOracle(t *testing.T, tgt *WinRMTarget, ruleName string) firewallRuleOracleResult {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$name = "%s"
$rule = Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue | Select-Object -First 1
if ($null -eq $rule) {
  Write-Output "absent|||"
  exit 0
}
$portFilter = $rule | Get-NetFirewallPortFilter
$fields = @(
  $rule.Direction,
  $rule.Action,
  $portFilter.Protocol,
  [string]$portFilter.LocalPort
)
Write-Output ("present|" + ($fields -join "|"))
`, ruleName))
	if err != nil {
		t.Fatalf("firewall rule oracle script failed: %v", err)
	}

	parts := strings.SplitN(strings.TrimSpace(out), "|", 5)
	if len(parts) < 1 {
		t.Fatalf("firewall rule oracle: unexpected output format: %q", out)
	}

	result := firewallRuleOracleResult{
		Present: parts[0] == "present",
	}
	if result.Present && len(parts) >= 5 {
		result.Direction = parts[1]
		result.Action = parts[2]
		result.Protocol = parts[3]
		result.LocalPort = parts[4]
	}
	return result
}

// readRegistryOracle is an independent PowerShell oracle that reads a registry
// value's data via Get-ItemProperty. It is written separately from the module's
// Check script to serve as a truthful assertion source.
//
// Returns the value data as a string, or "missing" when the path does not
// exist, or an empty string when the path exists but the named value does not.
//
// The provider path is injected into a PowerShell double-quoted string.
// PowerShell treats backslash as a literal character inside double quotes,
// so no additional escaping is needed.
func readRegistryOracle(t *testing.T, tgt *WinRMTarget, providerPath, valueName string) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$path = "%s"
$name = "%s"
if (-not (Test-Path -LiteralPath $path)) {
  Write-Output 'missing'
  exit 0
}
$props = Get-ItemProperty -LiteralPath $path -ErrorAction SilentlyContinue
if ($null -eq $props) {
  Write-Output 'missing'
  exit 0
}
$prop = $props.PSObject.Properties[$name]
if ($null -eq $prop) {
  Write-Output ''
  exit 0
}
$value = $prop.Value
if ($null -eq $value) {
  Write-Output ''
  exit 0
}
if ($value -is [byte[]]) {
  Write-Output ($value | ForEach-Object { $_ })
  exit 0
}
Write-Output ([string]$value)
`, providerPath, valueName))
	if err != nil {
		t.Fatalf("oracle script failed: %v", err)
	}
	return strings.TrimSpace(out)
}
