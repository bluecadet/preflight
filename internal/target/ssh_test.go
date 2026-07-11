package target

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf16"
)

// fakePluggableModule lets target tests exercise the PluggableModule branch
// without depending on the plugins package (which would create a target →
// plugins → target import cycle). Its BindTarget returns itself unbound so it
// can stand in for a plugin in tests that only need the type distinction.
type fakePluggableModule struct{ path string }

func (fakePluggableModule) Check(context.Context, map[string]any, OutputFunc) (CheckResult, error) {
	return CheckResult{}, nil
}
func (fakePluggableModule) Apply(context.Context, map[string]any, OutputFunc) (ApplyResult, error) {
	return ApplyResult{}, nil
}
func (m fakePluggableModule) PluginPath() string              { return m.path }
func (m fakePluggableModule) BindTarget(ops TargetOps) Module { return m }

// recordingPluggableModule is a fakePluggableModule that records whether its
// ops backend was bound and its Check/Apply invoked. Used to assert plugins
// are dispatched (not refused) over a transport.
type recordingPluggableModule struct {
	fakePluggableModule
	bound   bool
	checked bool
}

func (m *recordingPluggableModule) BindTarget(ops TargetOps) Module {
	m.bound = true
	return m
}
func (m *recordingPluggableModule) Check(ctx context.Context, params map[string]any, out OutputFunc) (CheckResult, error) {
	m.checked = true
	return m.fakePluggableModule.Check(ctx, params, out)
}

type fakeSSHRunner struct {
	run func(context.Context, string, []byte) (string, string, int, error)
}

func (f *fakeSSHRunner) Run(ctx context.Context, command string, stdin []byte) (string, string, int, error) {
	return f.run(ctx, command, stdin)
}

func TestSSHTarget_ExecuteShellPOSIX(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				return "preflight", "", 0, nil
			case isPOSIXProbeCommand(command):
				return posixProbeOutput(), "", 0, nil
			case strings.Contains(command, `"echo" "hello"`):
				return "", "", 0, nil
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
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
		t.Fatalf("expected changed result, got %q", result.Status)
	}
}

func TestSSHTarget_ExecuteShellPOSIXWithBecomeUser(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, stdin []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				return "preflight", "", 0, nil
			case isPOSIXProbeCommand(command):
				return posixProbeOutput(), "", 0, nil
			case strings.Contains(command, "sudo -S -p '' -u 'appuser' /bin/sh -lc"):
				if string(stdin) != "hunter2\n" {
					t.Fatalf("unexpected stdin %q", string(stdin))
				}
				return "", "", 0, nil
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	result, err := tgt.Execute(context.Background(), "task-1", "shell", map[string]any{
		"cmd":  "echo",
		"args": []any{"hello"},
	}, ExecutionOptions{
		Become: &BecomeOptions{
			Enabled:  true,
			User:     "appuser",
			Password: "hunter2",
		},
	}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected changed result, got %q", result.Status)
	}
}

func TestSSHTarget_POSIXRuntimeCachesDetection(t *testing.T) {
	var detectionCount int

	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, stdin []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				detectionCount++
				return "preflight", "", 0, nil
			case isPOSIXProbeCommand(command):
				return "hostname=kiosk-a\nkernel=Linux\narch=x86_64\nos_name=ubuntu\nos_version=22.04\npackage_manager=apt\ninit=systemd\neuid=1000\nsudo=1\n", "", 0, nil
			case strings.HasPrefix(command, "mkdir -p"):
				if decoded, err := base64.StdEncoding.DecodeString(string(stdin)); err != nil || string(decoded) != "hello" {
					t.Fatalf("unexpected stdin %q err=%v", string(stdin), err)
				}
				return "", "", 0, nil
			case strings.HasPrefix(command, "chmod "):
				if !strings.Contains(command, "0644") {
					t.Errorf("chmod called with unexpected mode: %q", command)
				}
				return "", "", 0, nil
			case strings.HasPrefix(command, "base64 <"):
				return base64.StdEncoding.EncodeToString([]byte("hello")), "", 0, nil
			case command == "echo preflight":
				return "preflight", "", 0, nil
			case strings.Contains(command, "$(hostname)") && strings.Contains(command, "$(uname -s)") && strings.Contains(command, "$(uname -m)"):
				return "hostname=kiosk-a\nkernel=Linux\narch=x86_64\nos_name=ubuntu\nos_version=22.04\npackage_manager=apt\ninit=systemd\n", "", 0, nil
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	src := t.TempDir() + "/src.txt"
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := tgt.CopyFile(context.Background(), src, "/tmp/dst.txt"); err != nil {
		t.Fatalf("CopyFile returned error: %v", err)
	}
	data, err := tgt.ReadFile(context.Background(), "/tmp/dst.txt")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("expected hello, got %q", string(data))
	}

	reachable, err := tgt.Reachable(context.Background())
	if err != nil {
		t.Fatalf("Reachable returned error: %v", err)
	}
	if !reachable {
		t.Fatal("expected target to be reachable")
	}

	info, err := tgt.Info(context.Background())
	if err != nil {
		t.Fatalf("Info returned error: %v", err)
	}
	if info.Hostname != "kiosk-a" || info.OSVersion != "22.04" || info.Arch != "x86_64" || info.OSName != "ubuntu" || info.PackageManager != "apt" || info.Init != "systemd" || info.OSFamily != OSFamilyLinux {
		t.Fatalf("unexpected info: %#v", info)
	}

	if detectionCount != 1 {
		t.Fatalf("expected runtime detection to be cached, got %d probes", detectionCount)
	}
}

// TestSSHTarget_POSIXProbeCachedAcrossInfoCalls asserts the one-probe-per-run
// contract: the POSIX detection probe runs lazily on the first Info() call and
// is cached so a second Info() call (and the facts gatherer) read the cached
// result without re-running the probe script.
func TestSSHTarget_POSIXProbeCachedAcrossInfoCalls(t *testing.T) {
	var probeRuns int
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				return "preflight", "", 0, nil
			case strings.Contains(command, "os_name=") && strings.Contains(command, "package_manager="):
				probeRuns++
				return "hostname=kiosk-c\nkernel=Linux\narch=x86_64\nos_name=rocky\nos_version=9.3\npackage_manager=dnf\ninit=systemd\neuid=0\nsudo=1\n", "", 0, nil
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	for i := range 2 {
		info, err := tgt.Info(context.Background())
		if err != nil {
			t.Fatalf("Info #%d returned error: %v", i, err)
		}
		if info.OSName != "rocky" || info.PackageManager != "dnf" || info.Init != "systemd" || info.OSVersion != "9.3" {
			t.Fatalf("Info #%d unexpected: %#v", i, info)
		}
	}
	if probeRuns != 1 {
		t.Fatalf("expected POSIX probe to run once per target, got %d", probeRuns)
	}
}
func TestSSHTarget_DetectsWindowsPowerShellRuntimeAndExecutesShell(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			if !isEncodedPowerShellCommand(command) {
				t.Fatalf("expected encoded powershell command, got %q", command)
			}
			decoded := decodeEncodedPowerShellCommand(t, command)
			switch {
			case strings.Contains(decoded, "preflight-windows"):
				return "preflight-windows", "", 0, nil
			case strings.Contains(decoded, "& $cmd @args"):
				return "applied", "", 0, nil
			default:
				t.Fatalf("unexpected powershell script %q", decoded)
				return "", "", 0, nil
			}
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
		t.Fatalf("expected changed result, got %q", result.Status)
	}
	if result.Message != "applied" {
		t.Fatalf("expected apply output, got %q", result.Message)
	}
}

func TestSSHTarget_WindowsCopyReadReachableAndInfo(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, stdin []byte) (string, string, int, error) {
			if !isEncodedPowerShellCommand(command) {
				t.Fatalf("expected encoded powershell command, got %q", command)
			}
			decoded := decodeEncodedPowerShellCommand(t, command)
			switch {
			case strings.Contains(decoded, "preflight-windows"):
				return "preflight-windows", "", 0, nil
			case strings.Contains(decoded, "FromBase64String($payload)"):
				if decodedBytes, err := base64.StdEncoding.DecodeString(string(stdin)); err != nil || string(decodedBytes) != "hello" {
					t.Fatalf("unexpected stdin %q err=%v", string(stdin), err)
				}
				return "", "", 0, nil
			case strings.Contains(decoded, "ToBase64String([IO.File]::ReadAllBytes"):
				return base64.StdEncoding.EncodeToString([]byte("hello")), "", 0, nil
			case strings.Contains(decoded, "Write-Output 'preflight'"):
				return "preflight", "", 0, nil
			case strings.Contains(decoded, "ConvertTo-Json -Compress"):
				return `{"hostname":"kiosk-a","version":"10.0.19045","build":"19045","arch":"64-bit"}`, "", 0, nil
			default:
				t.Fatalf("unexpected powershell script %q", decoded)
				return "", "", 0, nil
			}
		},
	}

	src := t.TempDir() + "/src.txt"
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := tgt.CopyFile(context.Background(), src, `C:\Temp\dst.txt`); err != nil {
		t.Fatalf("CopyFile returned error: %v", err)
	}
	data, err := tgt.ReadFile(context.Background(), `C:\Temp\dst.txt`)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("expected hello, got %q", string(data))
	}

	reachable, err := tgt.Reachable(context.Background())
	if err != nil {
		t.Fatalf("Reachable returned error: %v", err)
	}
	if !reachable {
		t.Fatal("expected target to be reachable")
	}

	info, err := tgt.Info(context.Background())
	if err != nil {
		t.Fatalf("Info returned error: %v", err)
	}
	if info.Hostname != "kiosk-a" || info.OSBuild != "19045" || info.Arch != "amd64" {
		t.Fatalf("unexpected info: %#v", info)
	}
}

func TestSSHTarget_WindowsPowerShellModuleCheckScript(t *testing.T) {
	call := 0

	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			if !isEncodedPowerShellCommand(command) {
				t.Fatalf("expected encoded powershell command, got %q", command)
			}
			decoded := decodeEncodedPowerShellCommand(t, command)
			switch {
			case strings.Contains(decoded, "preflight-windows"):
				return "preflight-windows", "", 0, nil
			case strings.Contains(decoded, "__pf_check_script"):
				// Combined ensure script: simulate apply output + sentinel.
				call++
				return "applied\nchanged", "", 0, nil
			default:
				t.Fatalf("unexpected powershell script %q", decoded)
				return "", "", 0, nil
			}
		},
	}

	result, err := tgt.Execute(context.Background(), "task-ps", "powershell", map[string]any{
		"check_script": "return $true",
		"script":       "Write-Output 'applied'",
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected changed result, got %q", result.Status)
	}
	if call != 1 {
		t.Fatalf("expected 1 combined powershell invocation after detection, got %d", call)
	}
}

func TestSSHTarget_WindowsEnvironmentWaitRegistryAndReboot(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			if !isEncodedPowerShellCommand(command) {
				t.Fatalf("expected encoded powershell command, got %q", command)
			}
			decoded := decodeEncodedPowerShellCommand(t, command)
			switch {
			case strings.Contains(decoded, "preflight-windows"):
				return "preflight-windows", "", 0, nil
			case strings.Contains(decoded, "GetEnvironmentVariable"):
				return "changed", "", 0, nil
			case strings.Contains(decoded, "switch ($params.condition)"):
				return "true", "", 0, nil
			case strings.Contains(decoded, "Normalize-RegistryKind"):
				return "changed", "", 0, nil
			case strings.Contains(decoded, "shutdown /r /t 45"):
				return "", "", 0, nil
			default:
				return "", "", 0, nil
			}
		},
	}

	envResult, err := tgt.Execute(context.Background(), "task-env", "environment", map[string]any{
		"name":  "PREFLIGHT_MODE",
		"value": "kiosk",
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("environment Execute returned error: %v", err)
	}
	if envResult.Status != StatusChanged {
		t.Fatalf("expected environment change, got %q", envResult.Status)
	}

	waitResult, err := tgt.Execute(context.Background(), "task-wait", "wait", map[string]any{
		"condition": "file_exists",
		"target":    `C:\Temp\flag.txt`,
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("wait Execute returned error: %v", err)
	}
	if waitResult.Status != StatusOK {
		t.Fatalf("expected wait no-op, got %q", waitResult.Status)
	}

	registryResult, err := tgt.Execute(context.Background(), "task-reg", "registry", map[string]any{
		"path": `HKLM:\Software\Preflight`,
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("registry Execute returned error: %v", err)
	}
	if registryResult.Status != StatusChanged {
		t.Fatalf("expected registry change, got %q", registryResult.Status)
	}

	rebootResult, err := tgt.Execute(context.Background(), "task-reboot", "reboot", map[string]any{
		"timeout": 45,
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("reboot Execute returned error: %v", err)
	}
	if rebootResult.Status != StatusChanged {
		t.Fatalf("expected reboot change, got %q", rebootResult.Status)
	}
}

func TestSSHTarget_POSIXFileHashNoop(t *testing.T) {
	src := t.TempDir() + "/src.txt"
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	expectedHash, err := hashLocalFile(src)
	if err != nil {
		t.Fatalf("hashLocalFile: %v", err)
	}

	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				return "preflight", "", 0, nil
			case isPOSIXProbeCommand(command):
				return posixProbeOutput(), "", 0, nil
			case strings.Contains(command, "printf missing") && strings.Contains(command, "/tmp/dst.txt"):
				return "file", "", 0, nil
			case strings.HasPrefix(command, "sha256sum "):
				return expectedHash + "  /tmp/dst.txt\n", "", 0, nil
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	result, err := tgt.Execute(context.Background(), "task-file", "file", map[string]any{
		"dest": "/tmp/dst.txt",
		"src":  src,
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("expected no-op status, got %q", result.Status)
	}
}

func TestSSHTarget_POSIXFileContentWritesStdin(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, stdin []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				return "preflight", "", 0, nil
			case isPOSIXProbeCommand(command):
				return posixProbeOutput(), "", 0, nil
			case strings.Contains(command, "printf missing") && strings.Contains(command, "/tmp/secret.txt"):
				return "missing", "", 0, nil
			case strings.Contains(command, "cat > ") && strings.Contains(command, "/tmp/secret.txt"):
				if string(stdin) != "secret\ncontent\n" {
					t.Fatalf("unexpected file content stdin %q", string(stdin))
				}
				return "", "", 0, nil
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	result, err := tgt.Execute(context.Background(), "task-file", "file", map[string]any{
		"dest":    "/tmp/secret.txt",
		"content": "secret\ncontent\n",
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected changed status, got %q", result.Status)
	}
}

func TestSSHTarget_POSIXFileContentHashNoop(t *testing.T) {
	content := "secret\ncontent\n"
	expectedHash := hashBytes([]byte(content))

	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				return "preflight", "", 0, nil
			case isPOSIXProbeCommand(command):
				return posixProbeOutput(), "", 0, nil
			case strings.Contains(command, "printf missing") && strings.Contains(command, "/tmp/secret.txt"):
				return "file", "", 0, nil
			case strings.HasPrefix(command, "sha256sum "):
				return expectedHash + "  /tmp/secret.txt\n", "", 0, nil
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	result, err := tgt.Execute(context.Background(), "task-file", "file", map[string]any{
		"dest":    "/tmp/secret.txt",
		"content": content,
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("expected no-op status, got %q", result.Status)
	}
}

func TestSSHTarget_POSIXPowerShellModuleUsesRemoteBinary(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			switch {
			case !isEncodedPowerShellCommand(command):
				if command == "printf preflight" {
					return "preflight", "", 0, nil
				}
				if isPOSIXProbeCommand(command) {
					return posixProbeOutput(), "", 0, nil
				}
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			default:
				decoded := decodeEncodedPowerShellCommand(t, command)
				switch {
				case strings.Contains(decoded, "preflight-nonwindows"):
					return "preflight-nonwindows", "", 0, nil
				case strings.Contains(decoded, "Write-Output 'applied'"):
					return "applied", "", 0, nil
				default:
					t.Fatalf("unexpected powershell script %q", decoded)
					return "", "", 0, nil
				}
			}
		},
	}

	result, err := tgt.Execute(context.Background(), "task-ps", "powershell", map[string]any{
		"script": "Write-Output 'applied'",
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected changed result, got %q", result.Status)
	}
	if result.Message != "applied" {
		t.Fatalf("expected apply output, got %q", result.Message)
	}
}

func TestSSHTarget_POSIXWaitServiceRunning(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				return "preflight", "", 0, nil
			case isPOSIXProbeCommand(command):
				return posixProbeOutput(), "", 0, nil
			case strings.Contains(command, "systemctl is-active --quiet"):
				return "", "", 0, nil // active → exit 0
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	result, err := tgt.Execute(context.Background(), "task-wait", "wait", map[string]any{
		"condition": "service_running",
		"target":    "nginx",
	}, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("expected status OK for an already-running service, got %q: %s", result.Status, result.Message)
	}
}

func TestSSHTarget_PluginModulesRunOverSSH(t *testing.T) {
	// Plugins execute controller-side with a target handle, so a plugin bound
	// into the SSH target's registry is dispatched through executeModule like
	// any built-in — the former "cannot run over this transport" refusal is
	// gone. The fake plugin records that it was bound and that Check ran.
	plugin := &recordingPluggableModule{fakePluggableModule: fakePluggableModule{path: "/tmp/custom-plugin"}}
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, ModuleRegistry{
		"custom": plugin,
	})
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				return "preflight", "", 0, nil
			case isPOSIXProbeCommand(command):
				return posixProbeOutput(), "", 0, nil
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	res, err := tgt.Execute(context.Background(), "task-plugin", "custom", nil, ExecutionOptions{}, false, nil)
	if err != nil {
		t.Fatalf("plugin should run over SSH, got error: %v", err)
	}
	if !plugin.bound {
		t.Error("plugin was not bound to the SSH target ops backend")
	}
	if !plugin.checked {
		t.Error("plugin Check was not invoked")
	}
	// The fake plugin reports no needed change, so the task is ok.
	if res.Status != StatusOK {
		t.Errorf("status = %q, want %q", res.Status, StatusOK)
	}
}

func TestSSHTarget_PluginBecomeRefused(t *testing.T) {
	// Plugin+become is refused in v1 with a uniform plugin_become error across
	// transports. Over SSH the plugin is deliberately not merged into the
	// become registry, so the unsupported callback fires and classifies it.
	plugin := &recordingPluggableModule{fakePluggableModule: fakePluggableModule{path: "/tmp/custom-plugin"}}
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, ModuleRegistry{
		"custom": plugin,
	})
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				return "preflight", "", 0, nil
			case isPOSIXProbeCommand(command):
				return posixProbeOutput(), "", 0, nil
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	_, err := tgt.Execute(context.Background(), "task-plugin", "custom", nil, ExecutionOptions{
		Become: &BecomeOptions{Enabled: true, User: "root"},
	}, false, nil)
	if err == nil {
		t.Fatal("expected plugin+become to be refused, got nil")
	}
	var mse *ModuleSupportError
	if !errors.As(err, &mse) {
		t.Fatalf("expected *ModuleSupportError, got %T: %v", err, err)
	}
	if mse.Class != ClassPluginBecome {
		t.Errorf("class = %q, want %q", mse.Class, ClassPluginBecome)
	}
	if mse.ReasonCode() != "plugin_become" {
		t.Errorf("reason = %q, want plugin_become", mse.ReasonCode())
	}
}

func TestSSHTarget_POSIXPowerShellRequiresRemoteBinary(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				return "preflight", "", 0, nil
			case isPOSIXProbeCommand(command):
				return posixProbeOutput(), "", 0, nil
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	_, err := tgt.Execute(context.Background(), "task-ps", "powershell", map[string]any{
		"script": "Write-Output 'hi'",
	}, ExecutionOptions{}, false, nil)
	if err == nil || !strings.Contains(err.Error(), "requires pwsh or powershell") {
		t.Fatalf("expected missing powershell error, got %v", err)
	}
}

func TestSSHTarget_POSIXUnsupportedModuleReturnsError(t *testing.T) {
	for _, module := range []string{"environment", "registry"} {
		t.Run(module, func(t *testing.T) {
			tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
			tgt.runner = &fakeSSHRunner{
				run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
					switch {
					case isEncodedPowerShellCommand(command):
						return "", "not found", 127, nil
					case command == "printf preflight":
						return "preflight", "", 0, nil
					case isPOSIXProbeCommand(command):
						return posixProbeOutput(), "", 0, nil
					default:
						t.Fatalf("unexpected command %q", command)
						return "", "", 0, nil
					}
				},
			}

			_, err := tgt.Execute(context.Background(), "task-1", module, map[string]any{}, ExecutionOptions{}, false, nil)
			if err == nil {
				t.Fatalf("expected error for unsupported module %q on POSIX runtime, got nil", module)
			}
			if !strings.Contains(err.Error(), module) {
				t.Fatalf("expected error to name the unsupported module %q, got: %v", module, err)
			}
		})
	}
}

func TestSSHTarget_ConcurrentRuntimeDetection(t *testing.T) {
	var detectionCount atomic.Int64

	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				detectionCount.Add(1)
				return "preflight", "", 0, nil
			case isPOSIXProbeCommand(command):
				return posixProbeOutput(), "", 0, nil
			case strings.Contains(command, `"echo" "hello"`):
				return "", "", 0, nil
			default:
				t.Errorf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			taskID := fmt.Sprintf("task-%d", n)
			_, err := tgt.Execute(context.Background(), taskID, "shell", map[string]any{
				"cmd":  "echo",
				"args": []any{"hello"},
			}, ExecutionOptions{}, false, nil)
			if err != nil {
				t.Errorf("goroutine %d: Execute returned error: %v", n, err)
			}
		}(i)
	}
	wg.Wait()

	if got := detectionCount.Load(); got != 1 {
		t.Fatalf("expected runtime detection exactly once, got %d", got)
	}
}

func isEncodedPowerShellCommand(command string) bool {
	return strings.Contains(command, `"-EncodedCommand"`)
}

// isPOSIXProbeCommand reports whether command is the POSIX runtime detection
// probe script (one id -u + command -v sudo round trip per target).
func isPOSIXProbeCommand(command string) bool {
	return strings.Contains(command, "$(hostname)") &&
		strings.Contains(command, "$(uname -s)") &&
		strings.Contains(command, "package_manager=")
}

// posixProbeOutput returns a probe response for an unprivileged session with
// sudo available. Tests that need a different posture (root, no sudo) build
// their own string.
func posixProbeOutput() string {
	return "hostname=kiosk-a\nkernel=Linux\narch=x86_64\nos_name=ubuntu\nos_version=22.04\npackage_manager=apt\ninit=systemd\neuid=1000\nsudo=1\n"
}

func decodeEncodedPowerShellCommand(t *testing.T, command string) string {
	t.Helper()
	re := regexp.MustCompile(`"-EncodedCommand" "([A-Za-z0-9+/=]+)"`)
	matches := re.FindStringSubmatch(command)
	if len(matches) != 2 {
		t.Fatalf("unable to find encoded command in %q", command)
	}
	data, err := base64.StdEncoding.DecodeString(matches[1])
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}
	if len(data)%2 != 0 {
		t.Fatalf("unexpected UTF-16 payload length %d", len(data))
	}
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		units = append(units, uint16(data[i])|uint16(data[i+1])<<8)
	}
	return string(utf16.Decode(units))
}
