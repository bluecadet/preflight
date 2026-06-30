package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

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
		t.Skip("PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS are not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	ctx := context.Background()

	// The namespace under which all test mutations happen. The run ID is
	// embedded in the key name (not as a sub-level) so concurrent `go test`
	// runs against the same VM don't collide. The key nests under
	// PreflightTest — one level deep — so the sentinel at IsSacrificial is
	// never touched and the module only creates one new key.
	runKey := "IntegrationTest-" + testRunID()[:12]
	ns := `HKLM\SOFTWARE\PreflightTest\` + runKey
	nsProvider := `Registry::HKEY_LOCAL_MACHINE\SOFTWARE\PreflightTest\` + runKey

	// ---- Cleanup: remove the per-run key ----
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
