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
		"Invoke-Winget -Arguments $args",
	} {
		if !strings.Contains(WingetPackageApplyScript, fragment) {
			t.Fatalf("expected winget apply script to contain %q, got:\n%s", fragment, WingetPackageApplyScript)
		}
	}
}

func TestWingetPackageScriptsInvokeWingetWithArgumentArray(t *testing.T) {
	for name, script := range map[string]string{
		"check": WingetPackageCheckScript,
		"apply": WingetPackageApplyScript,
	} {
		for _, fragment := range []string{
			"function Invoke-Winget",
			"& $wingetExe @Arguments 2>&1",
		} {
			if !strings.Contains(script, fragment) {
				t.Fatalf("expected %s winget script to contain %q, got:\n%s", name, fragment, script)
			}
		}
		if strings.Contains(script, "Start-Process -FilePath 'winget.exe'") {
			t.Fatalf("expected %s winget script to preserve argument arrays without Start-Process, got:\n%s", name, script)
		}
	}
}

func TestWingetPackageScriptsUseListFallbackForCurrentInstalls(t *testing.T) {
	for name, script := range map[string]string{
		"check": WingetPackageCheckScript,
		"apply": WingetPackageApplyScript,
	} {
		for _, fragment := range []string{
			"function Test-WingetPackageListedCurrent",
			"@('list', '--id', $Id, '--exact', '--accept-source-agreements', '--disable-interactivity')",
			"return ($stdout -notmatch '(?m)^\\s*Name\\s+Id\\s+Version\\s+Available\\s+Source\\s*$')",
			"function Test-WingetDesiredPresent",
			"$match = $InstalledMap[$id]",
			"if ($null -eq $match)",
			"return (Test-WingetPackageListedCurrent -Id $id -Source $source)",
		} {
			if !strings.Contains(script, fragment) {
				t.Fatalf("expected %s winget script to contain %q, got:\n%s", name, fragment, script)
			}
		}
		for _, fragment := range []string{"function Test-WingetPackageListed {"} {
			if strings.Contains(script, fragment) {
				t.Fatalf("expected %s winget script not to contain %q, got:\n%s", name, fragment, script)
			}
		}
	}
}

func TestWingetPackageApplyTreatsUpdateNotApplicableAsSuccessfulNoop(t *testing.T) {
	for _, fragment := range []string{
		"$result.ExitCode -eq -1978335189",
		"Test-WingetDesiredPresent -Spec $spec -InstalledMap $installedMap",
		"No available upgrade found",
		"No newer package versions are available",
		"continue",
		"winget command failed for '$id'",
	} {
		if !strings.Contains(WingetPackageApplyScript, fragment) {
			t.Fatalf("expected winget apply script to contain %q, got:\n%s", fragment, WingetPackageApplyScript)
		}
	}
}

func TestWingetPackageScriptsPointAtBootstrapWhenWingetIsNotReady(t *testing.T) {
	for name, script := range map[string]string{
		"check": WingetPackageCheckScript,
		"apply": WingetPackageApplyScript,
	} {
		for _, fragment := range []string{
			`Join-Path $env:LOCALAPPDATA 'Microsoft\WindowsApps\winget.exe'`,
			"winget.exe was not found for $wingetUser. Add a task with 'uses: preflight/winget'",
			"$null -eq $doc.Sources -and $null -eq (Get-AppxPackage -Name Microsoft.Winget.Source)",
			"the winget source is not registered for $wingetUser",
		} {
			if !strings.Contains(script, fragment) {
				t.Fatalf("expected %s winget script to contain %q, got:\n%s", name, fragment, script)
			}
		}
		if strings.Contains(script, "Get-Command winget.exe -ErrorAction Stop") {
			t.Fatalf("expected %s winget script to resolve winget without a raw Get-Command failure", name)
		}
	}
}

func TestWingetPackageScriptsSkipNullExportEntries(t *testing.T) {
	for name, script := range map[string]string{
		"check": WingetPackageCheckScript,
		"apply": WingetPackageApplyScript,
	} {
		for _, fragment := range []string{
			"if (Test-Path -LiteralPath $tempPath)",
			"@($doc.Sources | Where-Object { $null -ne $_ })",
			"@($src.Packages | Where-Object { $_.PackageIdentifier })",
		} {
			if !strings.Contains(script, fragment) {
				t.Fatalf("expected %s winget script to contain %q, got:\n%s", name, fragment, script)
			}
		}
	}
}
