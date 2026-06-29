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

// TestWinRMIntegration_User exercises the user module against a real Windows
// endpoint over WinRM. It is gated by PREFLIGHT_TEST_WINRM and the sacrificial
// sentinel, so it is inert on CI and on any dev machine without a configured VM.
//
// The test uses an independent PowerShell oracle (Get-LocalUser) to assert
// correctness rather than relying on the module's own Check().
func TestWinRMIntegration_User(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM is not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	// The sacrificial namespace for user names follows pf-test-* so the test
	// never touches non-test users. A timestamp suffix ensures uniqueness
	// across parallel runs.
	testName := fmt.Sprintf("pf-test-int-%d", time.Now().UnixNano())

	// ---- Cleanup: remove the test user via raw PowerShell ----
	// Using a direct Remove-LocalUser command rather than the module under
	// test ensures cleanup is independent of module correctness. This follows
	// the pattern established by the registry test's cleanup.
	t.Cleanup(func() {
		_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
			`Remove-LocalUser -Name "%s" -ErrorAction SilentlyContinue`,
			testName,
		))
		if err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	// ---- Step 1: Create a user with password and group membership ----
	params := map[string]any{
		"name":     testName,
		"password": "PreflightTest123!",
		"groups":   []any{"Users"},
	}

	result, err := tgt.Execute(ctx, "user-apply", "user", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("user apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("user apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 2: Verify via independent oracle ----
	got := readUserOracle(t, tgt, testName)
	if got != "present" {
		t.Fatalf("independent oracle: expected 'present', got %q", got)
	}

	// ---- Step 3: Verify group membership via oracle ----
	if !readUserGroupOracle(t, tgt, testName, "Users") {
		t.Fatal("independent oracle: expected user to be member of Users group")
	}

	// ---- Step 4: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "user-recheck", "user", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("user re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("user re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "user-reapply", "user", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("user re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("user re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 6: Ensure absent removes the user ----
	absentParams := map[string]any{
		"name":   testName,
		"ensure": "absent",
	}

	result, err = tgt.Execute(ctx, "user-absent", "user", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("user absent: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("user absent: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 7: Verify via oracle that user is absent ----
	got = readUserOracle(t, tgt, testName)
	if got != "absent" {
		t.Fatalf("independent oracle: expected 'absent', got %q", got)
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

// readUserOracle is an independent PowerShell oracle that checks whether a
// local user exists via Get-LocalUser. It is written separately from the
// module's Check script to serve as a truthful assertion source.
//
// Returns "present" when the user exists, or "absent" when the user does not.
func readUserOracle(t *testing.T, tgt *WinRMTarget, username string) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$name = "%s"
$user = Get-LocalUser -Name $name -ErrorAction SilentlyContinue
if ($null -eq $user) { Write-Output 'absent'; exit 0 }
Write-Output 'present'
`, username))
	if err != nil {
		t.Fatalf("user oracle script failed: %v", err)
	}
	return strings.TrimSpace(out)
}

// readUserGroupOracle is an independent PowerShell oracle that checks whether
// a user is a member of a local group via Get-LocalGroupMember. It is written
// separately from the module's Check script to serve as a truthful assertion
// source.
//
// Returns true when the user is a member of the group, false otherwise.
func readUserGroupOracle(t *testing.T, tgt *WinRMTarget, username, group string) bool {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$name = "%s"
$group = "%s"
$user = Get-LocalUser -Name $name -ErrorAction SilentlyContinue
if ($null -eq $user) { Write-Output 'false'; exit 0 }
$members = Get-LocalGroupMember -Group $group -ErrorAction SilentlyContinue
$member = $members | Where-Object { $_.Name -match ("(^|\\\\)" + [regex]::Escape($name) + "$") }
if ($null -eq $member) { Write-Output 'false'; exit 0 }
Write-Output 'true'
`, username, group))
	if err != nil {
		t.Fatalf("user group oracle script failed: %v", err)
	}
	return strings.TrimSpace(out) == "true"
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

// TestWinRMIntegration_PowerPlan exercises the power_plan module against a
// real Windows endpoint over WinRM. Since power_plan mutates global machine
// state (the active power scheme), the test captures the original active
// scheme before making changes and restores it in t.Cleanup.
//
// The test uses independent powercfg /list and powercfg /getactivescheme
// calls as oracles rather than relying on the module's own Check().
func TestWinRMIntegration_PowerPlan(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM is not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	testName := "preflight-integration-test"

	// ---- Capture the original active scheme so we can restore it later ----
	origGUID := getActivePowerSchemeOracle(t, tgt)
	t.Logf("original active scheme GUID: %s", origGUID)

	// ---- Cleanup: restore original active scheme and delete test scheme ----
	t.Cleanup(func() {
		// Restore the original active scheme first (important: do this
		// before trying to delete the test scheme, in case the test scheme
		// is currently active).
		_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
			`& powercfg.exe /setactive "%s" 2>&1 | Out-Null`, origGUID,
		))
		if err != nil {
			t.Logf("cleanup restore active scheme: %v", err)
		}
		// Now delete the test scheme if it still exists.
		_, err = tgt.RunPowerShell(ctx, fmt.Sprintf(`
$targetName = '%s'
foreach ($line in (& powercfg.exe /list 2>&1)) {
  if ($line -match 'Power Scheme GUID:\s*([A-Fa-f0-9-]{36})\s+\((.+?)\)') {
    if ($matches[2] -eq $targetName) {
      & powercfg.exe /delete $matches[1] 2>&1 | Out-Null
    }
  }
}
`, testName))
		if err != nil {
			t.Logf("cleanup delete test scheme: %v", err)
		}
	})

	// ---- Step 1: Create a new power scheme and activate it ----
	params := map[string]any{
		"name":     testName,
		"ensure":   "present",
		"activate": true,
		"base":     "balanced",
	}

	result, err := tgt.Execute(ctx, "powerplan-apply", "power_plan", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("power_plan apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("power_plan apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 2: Verify via independent oracle ----
	testGUID := getNamedSchemeGUIDOracle(t, tgt, testName)
	if testGUID == "" {
		t.Fatal("independent oracle: test scheme not found via powercfg /list")
	}
	activeGUID := getActivePowerSchemeOracle(t, tgt)
	if activeGUID != testGUID {
		t.Fatalf("independent oracle: expected active scheme to be test scheme (%s), got %s", testGUID, activeGUID)
	}

	// ---- Step 3: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "powerplan-recheck", "power_plan", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("power_plan re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("power_plan re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 4: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "powerplan-reapply", "power_plan", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("power_plan re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("power_plan re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Ensure absent removes the scheme ----
	absentParams := map[string]any{
		"name":   testName,
		"ensure": "absent",
	}

	result, err = tgt.Execute(ctx, "powerplan-absent", "power_plan", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("power_plan absent: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("power_plan absent: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// Verify via oracle that the scheme no longer exists
	if guid := getNamedSchemeGUIDOracle(t, tgt, testName); guid != "" {
		t.Fatal("independent oracle: expected test scheme to be absent, but it still exists")
	}

	// ---- Step 6: Idempotency — ensure absent again is a no-op ----
	result, err = tgt.Execute(ctx, "powerplan-absent-idempotent", "power_plan", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("power_plan absent (idempotent): %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("power_plan absent (idempotent): expected StatusOK, got %q: %s", result.Status, result.Message)
	}
}

// TestWinRMIntegration_WindowsFeature exercises the windows_feature (DISM) module
// against a real Windows endpoint over WinRM. It uses TelnetClient as a
// lightweight feature — Enable/Disable-WindowsOptionalFeature on TelnetClient
// is fast, does not require reboot, and is available on virtually all Windows
// editions.
//
// Gated by PREFLIGHT_TEST_WINRM and the sacrificial sentinel.
//
// The test captures the original feature state before making changes and
// restores it in t.Cleanup. Correctness is asserted via an independent
// Get-WindowsOptionalFeature oracle rather than the module's own Check().
func TestWinRMIntegration_WindowsFeature(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM is not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	// TelnetClient is a well-known lightweight Windows optional feature that
	// is available on most editions and does not require a reboot or source
	// media when enabling/disabling.
	featureName := "TelnetClient"

	// ---- Capture the original feature state ----
	origState := readWindowsFeatureOracle(t, tgt, featureName)
	t.Logf("original state of %s: %s", featureName, origState)

	// ---- Cleanup: restore original feature state ----
	t.Cleanup(func() {
		switch origState {
		case "Enabled":
			_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
				`Enable-WindowsOptionalFeature -Online -FeatureName "%s" -NoRestart | Out-Null`,
				featureName,
			))
			if err != nil {
				t.Logf("cleanup enable %s: %v", featureName, err)
			}
		default: // "Disabled" or unknown — safe to disable
			_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
				`Disable-WindowsOptionalFeature -Online -FeatureName "%s" -NoRestart | Out-Null`,
				featureName,
			))
			if err != nil {
				t.Logf("cleanup disable %s: %v", featureName, err)
			}
		}
	})

	// Determine which direction counts as a change. If the feature is already
	// enabled we disable it; if it's disabled we enable it.
	targetEnsure := "present"
	if origState == "Enabled" {
		targetEnsure = "absent"
	}

	// ---- Step 1: Apply (toggle feature state) ----
	params := map[string]any{
		"name":   featureName,
		"ensure": targetEnsure,
	}

	result, err := tgt.Execute(ctx, "windows-feature-apply", "windows_feature", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("windows_feature apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("windows_feature apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 2: Verify via independent oracle ----
	gotState := readWindowsFeatureOracle(t, tgt, featureName)
	if targetEnsure == "present" && gotState != "Enabled" {
		t.Fatalf("independent oracle: expected feature to be Enabled, got %q", gotState)
	}
	if targetEnsure == "absent" && gotState != "Disabled" {
		t.Fatalf("independent oracle: expected feature to be Disabled, got %q", gotState)
	}

	// ---- Step 3: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "windows-feature-recheck", "windows_feature", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("windows_feature re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("windows_feature re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 4: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "windows-feature-reapply", "windows_feature", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("windows_feature re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("windows_feature re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Toggle back to the original state ----
	// If the feature was originally Enabled we need ensure=present to restore it.
	// If it was originally Disabled (or unknown) we need ensure=absent.
	originalEnsure := "absent"
	if origState == "Enabled" {
		originalEnsure = "present"
	}

	restoreParams := map[string]any{
		"name":   featureName,
		"ensure": originalEnsure,
	}

	result, err = tgt.Execute(ctx, "windows-feature-restore", "windows_feature", restoreParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("windows_feature restore: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("windows_feature restore: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 6: Verify via oracle ----
	gotState = readWindowsFeatureOracle(t, tgt, featureName)
	if originalEnsure == "present" && gotState != "Enabled" {
		t.Fatalf("independent oracle after restore: expected feature to be Enabled, got %q", gotState)
	}
	if originalEnsure == "absent" && gotState != "Disabled" {
		t.Fatalf("independent oracle after restore: expected feature to be Disabled, got %q", gotState)
	}

	// ---- Step 7: Idempotency — re-check on restored state ----
	result, err = tgt.Execute(ctx, "windows-feature-restore-recheck", "windows_feature", restoreParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("windows_feature restore re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("windows_feature restore re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 8: Idempotency — re-apply on restored state ----
	result, err = tgt.Execute(ctx, "windows-feature-restore-reapply", "windows_feature", restoreParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("windows_feature restore re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("windows_feature restore re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}
}

// readWindowsFeatureOracle is an independent PowerShell oracle that reads a
// Windows optional feature's state via Get-WindowsOptionalFeature, without
// using the module's Check or Apply scripts. Returns "Enabled", "Disabled",
// or an empty string if the feature is not found.
func readWindowsFeatureOracle(t *testing.T, tgt *WinRMTarget, featureName string) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$name = "%s"
$feature = Get-WindowsOptionalFeature -Online -FeatureName $name -ErrorAction SilentlyContinue
if ($null -eq $feature) {
  Write-Output "missing"
  exit 0
}
Write-Output $feature.State.ToString()
`, featureName))
	if err != nil {
		t.Fatalf("windows feature oracle script failed: %v", err)
	}
	return strings.TrimSpace(out)
}

// TestWinRMIntegration_WingetPackage exercises the winget_package module
// against a real Windows endpoint over WinRM. It installs a small test
// package via winget, asserts correctness via an independent winget list
// read, verifies idempotency, and removes the package in cleanup.
//
// The test uses 7zip.7zip from the winget source as the fixture because it
// is small (~1.5 MB), benign, stable, and available on all Windows editions
// that ship with winget. If winget is not available on the target the test
// skips gracefully.
func TestWinRMIntegration_WingetPackage(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM is not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	// The test package. 7-Zip is small, stable, and universally available
	// via the winget source on all Windows editions that ship with winget.
	pkgID := "7zip.7zip"
	pkgSource := "winget"

	// ---- Cleanup: ensure the test package is uninstalled ----
	t.Cleanup(func() {
		_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
			`& winget.exe uninstall --id "%s" --exact --disable-interactivity --accept-source-agreements 2>&1 | Out-Null`,
			pkgID,
		))
		if err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	// ---- Step 0: Guard — winget availability ----
	if !isWingetAvailable(ctx, tgt) {
		t.Skip("winget.exe not found on target; skipping winget_package integration test")
	}

	// ---- Step 0.5: Ensure the package is not already installed ----
	// (in case a previous test run was interrupted)
	_, _ = tgt.RunPowerShell(ctx, fmt.Sprintf(
		`& winget.exe uninstall --id "%s" --exact --disable-interactivity --accept-source-agreements 2>&1 | Out-Null`,
		pkgID,
	))

	// ---- Step 1: Verify package not installed via independent oracle ----
	if isWingetPackageInstalledOracle(t, tgt, pkgID) {
		t.Fatal("independent oracle: expected package to be absent before test")
	}

	// ---- Step 2: Apply the winget_package module to install ----
	params := map[string]any{
		"packages": []any{
			map[string]any{
				"id":     pkgID,
				"source": pkgSource,
			},
		},
	}

	result, err := tgt.Execute(ctx, "winget-apply", "winget_package", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("winget_package apply: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("winget_package apply: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 3: Verify via independent oracle ----
	if !isWingetPackageInstalledOracle(t, tgt, pkgID) {
		t.Fatal("independent oracle: expected package to be installed after apply")
	}

	// ---- Step 4: Idempotency — re-check says no change ----
	result, err = tgt.Execute(ctx, "winget-recheck", "winget_package", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("winget_package re-check: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("winget_package re-check: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 5: Idempotency — re-apply is a no-op ----
	result, err = tgt.Execute(ctx, "winget-reapply", "winget_package", params, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("winget_package re-apply: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("winget_package re-apply: expected StatusOK (idempotent), got %q: %s", result.Status, result.Message)
	}

	// ---- Step 6: Ensure absent removes the package ----
	absentParams := map[string]any{
		"packages": []any{
			map[string]any{
				"id":     pkgID,
				"ensure": "absent",
			},
		},
	}

	result, err = tgt.Execute(ctx, "winget-absent", "winget_package", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("winget_package absent: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("winget_package absent: expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// ---- Step 7: Verify oracle confirms absence ----
	if isWingetPackageInstalledOracle(t, tgt, pkgID) {
		t.Fatal("independent oracle: expected package to be absent after removal")
	}

	// ---- Step 8: Ensure absent is idempotent ----
	result, err = tgt.Execute(ctx, "winget-absent-idempotent", "winget_package", absentParams, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("winget_package absent idempotent: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("winget_package absent idempotent: expected StatusOK, got %q: %s", result.Status, result.Message)
	}
}

// isWingetAvailable checks whether winget.exe is available on the remote target.
func isWingetAvailable(ctx context.Context, tgt *WinRMTarget) bool {
	out, err := tgt.RunPowerShell(ctx,
		`if (Get-Command winget.exe -ErrorAction SilentlyContinue) { Write-Output 'true' } else { Write-Output 'false' }`)
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "true"
}

// isWingetPackageInstalledOracle is an independent PowerShell oracle that runs
// winget list for the given package ID and returns true if the package is
// currently installed. This is a truthful assertion source separate from the
// module's own Check/Apply logic.
func isWingetPackageInstalledOracle(t *testing.T, tgt *WinRMTarget, pkgID string) bool {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
		`$result = & winget.exe list --id "%s" --exact --accept-source-agreements --disable-interactivity 2>&1; if ($LASTEXITCODE -eq 0) { Write-Output 'present' } else { Write-Output 'absent' }`,
		pkgID,
	))
	if err != nil {
		t.Fatalf("winget oracle script failed: %v", err)
	}
	return strings.TrimSpace(out) == "present"
}

// getActivePowerSchemeOracle runs powercfg /getactivescheme and returns
// the GUID of the currently active power scheme. This is an independent
// oracle separate from the module's own check logic.
func getActivePowerSchemeOracle(t *testing.T, tgt *WinRMTarget) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, `
$line = & powercfg.exe /getactivescheme 2>&1
if ($line -match 'Power Scheme GUID:\s*([A-Fa-f0-9-]{36})') {
  Write-Output $matches[1]
  exit 0
}
Write-Output ''
`)
	if err != nil {
		t.Fatalf("getActivePowerSchemeOracle failed: %v", err)
	}
	return strings.TrimSpace(out)
}

// getNamedSchemeGUIDOracle returns the GUID of the first power scheme whose
// name matches the given name, or an empty string if no match is found. Uses
// powercfg /list as an independent oracle.
func getNamedSchemeGUIDOracle(t *testing.T, tgt *WinRMTarget, name string) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$targetName = '%s'
foreach ($line in (& powercfg.exe /list 2>&1)) {
  if ($line -match 'Power Scheme GUID:\s*([A-Fa-f0-9-]{36})\s+\((.+?)\)') {
    if ($matches[2] -eq $targetName) {
      Write-Output $matches[1]
      exit 0
    }
  }
}
Write-Output ''
`, name))
	if err != nil {
		t.Fatalf("getNamedSchemeGUIDOracle failed: %v", err)
	}
	return strings.TrimSpace(out)
}

// TestWinRMIntegration_Streaming exercises the output streaming path over a
// live WinRM connection. It runs a multi-line PowerShell command through the
// powershell module and asserts that output arrives incrementally through the
// onOutput callback rather than being batched until the command completes.
//
// The test uses a goroutine + buffered channel to observe interleaving: the
// script sleeps 100ms between lines, so the first line should reach the
// channel ~100ms into a ~500ms execution. A select with a 200ms timeout on
// channel read (before the Execute goroutine finishes) proves streaming. If
// batch fallback occurs, all output arrives after ~500ms and the 200ms
// timeout fires.
//
// Gated by PREFLIGHT_TEST_WINRM and the sacrificial sentinel.
func TestWinRMIntegration_Streaming(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM is not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	const script = `$lines = @('chunk-one','chunk-two','chunk-three','chunk-four','chunk-five')
foreach ($l in $lines) { Write-Output $l; Start-Sleep -Milliseconds 100 }
Write-Output 'done'`

	// Run Execute in a goroutine so we can observe whether onOutput fires
	// during execution (streaming) or only after it finishes (batch).
	ctx := context.Background()
	ch := make(chan string, 6)
	done := make(chan struct{})
	var result Result
	var execErr error

	go func() {
		result, execErr = tgt.Execute(ctx, "streaming-test", "powershell", map[string]any{
			"check_script": "return $true",
			"script":       script,
		}, ExecutionOptions{}, false, func(line string) {
			ch <- line
		})
		close(done)
	}()

	// Assert the first line arrives well before the script finishes (~500ms).
	// The script sleeps 100ms before each Write-Output, so the first line
	// hits the channel at ~100ms. If streaming fell back to batch, no data
	// arrives until done fires at ~500ms.
	select {
	case first := <-ch:
		if first != "chunk-one" {
			t.Fatalf("first line via onOutput: got %q, want %q", first, "chunk-one")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no output received within 200ms — output may be batched, not streamed")
	}

	// Collect remaining lines.
	gotLines := []string{"chunk-one"}
	for i := range 5 {
		select {
		case line := <-ch:
			gotLines = append(gotLines, line)
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for line %d of output", i+2)
		}
	}

	<-done // wait for Execute to complete

	if execErr != nil {
		t.Fatalf("Execute returned error: %v", execErr)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// Both onOutput and result.Output should carry the full script output.
	want := []string{"chunk-one", "chunk-two", "chunk-three", "chunk-four", "chunk-five", "done"}
	for i := range want {
		if gotLines[i] != want[i] {
			t.Fatalf("onOutput line %d: got %q, want %q", i, gotLines[i], want[i])
		}
	}

	if len(result.Output) < len(want) {
		t.Fatalf("result.Output has %d entries, want at least %d: %v", len(result.Output), len(want), result.Output)
	}
	for i := range want {
		if result.Output[i] != want[i] {
			t.Fatalf("result.Output[%d]: got %q, want %q", i, result.Output[i], want[i])
		}
	}
}
