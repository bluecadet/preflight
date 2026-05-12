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

func TestWingetPackageScriptsUseListFallback(t *testing.T) {
	for name, script := range map[string]string{
		"check": WingetPackageCheckScript,
		"apply": WingetPackageApplyScript,
	} {
		for _, fragment := range []string{
			"function Test-WingetPackageListed",
			"@('list', '--id', $Id, '--exact', '--accept-source-agreements', '--disable-interactivity')",
			"function Test-WingetDesiredPresent",
			"return (Test-WingetPackageListed -Id $id -Source $source)",
		} {
			if !strings.Contains(script, fragment) {
				t.Fatalf("expected %s winget script to contain %q, got:\n%s", name, fragment, script)
			}
		}
	}
}

func TestWingetPackageApplyTreatsUpdateNotApplicableAsSuccessfulNoop(t *testing.T) {
	for _, fragment := range []string{
		"$process.ExitCode -eq -1978335189",
		"Test-WingetDesiredPresent -Spec $spec -InstalledMap $installedMap",
		"continue",
		"winget command failed for '$id'",
	} {
		if !strings.Contains(WingetPackageApplyScript, fragment) {
			t.Fatalf("expected winget apply script to contain %q, got:\n%s", fragment, WingetPackageApplyScript)
		}
	}
}
