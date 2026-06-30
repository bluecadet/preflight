//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// firewallRuleOracleResult holds the fields read by the independent
// Get-NetFirewallRule oracle.
type firewallRuleOracleResult struct {
	Present   bool
	Direction string
	Action    string
	Protocol  string
	LocalPort string
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
		t.Skip("PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS are not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	// All test rules use the pf-test-* naming convention so they are easy to
	// identify and never conflict with real rules. The run ID suffix prevents
	// collisions when multiple `go test` processes share the same VM.
	ruleName := fmt.Sprintf("pf-test-%s-tcp443", testRunID()[:12])

	// ---- Cleanup: remove this run's test rule by exact name ----
	t.Cleanup(func() {
		_, err := tgt.RunPowerShell(ctx, fmt.Sprintf(
			`Get-NetFirewallRule -DisplayName "%s" -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue`,
			ruleName,
		))
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

// readFirewallRuleOracle is an independent PowerShell oracle that reads a
// firewall rule's properties directly via Get-NetFirewallRule, without using
// the module's Check or Apply scripts. This provides a truthful assertion
// source independent of the module implementation.
func readFirewallRuleOracle(t *testing.T, tgt PowerShellRunner, ruleName string) firewallRuleOracleResult {
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
