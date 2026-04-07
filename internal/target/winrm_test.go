package target

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	}, false, nil)
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
	if len(scripts) < 2 {
		t.Fatalf("expected chunked upload scripts, got %d", len(scripts))
	}

	data, err := tgt.ReadFile(context.Background(), "C:\\Temp\\dst.txt")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("expected hello, got %q", string(data))
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

func TestWinRMTarget_ExecutePowerShellCheckScript(t *testing.T) {
	var commands []string
	call := 0

	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "user", Password: "pass"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, command string) (string, string, int, error) {
			commands = append(commands, command)
			call++
			switch call {
			case 1:
				if !strings.Contains(command, "[ScriptBlock]::Create($checkScript)") {
					t.Fatalf("expected wrapped check_script, got %q", command)
				}
				return `{"needs_change":true,"message":"rename pending"}`, "", 0, nil
			case 2:
				if !strings.Contains(command, "Write-Output 'applied'") {
					t.Fatalf("expected apply script, got %q", command)
				}
				return "applied", "", 0, nil
			default:
				t.Fatalf("unexpected extra PowerShell invocation %d: %q", call, command)
				return "", "", 0, nil
			}
		},
	}

	result, err := tgt.Execute(context.Background(), "task-2", "powershell", map[string]any{
		"check_script": "return $true",
		"script":       "Write-Output 'applied'",
	}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected StatusChanged, got %q", result.Status)
	}
	if result.Message != "applied" {
		t.Fatalf("expected apply output, got %q", result.Message)
	}
	if len(commands) != 2 {
		t.Fatalf("expected 2 PowerShell invocations, got %d", len(commands))
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
	}, false, nil)
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
	}, false, nil)
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
	}, false, nil)
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
	}, false, nil)
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
	}, false, nil)
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
	}, false, nil)
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
	}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !normalizedPortsObserved {
		t.Fatalf("expected generated PowerShell to contain normalized ports %q", "80,443")
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
	}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}
