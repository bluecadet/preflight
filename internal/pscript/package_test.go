package pscript

import (
	"strings"
	"testing"
)

func TestWingetPackageApplyScriptAppendsExtraArgs(t *testing.T) {
	for _, fragment := range []string{
		"$wingetArgs = @()",
		"foreach ($arg in $spec.args)",
		"$args += $wingetArgs",
		"Start-Process -FilePath 'winget.exe' -ArgumentList $args",
	} {
		if !strings.Contains(WingetPackageApplyScript, fragment) {
			t.Fatalf("expected winget apply script to contain %q, got:\n%s", fragment, WingetPackageApplyScript)
		}
	}
}
