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

// TestIntegration_FirewallRule exercises the firewall_rule module against a real
// Windows endpoint over every configured transport (WinRM and/or SSH-to-Windows).
// Each transport gets its own subtest and is independently skipped when its
// env vars are unset.
//
// Coverage template (per transport):
//   - present:     create rule, verify via oracle
//   - idempotent:  re-check and re-apply both return StatusOK
//   - dry-run:     check-only predicts Changed, oracle confirms no mutation
//   - drift/update: port change converges to desired state
//   - absent:      ensure rule is removed, idempotent re-check
func TestIntegration_FirewallRule(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()

		// All test rules use the pf-test-* naming convention so they are easy to
		// identify and never conflict with real rules. The run ID suffix prevents
		// collisions when multiple `go test` processes share the same VM.
		ruleName := fmt.Sprintf("pf-test-%s-tcp443", testRunID()[:12])

		// ---- Cleanup: remove this run's test rule by exact name ----
		// NOTE: forEachTransport already registers t.Cleanup to close tgt,
		// so we only remove the firewall rule here.
		t.Cleanup(func() {
			_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
				`Get-NetFirewallRule -DisplayName "%s" -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue`,
				ruleName,
			))
			if err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		// ================================================================
		// Branch: present — apply a TCP allow inbound rule on port 443
		// ================================================================
		desiredParams := map[string]any{
			"name":      ruleName,
			"direction": "inbound",
			"action":    "allow",
			"protocol":  "tcp",
			"ports":     "443",
		}

		mustExecute(t, tgt, "fwrule-present", "firewall_rule", desiredParams, ExecutionOptions{}, false, StatusChanged)

		// ---- Verify via independent oracle ----
		r := readFirewallRuleOracle(t, runner, ruleName)
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

		// ================================================================
		// Branch: idempotent — re-check returns OK, re-apply is a no-op
		// ================================================================
		mustExecute(t, tgt, "fwrule-idemp-check", "firewall_rule", desiredParams, ExecutionOptions{}, false, StatusOK)
		mustExecute(t, tgt, "fwrule-idemp-apply", "firewall_rule", desiredParams, ExecutionOptions{}, false, StatusOK)

		r = readFirewallRuleOracle(t, runner, ruleName)
		if r.LocalPort != "443" {
			t.Fatalf("independent oracle after idempotent: expected LocalPort=443, got %q", r.LocalPort)
		}

		// ================================================================
		// Branch: dry-run — check-only predicts Changed, oracle confirms
		// no mutation occurred
		// ================================================================
		dryRunParams := map[string]any{
			"name":      ruleName,
			"direction": "inbound",
			"action":    "allow",
			"protocol":  "tcp",
			"ports":     "8443", // different from current 443
		}
		mustExecute(t, tgt, "fwrule-dryrun", "firewall_rule", dryRunParams, ExecutionOptions{}, true, StatusChanged)
		// Oracle confirms the port is still 443 — the dry run must not mutate
		r = readFirewallRuleOracle(t, runner, ruleName)
		if r.LocalPort != "443" {
			t.Fatalf("independent oracle after dry-run: expected LocalPort=443 (no mutation), got %q", r.LocalPort)
		}

		// ================================================================
		// Branch: drift/update — change port from 443 to 8443
		// ================================================================
		updateParams := map[string]any{
			"name":      ruleName,
			"direction": "inbound",
			"action":    "allow",
			"protocol":  "tcp",
			"ports":     "8443",
		}
		mustExecute(t, tgt, "fwrule-update", "firewall_rule", updateParams, ExecutionOptions{}, false, StatusChanged)

		r = readFirewallRuleOracle(t, runner, ruleName)
		if r.LocalPort != "8443" {
			t.Fatalf("independent oracle after update: expected LocalPort=8443, got %q", r.LocalPort)
		}

		// ================================================================
		// Branch: absent — ensure absent removes the rule
		// ================================================================
		absentParams := map[string]any{
			"name":   ruleName,
			"ensure": "absent",
		}
		mustExecute(t, tgt, "fwrule-absent", "firewall_rule", absentParams, ExecutionOptions{}, false, StatusChanged)

		// ---- Verify oracle confirms absence ----
		r = readFirewallRuleOracle(t, runner, ruleName)
		if r.Present {
			t.Fatalf("independent oracle after absent: expected rule to be removed, but it still exists")
		}

		// ================================================================
		// Branch: absent idempotent — re-check returns OK
		// ================================================================
		mustExecute(t, tgt, "fwrule-absent-idemp", "firewall_rule", absentParams, ExecutionOptions{}, false, StatusOK)
	})
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
