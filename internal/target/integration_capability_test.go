package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// isWinRMServicingUnsupported reports whether a windows_feature (DISM) error is
// the well-known component-store symlink restriction that surfaces when online
// servicing runs under a basic WinRM network-logon token. The operation works
// under CredSSP or an interactive logon, so callers treat this as an
// environment limitation and skip rather than fail.
func isWinRMServicingUnsupported(msg string) bool {
	return strings.Contains(strings.ToLower(msg), "symbolic link cannot be followed")
}

// appxRemovalBlockedReason attempts a direct all-users removal of name and
// returns the captured error message when it is the non-interactive-session
// limitation (HRESULT 0x80073D19 / "a user was logged off"). It returns an
// empty string when removal is not blocked for that reason — in which case a
// still-present package after the module ran is a genuine defect, not an
// environment limitation.
func appxRemovalBlockedReason(t *testing.T, tgt PowerShellRunner, name string) string {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, fmt.Sprintf(`
$name = '%s'
$pkg = Get-AppxPackage -AllUsers -Name $name | Select-Object -First 1
if ($null -eq $pkg) { Write-Output ''; exit 0 }
try {
  Remove-AppxPackage -Package $pkg.PackageFullName -AllUsers -ErrorAction Stop
  Write-Output ''
} catch {
  Write-Output ('err: ' + $_.Exception.Message)
}
`, name))
	if err != nil {
		t.Fatalf("appx removal probe failed: %v", err)
	}
	msg := strings.TrimSpace(out)
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "0x80073d19") || strings.Contains(lower, "user was logged off") {
		return msg
	}
	return ""
}
