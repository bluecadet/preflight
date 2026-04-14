package target

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterzen/winrm"
)

type fakeWinRMClient struct {
	runPS  func(context.Context, string) (string, string, int, error)
	runCmd func(context.Context, string) (string, string, int, error)
}

func (f *fakeWinRMClient) RunPSWithContext(ctx context.Context, command string) (string, string, int, error) {
	if f.runPS == nil {
		return "", "", 0, nil
	}
	return f.runPS(ctx, command)
}

func (f *fakeWinRMClient) RunCmdWithContext(ctx context.Context, command string) (string, string, int, error) {
	if f.runCmd == nil {
		return "", "", 0, nil
	}
	return f.runCmd(ctx, command)
}

type fakeWinRMShellClient struct {
	fakeWinRMClient
	createShell func() (*winrm.Shell, error)
}

func (f *fakeWinRMShellClient) CreateShell() (*winrm.Shell, error) {
	if f.createShell == nil {
		return nil, nil
	}
	return f.createShell()
}

func TestWinRMTarget_ExecuteShell(t *testing.T) {
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			if !strings.Contains(command, "& $cmd @args") {
				t.Fatalf("expected shell apply script, got %q", command)
			}
			return "applied", "", 0, nil
		},
	}

	result, err := tgt.Execute(context.Background(), "task-1", "shell", map[string]any{
		"cmd":  "echo",
		"args": []any{"hello"},
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected StatusChanged, got %q", result.Status)
	}
	if result.Message != "applied" {
		t.Fatalf("expected apply output, got %q", result.Message)
	}
}

func TestWinRMTarget_ExecuteShellWithBecomeUser(t *testing.T) {
	var sawRunCmd bool
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			switch {
			case strings.Contains(command, "[IO.File]::WriteAllBytes($path,"):
			case strings.Contains(command, "[IO.File]::Open($path, [IO.FileMode]::Append"):
			case strings.Contains(command, "Remove-Item -LiteralPath $path -Force"):
			default:
				t.Fatalf("unexpected powershell command %q", command)
			}
			return "applied", "", 0, nil
		},
		runCmd: func(_ context.Context, command string) (string, string, int, error) {
			sawRunCmd = true
			if !strings.Contains(command, `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "C:\Windows\Temp\preflight\run-`) {
				t.Fatalf("unexpected runCmd command %q", command)
			}
			return "applied", "", 0, nil
		},
	}

	result, err := tgt.Execute(context.Background(), "task-1", "shell", map[string]any{
		"cmd":  "echo",
		"args": []any{"hello"},
	}, ExecutionOptions{
		Become: &BecomeOptions{
			Enabled:  true,
			User:     "kiosk",
			Password: "secret",
		},
	}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected StatusChanged, got %q", result.Status)
	}
	if !sawRunCmd {
		t.Fatalf("expected staged command execution for become user")
	}
}

func TestWinRMTarget_ExecuteShellWithBecomeSystem(t *testing.T) {
	var sawRunCmd bool
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			switch {
			case strings.Contains(command, "[IO.File]::WriteAllBytes($path,"):
			case strings.Contains(command, "[IO.File]::Open($path, [IO.FileMode]::Append"):
			case strings.Contains(command, "Remove-Item -LiteralPath $path -Force"):
			default:
				t.Fatalf("unexpected powershell command %q", command)
			}
			return "applied", "", 0, nil
		},
		runCmd: func(_ context.Context, command string) (string, string, int, error) {
			sawRunCmd = true
			if !strings.Contains(command, `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "C:\Windows\Temp\preflight\run-`) {
				t.Fatalf("unexpected runCmd command %q", command)
			}
			return "applied", "", 0, nil
		},
	}

	result, err := tgt.Execute(context.Background(), "task-1", "shell", map[string]any{
		"cmd":  "echo",
		"args": []any{"hello"},
	}, ExecutionOptions{
		Become: &BecomeOptions{
			Enabled: true,
			User:    "SYSTEM",
		},
	}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected StatusChanged, got %q", result.Status)
	}
	if !sawRunCmd {
		t.Fatalf("expected staged command execution for become system")
	}
}

func TestBuildWindowsCredentialRunnerContainsRunAsWrapper(t *testing.T) {
	script, err := buildWindowsCredentialRunner(`C:\Windows\Temp\preflight`, "Write-Output 'hi'", &BecomeOptions{
		User:     "kiosk",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("buildWindowsCredentialRunner returned error: %v", err)
	}
	if !strings.Contains(script, "Start-Process @start") {
		t.Fatalf("expected runas wrapper, got %q", script)
	}
	if !strings.Contains(script, "PSCredential") {
		t.Fatalf("expected PSCredential usage, got %q", script)
	}
}

func TestBuildWindowsSystemRunnerContainsScheduledTaskWrapper(t *testing.T) {
	script, err := buildWindowsSystemRunner(`C:\Windows\Temp\preflight`, "Write-Output 'hi'")
	if err != nil {
		t.Fatalf("buildWindowsSystemRunner returned error: %v", err)
	}
	if !strings.Contains(script, "New-ScheduledTaskPrincipal -UserId 'SYSTEM'") {
		t.Fatalf("expected SYSTEM scheduled task wrapper, got %q", script)
	}
	if !strings.Contains(script, "Register-ScheduledTask") {
		t.Fatalf("expected scheduled task registration, got %q", script)
	}
}

func TestScheduledTaskScriptsCreateFoldersAndUsePrincipals(t *testing.T) {
	if !strings.Contains(scheduledTaskApplyScript, "Ensure-TaskFolder $path") {
		t.Fatalf("expected scheduled task apply script to create folders, got %q", scheduledTaskApplyScript)
	}
	if !strings.Contains(scheduledTaskApplyScript, "Normalize-TaskFolderPathForCom") {
		t.Fatalf("expected scheduled task apply script to normalize COM paths, got %q", scheduledTaskApplyScript)
	}
	if strings.Contains(scheduledTaskApplyScript, "\\' + $segment + '\\'") {
		t.Fatalf("expected scheduled task apply script to avoid trailing backslashes in COM folder lookups, got %q", scheduledTaskApplyScript)
	}
	if !strings.Contains(scheduledTaskApplyScript, "task '\" + $name + \"' was not registered in '\" + $path + \"'") {
		t.Fatalf("expected scheduled task apply script to verify exact registration, got %q", scheduledTaskApplyScript)
	}
	if !strings.Contains(scheduledTaskApplyScript, "New-ScheduledTaskPrincipal") {
		t.Fatalf("expected scheduled task apply script to build a principal, got %q", scheduledTaskApplyScript)
	}
	if !strings.Contains(scheduledTaskApplyScript, "ServiceAccount") {
		t.Fatalf("expected scheduled task apply script to support service-account logons, got %q", scheduledTaskApplyScript)
	}
}

func TestScheduledTaskCheckScriptUsesExactFolderLookup(t *testing.T) {
	if !strings.Contains(scheduledTaskCheckScript, "Get-TaskFromExactFolder $path $name") {
		t.Fatalf("expected scheduled task check script to use exact-folder lookup, got %q", scheduledTaskCheckScript)
	}
	if !strings.Contains(scheduledTaskCheckScript, "Normalize-TaskFolderPathForCom") {
		t.Fatalf("expected scheduled task check script to normalize COM paths, got %q", scheduledTaskCheckScript)
	}
	if !strings.Contains(scheduledTaskCheckScript, "[string]$_.TaskPath -eq $path") {
		t.Fatalf("expected scheduled task check script to filter exact task path, got %q", scheduledTaskCheckScript)
	}
	if !strings.Contains(scheduledTaskCheckScript, "$currentEnabled -ne $enabled") {
		t.Fatalf("expected scheduled task check script to compare enabled state, got %q", scheduledTaskCheckScript)
	}
}

func TestScheduledTaskApplyScriptEnablesPresentTasks(t *testing.T) {
	if !strings.Contains(scheduledTaskApplyScript, "Enable-ScheduledTask -TaskPath $path -TaskName $name | Out-Null") {
		t.Fatalf("expected scheduled task apply script to enable present tasks, got %q", scheduledTaskApplyScript)
	}
}

func TestRemoveAppxApplyScriptGuardsAgainstEmptyPackageNames(t *testing.T) {
	if !strings.Contains(removeAppxPackagesApplyScript, "IsNullOrWhiteSpace($packageFullName)") {
		t.Fatalf("expected remove-appx apply script to guard PackageFullName, got %q", removeAppxPackagesApplyScript)
	}
	if !strings.Contains(removeAppxPackagesApplyScript, "IsNullOrWhiteSpace($packageName)") {
		t.Fatalf("expected remove-appx apply script to guard provisioned PackageName, got %q", removeAppxPackagesApplyScript)
	}
	if !strings.Contains(removeAppxPackagesApplyScript, "skipping appx package ") {
		t.Fatalf("expected remove-appx apply script to log skipped malformed packages, got %q", removeAppxPackagesApplyScript)
	}
}

func TestRemoveAppxCheckScriptFiltersMalformedPackageNames(t *testing.T) {
	if !strings.Contains(removeAppxPackagesCheckScript, "IsNullOrWhiteSpace([string]$_.PackageFullName)") {
		t.Fatalf("expected remove-appx check script to guard PackageFullName, got %q", removeAppxPackagesCheckScript)
	}
	if !strings.Contains(removeAppxPackagesCheckScript, "IsNullOrWhiteSpace($packageName)") {
		t.Fatalf("expected remove-appx check script to guard provisioned PackageName, got %q", removeAppxPackagesCheckScript)
	}
}

func TestWinRMTarget_CopyAndReadFile(t *testing.T) {
	var scripts []string
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			scripts = append(scripts, command)
			if strings.Contains(command, "ToBase64String([IO.File]::ReadAllBytes") {
				return base64.StdEncoding.EncodeToString([]byte("hello")), "", 0, nil
			}
			return "", "", 0, nil
		},
	}

	src := t.TempDir() + "/src.txt"
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := tgt.CopyFile(context.Background(), src, "C:\\Temp\\dst.txt"); err != nil {
		t.Fatalf("CopyFile returned error: %v", err)
	}
	if len(scripts) < 1 {
		t.Fatalf("expected upload script, got %d", len(scripts))
	}

	data, err := tgt.ReadFile(context.Background(), "C:\\Temp\\dst.txt")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("expected hello, got %q", string(data))
	}
}

func TestWinRMTarget_CopyFileFallsBackWhenPersistentSessionCreationFails(t *testing.T) {
	var runPSCalls int
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMShellClient{
		createShell: func() (*winrm.Shell, error) {
			return nil, errors.New("shell unavailable")
		},
		fakeWinRMClient: fakeWinRMClient{
			runPS: func(_ context.Context, command string) (string, string, int, error) {
				runPSCalls++
				return "", "", 0, nil
			},
		},
	}

	src := t.TempDir() + "/src.txt"
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := tgt.CopyFile(context.Background(), src, "C:\\Temp\\dst.txt")
	if err != nil {
		t.Fatalf("expected CopyFile fallback to succeed, got %v", err)
	}
	if runPSCalls == 0 {
		t.Fatal("expected CopyFile to fall back to legacy PowerShell copy path")
	}
}

func TestWinRMTarget_ReachableAndInfo(t *testing.T) {
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runCmd: func(_ context.Context, command string) (string, string, int, error) {
			if command != "echo preflight" {
				t.Fatalf("unexpected command %q", command)
			}
			return "preflight", "", 0, nil
		},
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			if !strings.Contains(command, "ConvertTo-Json -Compress") {
				t.Fatalf("unexpected powershell %q", command)
			}
			return `{"hostname":"kiosk-a","version":"10.0.19045","build":"19045","arch":"64-bit"}`, "", 0, nil
		},
	}

	reachable, err := tgt.Reachable(context.Background())
	if err != nil {
		t.Fatalf("Reachable returned error: %v", err)
	}
	if !reachable {
		t.Fatal("expected reachable target")
	}

	info, err := tgt.Info(context.Background())
	if err != nil {
		t.Fatalf("Info returned error: %v", err)
	}
	if info.Hostname != "kiosk-a" || info.OSBuild != "19045" || info.Arch != "amd64" {
		t.Fatalf("unexpected info: %#v", info)
	}
}

func TestNormalizeWindowsArch(t *testing.T) {
	cases := map[string]string{
		"64-bit": "amd64",
		"X64":    "amd64",
		"Arm64":  "arm64",
		"x86":    "386",
	}
	for input, want := range cases {
		if got := normalizeWindowsArch(input); got != want {
			t.Fatalf("normalizeWindowsArch(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWinRMTarget_ExecutePowerShellCheckScript(t *testing.T) {
	var commands []string

	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			commands = append(commands, command)
			if !strings.Contains(command, "__pf_check_script") {
				t.Fatalf("expected combined ensure script, got %q", command)
			}
			// Simulate apply output followed by the ensure sentinel.
			return "applied\nchanged", "", 0, nil
		},
	}

	result, err := tgt.Execute(context.Background(), "task-2", "powershell", map[string]any{
		"check_script": "return $true",
		"script":       "Write-Output 'applied'",
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected StatusChanged, got %q", result.Status)
	}
	if len(commands) != 1 {
		t.Fatalf("expected 1 combined PowerShell invocation, got %d", len(commands))
	}
}

func TestWinRMTarget_RunPowerShellScriptFallsBackToTempFileWhenCommandLineTooLong(t *testing.T) {
	var psCommands []string
	var cmdCommands []string

	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			psCommands = append(psCommands, command)
			switch {
			case strings.Contains(command, "Write-Output 'oversized'"):
				return "", "The command line is too long.", 1, nil
			case strings.Contains(command, "[IO.File]::WriteAllBytes($path,"):
				return "", "", 0, nil
			case strings.Contains(command, "Remove-Item -LiteralPath $path -Force"):
				return "", "", 0, nil
			default:
				t.Fatalf("unexpected powershell command %q", command)
				return "", "", 0, nil
			}
		},
		runCmd: func(_ context.Context, command string) (string, string, int, error) {
			cmdCommands = append(cmdCommands, command)
			if !strings.Contains(command, `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "C:\Windows\Temp\preflight\run-`) {
				t.Fatalf("unexpected runCmd command %q", command)
			}
			return "oversized-ok", "", 0, nil
		},
	}

	out, err := tgt.RunPowerShellScript(context.Background(), "Write-Output 'oversized'")
	if err != nil {
		t.Fatalf("RunPowerShellScript returned error: %v", err)
	}
	if out != "oversized-ok" {
		t.Fatalf("expected fallback output, got %q", out)
	}
	if len(cmdCommands) != 1 {
		t.Fatalf("expected 1 runCmd invocation, got %d", len(cmdCommands))
	}
	if len(psCommands) < 3 {
		t.Fatalf("expected initial run, upload, and cleanup scripts; got %d", len(psCommands))
	}
}

func TestWinRMTarget_RunPowerShellScriptProactivelyStagesLargeScripts(t *testing.T) {
	var psCommands []string
	var cmdCommands []string

	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			psCommands = append(psCommands, command)
			switch {
			case strings.Contains(command, "[IO.File]::WriteAllBytes($path,"):
				return "", "", 0, nil
			case strings.Contains(command, "[IO.File]::Open($path, [IO.FileMode]::Append"):
				return "", "", 0, nil
			case strings.Contains(command, "Remove-Item -LiteralPath $path -Force"):
				return "", "", 0, nil
			default:
				t.Fatalf("unexpected powershell command %q", command)
				return "", "", 0, nil
			}
		},
		runCmd: func(_ context.Context, command string) (string, string, int, error) {
			cmdCommands = append(cmdCommands, command)
			return "staged-ok", "", 0, nil
		},
	}

	largeScript := strings.Repeat("Write-Output 'oversized'\n", 200)
	out, err := tgt.RunPowerShellScript(context.Background(), largeScript)
	if err != nil {
		t.Fatalf("RunPowerShellScript returned error: %v", err)
	}
	if out != "staged-ok" {
		t.Fatalf("expected staged output, got %q", out)
	}
	if len(cmdCommands) != 1 {
		t.Fatalf("expected 1 runCmd invocation, got %d", len(cmdCommands))
	}
	for _, command := range psCommands {
		if strings.Contains(command, largeScript) {
			t.Fatalf("expected large script to avoid direct RunPSWithContext call")
		}
	}
}

func TestWinRMPackageRemotePathUsesWindowsTempAndUniqueNames(t *testing.T) {
	first := winRMPackageRemotePath(0, "/tmp/app/installer.msi")
	second := winRMPackageRemotePath(1, "/tmp/other/installer.msi")

	if first != `C:\Windows\Temp\preflight\000-installer.msi` {
		t.Fatalf("unexpected first remote path %q", first)
	}
	if second != `C:\Windows\Temp\preflight\001-installer.msi` {
		t.Fatalf("unexpected second remote path %q", second)
	}
}

func TestWinRMTarget_ApplyPackageStagesInstallersToWindowsPath(t *testing.T) {
	var scripts []string
	call := 0

	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			scripts = append(scripts, command)
			call++
			if call == 1 {
				return "true", "", 0, nil
			}
			return "", "", 0, nil
		},
	}

	dir := t.TempDir()
	firstDir := filepath.Join(dir, "one")
	secondDir := filepath.Join(dir, "two")
	if err := os.MkdirAll(firstDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", firstDir, err)
	}
	if err := os.MkdirAll(secondDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", secondDir, err)
	}
	msi := filepath.Join(firstDir, "installer.msi")
	exe := filepath.Join(secondDir, "installer.msi")
	if err := os.WriteFile(msi, []byte("msi"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", msi, err)
	}
	if err := os.WriteFile(exe, []byte("exe"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", exe, err)
	}

	_, err := tgt.Execute(context.Background(), "task-package", "package", map[string]any{
		"packages": []any{
			map[string]any{"product_id": "{APP-1}", "source": msi},
			map[string]any{"product_id": "{APP-2}", "source": exe},
		},
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	joined := strings.Join(scripts, "\n")
	if strings.Contains(joined, os.TempDir()) {
		t.Fatalf("expected remote staging path to avoid controller temp dir, got scripts:\n%s", joined)
	}
	for _, expected := range []string{
		winRMPackageRemotePath(0, msi),
		winRMPackageRemotePath(1, exe),
	} {
		encodedPath, err := json.Marshal(expected)
		if err != nil {
			t.Fatalf("json.Marshal(%q): %v", expected, err)
		}
		if !strings.Contains(joined, base64.StdEncoding.EncodeToString(encodedPath)) {
			t.Fatalf("expected staged path %q in scripts:\n%s", expected, joined)
		}
	}
}

func TestWinRMTarget_ApplyPackageAbsentSkipsUpload(t *testing.T) {
	var scripts []string
	call := 0

	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			scripts = append(scripts, command)
			call++
			if call == 1 {
				return "true", "", 0, nil
			}
			return "", "", 0, nil
		},
	}

	_, err := tgt.Execute(context.Background(), "task-package-absent", "package", map[string]any{
		"packages": []any{
			map[string]any{"product_id": "{APP-1}", "ensure": "absent"},
		},
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(scripts) != 2 {
		t.Fatalf("expected check and apply scripts only, got %d scripts", len(scripts))
	}
	for _, script := range scripts {
		if strings.Contains(script, "WriteAllBytes") {
			t.Fatalf("expected absent package to skip upload scripts, got:\n%s", script)
		}
	}
}

func TestWinRMTarget_ExecuteShortcutDetectsDriftAndCreatesParentDir(t *testing.T) {
	call := 0
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			call++
			switch call {
			case 1:
				for _, fragment := range []string{
					"$shortcut.TargetPath -ne [string]$params.target",
					"$shortcut.Arguments -ne $args",
					"$shortcut.IconLocation -ne $icon",
				} {
					if !strings.Contains(command, fragment) {
						t.Fatalf("expected shortcut check script to contain %q, got:\n%s", fragment, command)
					}
				}
				return "true", "", 0, nil
			case 2:
				for _, fragment := range []string{
					"New-Item -ItemType Directory -Path $parent -Force",
					"$shortcut.Arguments = if ($params.args)",
					"$shortcut.IconLocation = if ($params.icon)",
				} {
					if !strings.Contains(command, fragment) {
						t.Fatalf("expected shortcut apply script to contain %q, got:\n%s", fragment, command)
					}
				}
				return "", "", 0, nil
			default:
				t.Fatalf("unexpected extra PowerShell invocation %d", call)
				return "", "", 0, nil
			}
		},
	}

	result, err := tgt.Execute(context.Background(), "task-shortcut", "shortcut", map[string]any{
		"destination": `C:\Users\Public\Desktop\App.lnk`,
		"target":      `C:\Program Files\App\app.exe`,
		"args":        "--kiosk",
		"icon":        `C:\Program Files\App\app.ico`,
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected StatusChanged, got %q", result.Status)
	}
}

func TestWinRMTarget_ExecuteShortcutAbsentRemovesShortcut(t *testing.T) {
	call := 0
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			call++
			switch call {
			case 1:
				if !strings.Contains(command, "$ensure -eq 'absent'") || !strings.Contains(command, "Test-Path -LiteralPath $destination") {
					t.Fatalf("expected shortcut absent check script, got:\n%s", command)
				}
				return "true", "", 0, nil
			case 2:
				if !strings.Contains(command, "Remove-Item -LiteralPath $destination -Force") {
					t.Fatalf("expected shortcut remove script, got:\n%s", command)
				}
				return "", "", 0, nil
			default:
				t.Fatalf("unexpected extra PowerShell invocation %d", call)
				return "", "", 0, nil
			}
		},
	}

	_, err := tgt.Execute(context.Background(), "task-shortcut-absent", "shortcut", map[string]any{
		"destination": `C:\Users\Public\Desktop\App.lnk`,
		"ensure":      "absent",
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}

func TestWinRMTarget_ExecuteUserHonorsPasswordAndGroupSemantics(t *testing.T) {
	call := 0
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			call++
			switch call {
			case 1:
				for _, fragment := range []string{
					"if ($params.password)",
					"Get-LocalGroupMember -Group ([string]$group)",
					"[regex]::Escape($name)",
				} {
					if !strings.Contains(command, fragment) {
						t.Fatalf("expected user check script to contain %q, got:\n%s", fragment, command)
					}
				}
				return "true", "", 0, nil
			case 2:
				for _, fragment := range []string{
					"New-LocalUser -Name $name -NoPassword",
					"Set-LocalUser -Password $securePassword",
					"Add-LocalGroupMember -Group ([string]$group) -Member $name",
				} {
					if !strings.Contains(command, fragment) {
						t.Fatalf("expected user apply script to contain %q, got:\n%s", fragment, command)
					}
				}
				return "", "", 0, nil
			default:
				t.Fatalf("unexpected extra PowerShell invocation %d", call)
				return "", "", 0, nil
			}
		},
	}

	_, err := tgt.Execute(context.Background(), "task-user", "user", map[string]any{
		"name":     "kiosk",
		"password": "secret",
		"groups":   []any{"Users", "Remote Desktop Users"},
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}

func TestWinRMTarget_ExecuteUserAbsentRemovesUser(t *testing.T) {
	call := 0
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			call++
			switch call {
			case 1:
				if !strings.Contains(command, "$ensure -eq 'absent'") || !strings.Contains(command, "Write-Output ($null -ne $user)") {
					t.Fatalf("expected user absent check script, got:\n%s", command)
				}
				return "true", "", 0, nil
			case 2:
				if !strings.Contains(command, "Remove-LocalUser -Name $name") {
					t.Fatalf("expected user remove script, got:\n%s", command)
				}
				return "", "", 0, nil
			default:
				t.Fatalf("unexpected extra PowerShell invocation %d", call)
				return "", "", 0, nil
			}
		},
	}

	_, err := tgt.Execute(context.Background(), "task-user-absent", "user", map[string]any{
		"name":   "kiosk",
		"ensure": "absent",
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}

func TestNormalizeFirewallRuleParamsCanonicalizesPorts(t *testing.T) {
	normalized, err := normalizeFirewallRuleParams(map[string]any{
		"name":  "HTTP",
		"ports": []any{80, "443"},
	})
	if err != nil {
		t.Fatalf("normalizeFirewallRuleParams returned error: %v", err)
	}
	if normalized["ports"] != "80,443" {
		t.Fatalf("expected normalized ports, got %#v", normalized["ports"])
	}
}

func commandContainsNormalizedPorts(command, expected string) bool {
	if strings.Contains(command, expected) {
		return true
	}

	expectedJSON, err := json.Marshal(expected)
	if err == nil && strings.Contains(command, base64.StdEncoding.EncodeToString(expectedJSON)) {
		return true
	}
	if strings.Contains(command, base64.StdEncoding.EncodeToString([]byte(expected))) {
		return true
	}

	tokens := strings.FieldsFunc(command, func(r rune) bool {
		switch r {
		case ' ', '\n', '\r', '\t', '"', '\'', '`', '(', ')', '{', '}', '[', ']', ',', ';':
			return true
		default:
			return false
		}
	})
	for _, token := range tokens {
		if len(token) < 8 || len(token)%4 != 0 {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(token)
		if err != nil {
			continue
		}
		if strings.Contains(string(decoded), expected) {
			return true
		}
		var decodedString string
		if err := json.Unmarshal(decoded, &decodedString); err == nil && decodedString == expected {
			return true
		}
	}

	return false
}

func TestWinRMTarget_ExecuteFirewallRuleDetectsDriftAndUpdatesRule(t *testing.T) {
	call := 0
	normalizedPortsObserved := false
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			if commandContainsNormalizedPorts(command, "80,443") {
				normalizedPortsObserved = true
			}
			call++
			switch call {
			case 1:
				for _, fragment := range []string{
					"Get-NetFirewallPortFilter",
					"$rule.Direction -ne $directionMap",
					"$portFilter.LocalPort",
				} {
					if !strings.Contains(command, fragment) {
						t.Fatalf("expected firewall check script to contain %q, got:\n%s", fragment, command)
					}
				}
				return "true", "", 0, nil
			case 2:
				for _, fragment := range []string{
					"Set-NetFirewallRule -DisplayName $name -Direction",
					"New-NetFirewallRule @newParams",
				} {
					if !strings.Contains(command, fragment) {
						t.Fatalf("expected firewall apply script to contain %q, got:\n%s", fragment, command)
					}
				}
				return "", "", 0, nil
			default:
				t.Fatalf("unexpected extra PowerShell invocation %d", call)
				return "", "", 0, nil
			}
		},
	}

	_, err := tgt.Execute(context.Background(), "task-firewall", "firewall_rule", map[string]any{
		"name":      "HTTP",
		"direction": "inbound",
		"action":    "allow",
		"protocol":  "tcp",
		"ports":     []any{80, 443},
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !normalizedPortsObserved {
		t.Fatalf("expected generated PowerShell to contain normalized ports %q", "80,443")
	}
}

func TestWinRMTarget_ExecuteUnknownModuleErrors(t *testing.T) {
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{}

	// Without become: error should be the standard unsupportedRuntimeModuleError format.
	_, err := tgt.Execute(context.Background(), "task-1", "nonexistent_module", nil, ExecutionOptions{}, false, nil)
	if err == nil {
		t.Fatal("expected error for unknown module, got nil")
	}
	want := `windows-powershell runtime: module "nonexistent_module" is not supported`
	if err.Error() != want {
		t.Errorf("without become: expected %q, got %q", want, err.Error())
	}

	// With become enabled: unknown modules should still be reported as unsupported runtime modules.
	_, err = tgt.Execute(context.Background(), "task-2", "nonexistent_module", nil, ExecutionOptions{
		Become: &BecomeOptions{Enabled: true, User: "kiosk", Password: "secret"},
	}, false, nil)
	if err == nil {
		t.Fatal("expected error for unknown module with become, got nil")
	}
	wantBecome := `windows-powershell runtime: module "nonexistent_module" is not supported`
	if err.Error() != wantBecome {
		t.Errorf("with become: expected %q, got %q", wantBecome, err.Error())
	}
}

func TestWinRMTarget_ExecuteFirewallRuleAbsentRemovesRule(t *testing.T) {
	call := 0
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			call++
			switch call {
			case 1:
				if !strings.Contains(command, "$ensure -eq 'absent'") || !strings.Contains(command, "Write-Output ($null -ne $rule)") {
					t.Fatalf("expected firewall absent check script, got:\n%s", command)
				}
				return "true", "", 0, nil
			case 2:
				if !strings.Contains(command, "Remove-NetFirewallRule -DisplayName $name") {
					t.Fatalf("expected firewall remove script, got:\n%s", command)
				}
				return "", "", 0, nil
			default:
				t.Fatalf("unexpected extra PowerShell invocation %d", call)
				return "", "", 0, nil
			}
		},
	}

	_, err := tgt.Execute(context.Background(), "task-firewall-absent", "firewall_rule", map[string]any{
		"name":   "HTTP",
		"ensure": "absent",
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}
