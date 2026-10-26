//go:build integration

package target

import (
	"context"
	"strings"
	"testing"
)

// TestIntegration_RegistryUserProfile exercises registry.user and
// environment.user against a throwaway local account whose profile exists but
// is not loaded (signed out), and against the same profile with its hive
// already loaded (signed in).
//
// Coverage (per transport):
//   - signed out: the helper loads NTUSER.DAT, writes, and unloads the hive
//   - signed in:  the loaded hive is used in place and left loaded
//   - missing:    a user with no profile fails with a clear error
func TestIntegration_RegistryUserProfile(t *testing.T) {
	forEachTransport(t, func(t *testing.T, runner PowerShellRunner, tgt Target) {
		ctx := context.Background()
		user := "pf" + testRunID()[:10]

		ps := func(script string) string {
			t.Helper()
			out, err := runner.RunPowerShell(ctx, script)
			if err != nil {
				t.Fatalf("powershell: %v\n%s", err, script)
			}
			return strings.TrimSpace(out)
		}

		// Create the account and its profile directory without a logon.
		sid := ps(`
$pw = ConvertTo-SecureString ('Pf!' + [guid]::NewGuid().ToString('N')) -AsPlainText -Force
New-LocalUser -Name '` + user + `' -Password $pw | Out-Null
$sid = (New-Object System.Security.Principal.NTAccount('` + user + `')).Translate([System.Security.Principal.SecurityIdentifier]).Value
Add-Type -Namespace Pf -Name UserEnv -MemberDefinition '[System.Runtime.InteropServices.DllImport("userenv.dll", CharSet=System.Runtime.InteropServices.CharSet.Unicode)] public static extern int CreateProfile(string sid, string name, System.Text.StringBuilder path, uint size);'
$sb = New-Object System.Text.StringBuilder 260
$hr = [Pf.UserEnv]::CreateProfile($sid, '` + user + `', $sb, 260)
if ($hr -ne 0) { throw ('CreateProfile failed: 0x{0:X}' -f $hr) }
Write-Output $sid
`)
		t.Cleanup(func() {
			_, _ = runner.RunPowerShell(ctx, `
reg.exe unload 'HKU\`+sid+`' 2>&1 | Out-Null
Remove-LocalUser -Name '`+user+`' -ErrorAction SilentlyContinue
Get-CimInstance Win32_UserProfile | Where-Object { $_.SID -eq '`+sid+`' } | Remove-CimInstance -ErrorAction SilentlyContinue
`)
		})

		hiveLoaded := func(name string) bool {
			return ps(`Write-Output (Test-Path -LiteralPath 'Registry::HKEY_USERS\`+name+`')`) == "True"
		}
		readViaLoad := func(subKey, name string) string {
			// Independent oracle: load the hive ourselves when it is not already.
			return ps(`
$sid = '` + sid + `'
$root = 'Registry::HKEY_USERS\' + $sid
$mounted = $false
if (-not (Test-Path -LiteralPath $root)) {
  $p = (Get-ItemProperty -LiteralPath ('Registry::HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\' + $sid)).ProfileImagePath
  reg.exe load ('HKU\oracle-' + $sid) (Join-Path $p 'NTUSER.DAT') | Out-Null
  $root = 'Registry::HKEY_USERS\oracle-' + $sid
  $mounted = $true
}
try { Write-Output ([string](Get-ItemProperty -LiteralPath ($root + '\` + subKey + `') -Name '` + name + `' -ErrorAction SilentlyContinue).'` + name + `') }
finally { if ($mounted) { [gc]::Collect(); [gc]::WaitForPendingFinalizers(); reg.exe unload ('HKU\oracle-' + $sid) | Out-Null } }
`)
		}

		params := func(data any) map[string]any {
			return map[string]any{
				"path": `HKCU:\Software\PreflightTest`,
				"user": user,
				"values": []any{
					map[string]any{"name": "Marker", "type": "string", "data": data},
				},
			}
		}

		// ---- signed out: hive is loaded on demand and unloaded afterwards ----
		mustExecute(t, tgt, "profile-out-apply", "registry", params("signed-out"), false, StatusChanged)
		if hiveLoaded("preflight-" + sid) {
			t.Fatal("hive preflight-<SID> still loaded after apply")
		}
		if got := readViaLoad(`Software\PreflightTest`, "Marker"); got != "signed-out" {
			t.Fatalf("signed-out marker = %q", got)
		}
		mustExecute(t, tgt, "profile-out-idempotent", "registry", params("signed-out"), false, StatusOK)

		envParams := map[string]any{"name": "PF_PROFILE_TEST", "value": "1", "scope": "user", "user": user}
		mustExecute(t, tgt, "profile-env-apply", "environment", envParams, false, StatusChanged)
		mustExecute(t, tgt, "profile-env-idempotent", "environment", envParams, false, StatusOK)
		if got := readViaLoad("Environment", "PF_PROFILE_TEST"); got != "1" {
			t.Fatalf("environment value = %q", got)
		}

		// ---- signed in: an already-loaded hive is used and left loaded ----
		ps(`
$p = (Get-ItemProperty -LiteralPath ('Registry::HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\` + sid + `')).ProfileImagePath
reg.exe load 'HKU\` + sid + `' (Join-Path $p 'NTUSER.DAT') | Out-Null
`)
		mustExecute(t, tgt, "profile-in-apply", "registry", params("signed-in"), false, StatusChanged)
		if !hiveLoaded(sid) {
			t.Fatal("signed-in hive was unloaded by the helper")
		}
		if got := readViaLoad(`Software\PreflightTest`, "Marker"); got != "signed-in" {
			t.Fatalf("signed-in marker = %q", got)
		}

		// ---- missing profile ----
		ghost := "pf" + testRunID()[:10] + "x"
		ps(`New-LocalUser -Name '` + ghost + `' -NoPassword | Out-Null`)
		t.Cleanup(func() {
			_, _ = runner.RunPowerShell(ctx, `Remove-LocalUser -Name '`+ghost+`' -ErrorAction SilentlyContinue`)
		})
		missing := params("x")
		missing["user"] = ghost
		_, err := tgt.Execute(ctx, "profile-missing", "registry", missing, false, nil)
		if err == nil || !strings.Contains(err.Error(), "has no profile on this machine") {
			t.Fatalf("expected missing-profile error, got %v", err)
		}
	})
}
