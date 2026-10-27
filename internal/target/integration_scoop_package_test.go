//go:build integration

package target

import (
	"context"
	"strings"
	"testing"
)

// scoopCmdExpr resolves scoop.cmd the same way the scoop_package module does.
const scoopCmdExpr = `(Join-Path $(if ([Environment]::GetEnvironmentVariable('SCOOP', 'User')) { [Environment]::GetEnvironmentVariable('SCOOP', 'User') } else { Join-Path $env:USERPROFILE 'scoop' }) 'shims\scoop.cmd')`

// TestIntegration_ScoopPackage exercises the scoop_package module against a
// real Windows endpoint over every configured transport. Coverage per
// transport: present, idempotent, dry-run, side-by-side versions with an
// active switch, drift, absent.
//
// The fixture is jq from the main bucket. Pinned versions need main to be a
// git clone, which preflight/scoop sets up. The test skips when Scoop is not
// installed for the connecting account.
func TestIntegration_ScoopPackage(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()
		uninstall := `& ` + scoopCmdExpr + ` uninstall jq 2>&1 | Out-Null`

		if !isScoopAvailable(ctx, runner) {
			t.Skip("scoop.cmd not found for the connecting account; skipping scoop_package integration test")
		}
		t.Cleanup(func() {
			if _, err := runner.RunPowerShell(ctx, uninstall); err != nil {
				t.Logf("cleanup: %v", err)
			}
		})
		_, _ = runner.RunPowerShell(ctx, uninstall)
		if got := scoopCurrentVersionOracle(t, runner, "jq"); got != "" {
			t.Fatalf("independent oracle: expected jq to be absent before test, current is %q", got)
		}

		present := map[string]any{"packages": []any{map[string]any{"id": "jq"}}}
		absent := map[string]any{"packages": []any{map[string]any{"id": "jq", "ensure": "absent"}}}
		sideBySide := func(active string) map[string]any {
			var packages []any
			for _, v := range []string{"1.7.1", "1.8.1"} {
				packages = append(packages, map[string]any{"id": "jq", "version": v, "active": v == active})
			}
			return map[string]any{"packages": packages}
		}

		// present, then idempotent
		mustExecute(t, tgt, "scoop-present", "scoop_package", present, false, StatusChanged)
		if scoopCurrentVersionOracle(t, runner, "jq") == "" {
			t.Fatal("independent oracle: expected jq to be installed after apply")
		}
		mustExecute(t, tgt, "scoop-idemp-check", "scoop_package", present, true, StatusOK)
		mustExecute(t, tgt, "scoop-idemp-apply", "scoop_package", present, false, StatusOK)

		// dry-run must not mutate
		mustExecute(t, tgt, "scoop-dryrun", "scoop_package", absent, true, StatusChanged)
		if scoopCurrentVersionOracle(t, runner, "jq") == "" {
			t.Fatal("independent oracle: expected jq to remain installed after dry-run")
		}

		// side-by-side versions, then switch the active one
		mustExecute(t, tgt, "scoop-versions", "scoop_package", sideBySide("1.8.1"), false, StatusChanged)
		if got := scoopCurrentVersionOracle(t, runner, "jq"); got != "1.8.1" {
			t.Fatalf("independent oracle: expected jq 1.8.1 current, got %q", got)
		}
		mustExecute(t, tgt, "scoop-versions-idemp", "scoop_package", sideBySide("1.8.1"), false, StatusOK)
		mustExecute(t, tgt, "scoop-switch", "scoop_package", sideBySide("1.7.1"), false, StatusChanged)
		if got := scoopCurrentVersionOracle(t, runner, "jq"); got != "1.7.1" {
			t.Fatalf("independent oracle: expected jq 1.7.1 current after switch, got %q", got)
		}

		// drift: removed behind the module's back, converges back
		if _, err := runner.RunPowerShell(ctx, uninstall); err != nil {
			t.Fatalf("drift setup: uninstall failed: %v", err)
		}
		mustExecute(t, tgt, "scoop-drift-check", "scoop_package", present, true, StatusChanged)
		mustExecute(t, tgt, "scoop-drift-apply", "scoop_package", present, false, StatusChanged)

		// absent, then idempotent
		mustExecute(t, tgt, "scoop-absent", "scoop_package", absent, false, StatusChanged)
		if got := scoopCurrentVersionOracle(t, runner, "jq"); got != "" {
			t.Fatalf("independent oracle: expected jq to be absent after removal, current is %q", got)
		}
		mustExecute(t, tgt, "scoop-absent-idemp", "scoop_package", absent, false, StatusOK)
	})
}

func isScoopAvailable(ctx context.Context, tgt PowerShellRunner) bool {
	out, err := tgt.RunPowerShell(ctx, `if (Test-Path -LiteralPath `+scoopCmdExpr+`) { 'true' } else { 'false' }`)
	return err == nil && strings.TrimSpace(out) == "true"
}

// scoopCurrentVersionOracle returns the version app's current link points at,
// or "" when the app is not installed.
func scoopCurrentVersionOracle(t *testing.T, tgt PowerShellRunner, app string) string {
	t.Helper()
	out, err := tgt.RunPowerShell(context.Background(),
		`$link = Get-Item -LiteralPath (Join-Path (Split-Path (Split-Path `+scoopCmdExpr+`)) 'apps\`+app+`\current') -ErrorAction SilentlyContinue; if ($link) { Split-Path -Leaf @($link.Target)[0] }`)
	if err != nil {
		t.Fatalf("scoop oracle script failed: %v", err)
	}
	return strings.TrimSpace(out)
}
