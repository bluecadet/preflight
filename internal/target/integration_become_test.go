//go:build integration

package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestIntegration_Become exercises become (runas) identity switching over both
// transports. It sets up a second throwaway local user, runs a powershell
// module task under that identity, and proves the task ran as the become user
// by reading an independent observable (a temp file written by the become task
// and read back as the connection user).
//
// Coverage (per transport):
//   - become:   powershell task runs with Become{enabled, user, password, method: runas}
//   - oracle:   independent reader asserts the temp-file identity is the become user
//   - cleanup:  become user removed, temp directory removed
//
// See ADR-0016.
func TestIntegration_Become(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()

		// ---- Setup: create a throwaway local user for become ----
		becomeUser := "pf-test-become-" + testRunID()[:12]
		becomePass := "PreflightBecome123!"

		_, err := runner.RunPowerShell(ctx, fmt.Sprintf(`
$password = ConvertTo-SecureString "%s" -AsPlainText -Force
New-LocalUser -Name "%s" -Password $password -PasswordNeverExpires -ErrorAction Stop
`, becomePass, becomeUser))
		if err != nil {
			t.Fatalf("setup: failed to create become user %q: %v", becomeUser, err)
		}

		// ---- Cleanup: remove the become user ----
		t.Cleanup(func() {
			_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
				`Remove-LocalUser -Name "%s" -ErrorAction SilentlyContinue`,
				becomeUser,
			))
			if err != nil {
				t.Logf("cleanup: remove user %q: %v", becomeUser, err)
			}
		})

		// ---- Setup: create a world-writable temp directory ----
		// The become user must be able to write the identity observable here.
		// We grant Everyone write access via an ACL rule.
		nsDir := fmt.Sprintf(`C:\Windows\Temp\PreflightTest\BecomeTest-%s`, testRunID()[:12])
		identityFile := nsDir + `\identity.txt`

		_, err = runner.RunPowerShell(ctx, fmt.Sprintf(`
$dir = "%s"
New-Item -Path $dir -ItemType Directory -Force -ErrorAction Stop | Out-Null
$acl = Get-Acl -LiteralPath $dir
$rule = New-Object System.Security.AccessControl.FileSystemAccessRule("Everyone", "Write", "ContainerInherit,ObjectInherit", "None", "Allow")
$acl.AddAccessRule($rule)
Set-Acl -LiteralPath $dir -AclObject $acl -ErrorAction Stop
`, nsDir))
		if err != nil {
			t.Fatalf("setup: failed to create temp directory %q: %v", nsDir, err)
		}

		// ---- Cleanup: remove the temp directory ----
		t.Cleanup(func() {
			_, err := runner.RunPowerShell(ctx, fmt.Sprintf(
				`Remove-Item -LiteralPath "%s" -Recurse -Force -ErrorAction SilentlyContinue`,
				nsDir,
			))
			if err != nil {
				t.Logf("cleanup: remove temp dir %q: %v", nsDir, err)
			}
		})

		// ================================================================
		// Branch: become — powershell module task with runas become
		// ================================================================
		// The check_script always reports a change is needed so Apply runs.
		// Apply writes $env:USERNAME (the identity of the become user) to a
		// world-writable temp file that the runner can read back.
		mustExecute(t, tgt, "become-identity", "powershell", map[string]any{
			"check_script": "return $true",
			"script": fmt.Sprintf(
				`$env:USERNAME | Out-File -FilePath "%s" -Encoding UTF8 -Force`,
				identityFile,
			),
		}, ExecutionOptions{
			Become: &BecomeOptions{
				Enabled:  true,
				User:     becomeUser,
				Password: becomePass,
				Method:   "runas",
			},
		}, false, StatusChanged)

		// ================================================================
		// Oracle: read the identity file as the connection user and assert
		// it matches the become user.
		// ================================================================
		got := readBecomeIdentityOracle(t, runner, identityFile)
		if got != becomeUser {
			t.Fatalf("independent oracle: expected identity %q, got %q (file=%q)",
				becomeUser, got, identityFile)
		}
	})
}

// readBecomeIdentityOracle reads the identity file written by the become task
// and returns its contents as a trimmed string. It is written independently
// of the module's Check script to serve as a truthful assertion source.
func readBecomeIdentityOracle(t *testing.T, runner PowerShellRunner, filePath string) string {
	t.Helper()
	ctx := context.Background()

	out, err := runner.RunPowerShell(ctx, fmt.Sprintf(`
$path = "%s"
if (-not (Test-Path -LiteralPath $path)) {
  Write-Output ''
  exit 0
}
(Get-Content -LiteralPath $path -Raw -ErrorAction Stop).Trim()
`, filePath))
	if err != nil {
		t.Fatalf("oracle: failed to read identity file %q: %v", filePath, err)
	}
	return strings.TrimSpace(out)
}
