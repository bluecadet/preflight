package target

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf16"
)

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
			case strings.HasPrefix(command, "mkdir -p"):
				if decoded, err := base64.StdEncoding.DecodeString(string(stdin)); err != nil || string(decoded) != "hello" {
					t.Fatalf("unexpected stdin %q err=%v", string(stdin), err)
				}
				return "", "", 0, nil
			case strings.HasPrefix(command, "base64 <"):
				return base64.StdEncoding.EncodeToString([]byte("hello")), "", 0, nil
			case command == "echo preflight":
				return "preflight", "", 0, nil
			case strings.Contains(command, "$(hostname)") && strings.Contains(command, "$(uname -s)") && strings.Contains(command, "$(uname -m)"):
				return "kiosk-a|Linux|x86_64", "", 0, nil
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
	if info.Hostname != "kiosk-a" || info.OSVersion != "Linux" || info.Arch != "x86_64" {
		t.Fatalf("unexpected info: %#v", info)
	}

	if detectionCount != 1 {
		t.Fatalf("expected runtime detection to be cached, got %d probes", detectionCount)
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
	}, false, nil)
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
			case strings.Contains(decoded, "[ScriptBlock]::Create($checkScript)"):
				call++
				return `{"needs_change":true,"message":"rename pending"}`, "", 0, nil
			case strings.Contains(decoded, "Write-Output 'applied'"):
				call++
				return "applied", "", 0, nil
			default:
				t.Fatalf("unexpected powershell script %q", decoded)
				return "", "", 0, nil
			}
		},
	}

	result, err := tgt.Execute(context.Background(), "task-ps", "powershell", map[string]any{
		"check_script": "return $true",
		"script":       "Write-Output 'applied'",
	}, false, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected changed result, got %q", result.Status)
	}
	if result.Message != "applied" {
		t.Fatalf("expected apply output, got %q", result.Message)
	}
	if call != 2 {
		t.Fatalf("expected 2 powershell invocations after detection, got %d", call)
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
				return "true", "", 0, nil
			case strings.Contains(decoded, "SetEnvironmentVariable"):
				return "", "", 0, nil
			case strings.Contains(decoded, "switch ($params.condition)"):
				return "true", "", 0, nil
			case strings.Contains(decoded, "Normalize-RegistryKind"):
				return "true", "", 0, nil
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
	}, false, nil)
	if err != nil {
		t.Fatalf("environment Execute returned error: %v", err)
	}
	if envResult.Status != StatusChanged {
		t.Fatalf("expected environment change, got %q", envResult.Status)
	}

	waitResult, err := tgt.Execute(context.Background(), "task-wait", "wait", map[string]any{
		"condition": "file_exists",
		"target":    `C:\Temp\flag.txt`,
	}, false, nil)
	if err != nil {
		t.Fatalf("wait Execute returned error: %v", err)
	}
	if waitResult.Status != StatusOK {
		t.Fatalf("expected wait no-op, got %q", waitResult.Status)
	}

	registryResult, err := tgt.Execute(context.Background(), "task-reg", "registry", map[string]any{
		"path": `HKLM:\Software\Preflight`,
	}, false, nil)
	if err != nil {
		t.Fatalf("registry Execute returned error: %v", err)
	}
	if registryResult.Status != StatusChanged {
		t.Fatalf("expected registry change, got %q", registryResult.Status)
	}

	rebootResult, err := tgt.Execute(context.Background(), "task-reboot", "reboot", map[string]any{
		"timeout": 45,
	}, false, nil)
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
	}, false, nil)
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
	}, false, nil)
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

func TestSSHTarget_POSIXWaitServiceRunningUnsupported(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				return "preflight", "", 0, nil
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	_, err := tgt.Execute(context.Background(), "task-wait", "wait", map[string]any{
		"condition": "service_running",
		"target":    "nginx",
	}, false, nil)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported error, got %v", err)
	}
}

func TestSSHTarget_PluginModulesDeferred(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, ModuleRegistry{
		"custom": &pluginModule{name: "custom", path: "/tmp/custom-plugin"},
	})
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			switch {
			case isEncodedPowerShellCommand(command):
				return "", "not found", 127, nil
			case command == "printf preflight":
				return "preflight", "", 0, nil
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	_, err := tgt.Execute(context.Background(), "task-plugin", "custom", nil, false, nil)
	if err == nil || !strings.Contains(err.Error(), "plugin module") {
		t.Fatalf("expected plugin deferral error, got %v", err)
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
			default:
				t.Fatalf("unexpected command %q", command)
				return "", "", 0, nil
			}
		},
	}

	_, err := tgt.Execute(context.Background(), "task-ps", "powershell", map[string]any{
		"script": "Write-Output 'hi'",
	}, false, nil)
	if err == nil || !strings.Contains(err.Error(), "requires pwsh or powershell") {
		t.Fatalf("expected missing powershell error, got %v", err)
	}
}

func TestSSHTarget_POSIXUnsupportedModuleReturnsError(t *testing.T) {
	for _, module := range []string{"environment", "service"} {
		module := module
		t.Run(module, func(t *testing.T) {
			tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
			tgt.runner = &fakeSSHRunner{
				run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
					switch {
					case isEncodedPowerShellCommand(command):
						return "", "not found", 127, nil
					case command == "printf preflight":
						return "preflight", "", 0, nil
					default:
						t.Fatalf("unexpected command %q", command)
						return "", "", 0, nil
					}
				},
			}

			_, err := tgt.Execute(context.Background(), "task-1", module, map[string]any{}, false, nil)
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
			}, false, nil)
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
