package pscript

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeAdapter treats $params.installed (id -> version) as manager state.
// Invoke-PmChange records each change and applies it, except for ids listed
// in $params.stuck, which it reports without changing.
const fakeAdapter = `
$fakeState = @{}
foreach ($p in $params.installed.PSObject.Properties) { $fakeState[$p.Name] = [string]$p.Value }
function Get-PmInstalled { return $fakeState.Clone() }
function Test-PmPresent {
  param($Spec, $Installed)
  if (-not $Installed.ContainsKey([string]$Spec.id)) { return $false }
  if ($Spec.version) { return $Installed[[string]$Spec.id] -eq [string]$Spec.version }
  return $true
}
function Invoke-PmChange {
  param($Spec, [string]$Ensure, $Installed)
  Write-Output "$Ensure $($Spec.id)"
  if (@($params.stuck) -contains $Spec.id) { return }
  if ($Ensure -eq 'absent') { $fakeState.Remove([string]$Spec.id) } else { $fakeState[[string]$Spec.id] = [string]$Spec.version }
}
`

func runManagerScript(t *testing.T, script string, params map[string]any) []string {
	t.Helper()
	out, err := execManagerScript(t, script, params)
	if err != nil {
		t.Fatalf("pwsh: %v\n%s", err, out)
	}
	return strings.Fields(strings.ReplaceAll(strings.TrimSpace(out), "\r", ""))
}

func execManagerScript(t *testing.T, script string, params map[string]any) (string, error) {
	t.Helper()
	return execManagerScriptEnv(t, script, params, nil)
}

func execManagerScriptEnv(t *testing.T, script string, params map[string]any, env []string) (string, error) {
	t.Helper()
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh not installed")
	}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	prelude := "$ErrorActionPreference = 'Stop'\n$params = @'\n" + string(data) + "\n'@ | ConvertFrom-Json\n"
	cmd := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-Command", prelude+script)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestManagerPackageEngine(t *testing.T) {
	installed := map[string]any{"a": "1.0", "b": "2.0"}
	tests := []struct {
		name      string
		packages  []any
		wantCheck string
		wantApply string
	}{
		{
			name:      "all satisfied",
			packages:  []any{map[string]any{"id": "a"}, map[string]any{"id": "b", "version": "2.0"}, map[string]any{"id": "c", "ensure": "absent"}},
			wantCheck: "false",
			wantApply: "",
		},
		{
			name:      "missing package",
			packages:  []any{map[string]any{"id": "a"}, map[string]any{"id": "c"}},
			wantCheck: "true",
			wantApply: "present c",
		},
		{
			name:      "version mismatch",
			packages:  []any{map[string]any{"id": "b", "version": "3.0"}},
			wantCheck: "true",
			wantApply: "present b",
		},
		{
			name:      "present package marked absent",
			packages:  []any{map[string]any{"id": "a", "ensure": "absent"}, map[string]any{"id": "c"}},
			wantCheck: "true",
			wantApply: "absent a present c",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]any{"packages": tc.packages, "installed": installed}
			check := runManagerScript(t, managerPackageSetup+fakeAdapter+managerPackageCheckTail, params)
			if got := strings.Join(check, " "); got != tc.wantCheck {
				t.Fatalf("check = %q, want %q", got, tc.wantCheck)
			}
			apply := runManagerScript(t, managerPackageSetup+fakeAdapter+managerPackageApplyTail, params)
			if got := strings.Join(apply, " "); got != tc.wantApply {
				t.Fatalf("apply = %q, want %q", got, tc.wantApply)
			}
		})
	}
}

func TestManagerPackageEngineFailsWhenChangeDoesNotConverge(t *testing.T) {
	out, err := execManagerScript(t, managerPackageSetup+fakeAdapter+managerPackageApplyTail, map[string]any{
		"packages":  []any{map[string]any{"id": "a", "ensure": "absent"}, map[string]any{"id": "c"}},
		"installed": map[string]any{"a": "1.0"},
		"stuck":     []any{"a"},
	})
	if err == nil {
		t.Fatalf("expected apply to fail, got:\n%s", out)
	}
	if !strings.Contains(out, "not in the desired state: a (absent)") {
		t.Fatalf("expected the stuck package in the error, got:\n%s", out)
	}
}

// The winget harness runs the real winget adapter with Get-Command,
// Get-AppxPackage and Invoke-Winget replaced, so tests control what "winget"
// returns. Invoke-Winget can't be a shell fake because winget exit codes such
// as -1978335189 don't survive a POSIX exit status.
//
// $params.winget supplies export / export_after (JSON documents; the second
// and later exports use export_after when set) and list / install / uninstall
// ({exit, output}). $params.wingetMissing hides winget.exe and
// $params.sourceRegistered makes Get-AppxPackage find the source.
const wingetHarnessPrelude = `
function Get-Command {
  [CmdletBinding()] param([string]$Name)
  if (-not $params.wingetMissing) { return [pscustomobject]@{ Source = 'winget-fake' } }
}
function Get-AppxPackage {
  [CmdletBinding()] param([string]$Name)
  if ($params.sourceRegistered) { return [pscustomobject]@{ Name = $Name } }
}
`

const wingetHarnessOverride = `
$script:exportCalls = 0
function Invoke-Winget {
  param([string[]]$Arguments)
  Write-Host ('CALL ' + ($Arguments -join ' '))
  $cmd = $Arguments[0]
  if ($cmd -eq 'export') {
    $script:exportCalls++
    $json = if ($script:exportCalls -gt 1 -and $params.winget.export_after) { $params.winget.export_after } else { $params.winget.export }
    Set-Content -LiteralPath $Arguments[$Arguments.IndexOf('--output') + 1] -Value $json
    return [pscustomobject]@{ ExitCode = 0; Details = [string[]]@() }
  }
  $r = $params.winget.$cmd
  [string[]]$details = @()
  if ($r.output) { $details = [string[]]@($r.output) }
  return [pscustomobject]@{ ExitCode = [int]$r.exit; Details = $details }
}
`

const (
	wingetUpdateNotApplicable = -1978335189
	wingetNoUpgradeOutput     = "No available upgrade found.\nNo newer package versions are available from the configured sources."
	wingetEmptyExport         = `{"Sources":[{"Packages":[]}]}`
	wingetHasA                = `{"Sources":[{"Packages":[{"PackageIdentifier":"a","Version":"1.0"}]}]}`
)

func execWingetHarness(t *testing.T, tail string, params map[string]any) (string, error) {
	t.Helper()
	script := managerPackageSetup + wingetHarnessPrelude + wingetAdapter + wingetHarnessOverride + tail
	// LOCALAPPDATA is emptied so the WindowsApps alias fallback never finds a real winget.
	return execManagerScriptEnv(t, script, params, []string{"TEMP=" + t.TempDir(), "LOCALAPPDATA="})
}

func TestWingetAdapterReadsExport(t *testing.T) {
	tests := []struct {
		name     string
		packages []any
		export   string
		want     string
	}{
		{
			name:     "installed",
			packages: []any{map[string]any{"id": "a"}},
			export:   wingetHasA,
			want:     "false",
		},
		{
			name:     "version must match exactly",
			packages: []any{map[string]any{"id": "a", "version": "1.0.0"}},
			export:   wingetHasA,
			want:     "true",
		},
		{
			name:     "null sources and packages are skipped",
			packages: []any{map[string]any{"id": "a"}},
			export:   `{"Sources":[null,{"Packages":[null,{"PackageIdentifier":"a","Version":"1.0"}]}]}`,
			want:     "false",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := execWingetHarness(t, managerPackageCheckTail, map[string]any{
				"packages": tc.packages,
				"winget":   map[string]any{"export": tc.export},
			})
			if err != nil {
				t.Fatalf("pwsh: %v\n%s", err, out)
			}
			if !strings.HasSuffix(strings.TrimSpace(out), tc.want) {
				t.Fatalf("check = %q, want %q", out, tc.want)
			}
		})
	}
}

func TestWingetAdapterListFallbackForCurrentInstalls(t *testing.T) {
	const current = "Name Id Version Source\n----\nA a 1.0 winget"
	const upgradable = "Name Id Version Available Source\n----\nA a 1.0 2.0 winget"
	tests := []struct {
		name     string
		spec     map[string]any
		list     map[string]any
		want     string
		wantList bool
	}{
		{"current install is present", map[string]any{"id": "a"}, map[string]any{"exit": 0, "output": current}, "false", true},
		{"upgradable install needs change", map[string]any{"id": "a"}, map[string]any{"exit": 0, "output": upgradable}, "true", true},
		{"failed list needs change", map[string]any{"id": "a"}, map[string]any{"exit": 1, "output": current}, "true", true},
		{"versioned entry skips list", map[string]any{"id": "a", "version": "1.0"}, map[string]any{"exit": 0, "output": current}, "true", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := execWingetHarness(t, managerPackageCheckTail, map[string]any{
				"packages": []any{tc.spec},
				"winget":   map[string]any{"export": wingetEmptyExport, "list": tc.list},
			})
			if err != nil {
				t.Fatalf("pwsh: %v\n%s", err, out)
			}
			if !strings.HasSuffix(strings.TrimSpace(out), tc.want) {
				t.Fatalf("check = %q, want %q", out, tc.want)
			}
			if got := strings.Contains(out, "CALL list --id a --exact"); got != tc.wantList {
				t.Fatalf("list called = %v, want %v\n%s", got, tc.wantList, out)
			}
		})
	}
}

func TestWingetAdapterPointsAtBootstrap(t *testing.T) {
	tests := []struct {
		name    string
		params  map[string]any
		wantErr string
	}{
		{
			// LOCALAPPDATA is empty in the harness, so this also covers the alias fallback.
			name:    "winget.exe missing",
			params:  map[string]any{"wingetMissing": true, "winget": map[string]any{"export": wingetEmptyExport}},
			wantErr: "Add a task with 'uses: preflight/winget' before this one",
		},
		{
			name:    "source not registered",
			params:  map[string]any{"winget": map[string]any{"export": "{}"}},
			wantErr: "the winget source is not registered for",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.params["packages"] = []any{map[string]any{"id": "a"}}
			out, err := execWingetHarness(t, managerPackageCheckTail, tc.params)
			if err == nil {
				t.Fatalf("expected failure, got:\n%s", out)
			}
			if !strings.Contains(out, tc.wantErr) || !strings.Contains(out, "preflight/winget") {
				t.Fatalf("expected %q in output, got:\n%s", tc.wantErr, out)
			}
		})
	}
}

func TestWingetAdapterUpdateNotApplicable(t *testing.T) {
	install := map[string]any{"exit": wingetUpdateNotApplicable, "output": wingetNoUpgradeOutput}
	packages := []any{map[string]any{"id": "a"}}

	t.Run("noop when the package is present afterwards", func(t *testing.T) {
		out, err := execWingetHarness(t, managerPackageApplyTail, map[string]any{
			"packages": packages,
			"winget":   map[string]any{"export": wingetEmptyExport, "export_after": wingetHasA, "install": install},
		})
		if err != nil {
			t.Fatalf("expected success, got: %v\n%s", err, out)
		}
		if !strings.Contains(out, "CALL install --id a") {
			t.Fatalf("expected an install attempt, got:\n%s", out)
		}
	})

	t.Run("other failures throw", func(t *testing.T) {
		out, err := execWingetHarness(t, managerPackageApplyTail, map[string]any{
			"packages": packages,
			"winget":   map[string]any{"export": wingetEmptyExport, "install": map[string]any{"exit": 1, "output": "boom"}},
		})
		if err == nil || !strings.Contains(out, "winget command failed for 'a' with exit code 1") {
			t.Fatalf("expected a command failure, got: %v\n%s", err, out)
		}
	})
}

// fakeWingetExe is a POSIX stand-in for winget.exe that logs every argument
// on its own line and reports package "a" as installed once install has run.
const fakeWingetExe = `#!/bin/sh
D="$FAKE_WINGET_DIR"
for a in "$@"; do printf '[%s]\n' "$a" >> "$D/calls.log"; done
echo -- >> "$D/calls.log"
case "$1" in
export)
  while [ $# -gt 0 ]; do
    if [ "$1" = "--output" ]; then out=$2; fi
    shift
  done
  if [ -f "$D/installed" ]; then
    echo '{"Sources":[{"Packages":[{"PackageIdentifier":"a","Version":"1.0"}]}]}' > "$out"
  else
    echo '{"Sources":[{"Packages":[]}]}' > "$out"
  fi
  ;;
install) touch "$D/installed" ;;
esac
exit 0
`

func TestWingetPackageApplyInvokesWingetWithArgumentArray(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell script standing in for winget.exe")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "winget.exe"), []byte(fakeWingetExe), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"FAKE_WINGET_DIR=" + dir,
		"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"TEMP=" + t.TempDir(),
		"LOCALAPPDATA=",
	}
	params := map[string]any{"packages": []any{map[string]any{
		"id": "a", "source": "winget", "version": "1.0", "args": []any{"--override", "/S /D=C:\\Program Files\\a"},
	}}}
	if out, err := execManagerScriptEnv(t, WingetPackageApplyScript, params, env); err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls.log"))
	if err != nil {
		t.Fatal(err)
	}
	// Each argument stays one argument, and extra args follow the generated ones.
	for _, want := range []string{"[install]\n[--id]\n[a]", "[--version]\n[1.0]\n[--source]\n[winget]\n[--override]\n[/S /D=C:\\Program Files\\a]\n--"} {
		if !strings.Contains(strings.ReplaceAll(string(calls), "\r", ""), want) {
			t.Fatalf("expected %q in winget calls, got:\n%s", want, calls)
		}
	}
	// The exported check script sees the install, so a following check passes.
	out, err := execManagerScriptEnv(t, WingetPackageCheckScript, params, env)
	if err != nil || strings.TrimSpace(out) != "false" {
		t.Fatalf("check after apply = %q, %v", out, err)
	}
}
func TestManagerPackageScriptsParse(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh not installed")
	}
	for name, script := range map[string]string{
		"winget check": WingetPackageCheckScript,
		"winget apply": WingetPackageApplyScript,
	} {
		cmd := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-Command",
			"$errs = $null; [void][System.Management.Automation.Language.Parser]::ParseInput([Console]::In.ReadToEnd(), [ref]$null, [ref]$errs); $errs | ForEach-Object { $_.Message }")
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "" {
			t.Fatalf("%s script does not parse: %v\n%s", name, err, out)
		}
	}
}

// The post-apply verification turns a "no upgrade" install that left the
// package missing into a failure, where it used to pass silently.
func TestWingetAdapterUpdateNotApplicableFailsVerification(t *testing.T) {
	out, err := execWingetHarness(t, managerPackageApplyTail, map[string]any{
		"packages": []any{map[string]any{"id": "a"}},
		"winget": map[string]any{
			"export":  wingetEmptyExport,
			"install": map[string]any{"exit": wingetUpdateNotApplicable, "output": wingetNoUpgradeOutput},
		},
	})
	if err == nil {
		t.Fatalf("expected apply to fail, got:\n%s", out)
	}
	if !strings.Contains(out, "not in the desired state: a (present)") {
		t.Fatalf("expected the verification error, got:\n%s", out)
	}
}
