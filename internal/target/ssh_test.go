package target

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// fakePluggableModule lets ssh_test exercise the PluggableModule branch in
// SSHTarget.unsupportedModuleError without depending on the plugins package
// (which would create a target → plugins → target import cycle).
type fakePluggableModule struct{ path string }

func (fakePluggableModule) Check(context.Context, map[string]any, OutputFunc) (CheckResult, error) {
	return CheckResult{}, nil
}
func (fakePluggableModule) Apply(context.Context, map[string]any, OutputFunc) (ApplyResult, error) {
	return ApplyResult{}, nil
}
func (m fakePluggableModule) PluginPath() string  { return m.path }
func (m fakePluggableModule) CloneModule() Module { return m }

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
	}, ExecutionOptions{}, false, nil)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported error, got %v", err)
	}
}

func TestSSHTarget_PluginModulesDeferred(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, ModuleRegistry{
		"custom": fakePluggableModule{path: "/tmp/custom-plugin"},
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

	_, err := tgt.Execute(context.Background(), "task-plugin", "custom", nil, ExecutionOptions{}, false, nil)
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
	}, ExecutionOptions{}, false, nil)
	if err == nil || !strings.Contains(err.Error(), "requires pwsh or powershell") {
		t.Fatalf("expected missing powershell error, got %v", err)
	}
}

func TestSSHTarget_POSIXUnsupportedModuleReturnsError(t *testing.T) {
	for _, module := range []string{"environment", "service"} {
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

// closeCloser closes c, ignoring any error, when c is non-nil. Test mirror of
// the production closeAgent helper, used to release the SSH agent connection
// buildSSHClientConfig may return.
func closeCloser(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}

// TestDialSSHClient_HandshakeTimesOut verifies that dialSSHClient bounds the
// SSH handshake itself, not just the TCP connect. x/crypto/ssh's own
// ssh.Dial only applies config.Timeout to net.DialTimeout, leaving a
// stalled-but-connected remote (accepts the TCP connection, never speaks)
// able to hang the handshake forever; dialSSHConnBounded fixes this with an
// explicit conn deadline.
func TestDialSSHClient_HandshakeTimesOut(t *testing.T) {
	withSSHUserKeyDir(t, t.TempDir())

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()
	go func() {
		for {
			conn, acceptErr := l.Accept()
			if acceptErr != nil {
				return
			}
			// Accept but never write or close: simulates a stalled sshd
			// that never completes the version exchange/handshake. conn is
			// intentionally left open; l.Close() at test end is sufficient
			// cleanup for this short-lived test.
			_ = conn
		}
	}()

	host, portStr, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("Atoi: %v", err)
	}

	start := time.Now()
	_, err = dialSSHClient(SSHConfig{
		Host:          host,
		Port:          port,
		Username:      "user",
		Password:      "x",
		Timeout:       200 * time.Millisecond,
		HostKeyPolicy: HostKeyPolicyInsecure,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a handshake timeout error, got nil")
	}
	// Generous upper bound to avoid flakes while still proving the handshake
	// did not hang indefinitely (it would with unbounded ssh.Dial).
	if elapsed > 3*time.Second {
		t.Fatalf("expected the dial to fail quickly, took %s: %v", elapsed, err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "timeout") && !strings.Contains(msg, "deadline") {
		t.Fatalf("expected error to mention a timeout/deadline, got: %v", err)
	}
}

func TestBuildSSHClientConfig_DefaultsTimeoutTo30s(t *testing.T) {
	withSSHUserKeyDir(t, t.TempDir())
	cfg, closer, err := buildSSHClientConfig(SSHConfig{Host: "host", Username: "user", Password: "x"})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	if cfg.Timeout != defaultSSHTimeout {
		t.Fatalf("expected default timeout %s, got %s", defaultSSHTimeout, cfg.Timeout)
	}
}

func TestBuildSSHClientConfig_HonorsExplicitTimeout(t *testing.T) {
	withSSHUserKeyDir(t, t.TempDir())
	want := 5 * time.Second
	cfg, closer, err := buildSSHClientConfig(SSHConfig{Host: "host", Username: "user", Password: "x", Timeout: want})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	if cfg.Timeout != want {
		t.Fatalf("expected timeout %s, got %s", want, cfg.Timeout)
	}
}

// withSSHUserKeyDir overrides the package-level default-key-directory lookup
// for the duration of a test.
func withSSHUserKeyDir(t *testing.T, dir string) {
	t.Helper()
	orig := sshUserKeyDir
	sshUserKeyDir = func() string { return dir }
	t.Cleanup(func() { sshUserKeyDir = orig })
}

// withSSHAuthSock overrides the package-level SSH_AUTH_SOCK lookup for the
// duration of a test.
func withSSHAuthSock(t *testing.T, sock string) {
	t.Helper()
	orig := sshAuthSockEnv
	sshAuthSockEnv = func() string { return sock }
	t.Cleanup(func() { sshAuthSockEnv = orig })
}

// generateEncryptedTestKey returns a PEM-encoded ed25519 private key
// encrypted with the given passphrase.
func generateEncryptedTestKey(t *testing.T, passphrase string) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	if err != nil {
		t.Fatalf("MarshalPrivateKeyWithPassphrase: %v", err)
	}
	return pem.EncodeToMemory(block)
}

func TestBuildSSHClientConfig_EncryptedKeyWithCorrectPassphrase(t *testing.T) {
	withSSHAuthSock(t, "")
	withSSHUserKeyDir(t, t.TempDir())
	keyPEM := generateEncryptedTestKey(t, "s3cret-passphrase")

	cfg, closer, err := buildSSHClientConfig(SSHConfig{
		Host:                 "host",
		Username:             "user",
		PrivateKey:           string(keyPEM),
		PrivateKeyPassphrase: "s3cret-passphrase",
	})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	if len(cfg.Auth) != 1 {
		t.Fatalf("expected 1 auth method (PublicKeys), got %d", len(cfg.Auth))
	}
}

func TestBuildSSHClientConfig_EncryptedKeyWithoutPassphraseErrors(t *testing.T) {
	withSSHAuthSock(t, "")
	withSSHUserKeyDir(t, t.TempDir())
	keyPEM := generateEncryptedTestKey(t, "s3cret-passphrase")

	_, closer, err := buildSSHClientConfig(SSHConfig{
		Host:       "host",
		Username:   "user",
		PrivateKey: string(keyPEM),
	})
	defer closeCloser(closer)
	if err == nil {
		t.Fatal("expected error for encrypted key with no passphrase")
	}
	if !strings.Contains(err.Error(), "private_key_passphrase") {
		t.Fatalf("expected error to mention private_key_passphrase, got: %v", err)
	}
}

func TestBuildSSHClientConfig_DefaultKeyDiscoveryAddsAuthMethod(t *testing.T) {
	withSSHAuthSock(t, "")
	dir := t.TempDir()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519"), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	withSSHUserKeyDir(t, dir)

	cfg, closer, err := buildSSHClientConfig(SSHConfig{Host: "host", Username: "user"})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	if len(cfg.Auth) != 1 {
		t.Fatalf("expected 1 auth method from default key discovery, got %d", len(cfg.Auth))
	}
}

func TestBuildSSHClientConfig_NoAuthMethodsAvailableErrors(t *testing.T) {
	withSSHAuthSock(t, "")
	withSSHUserKeyDir(t, t.TempDir())

	_, closer, err := buildSSHClientConfig(SSHConfig{Host: "kiosk-01", Username: "user"})
	defer closeCloser(closer)
	if err == nil {
		t.Fatal("expected error when no authentication method is available")
	}
	if !strings.Contains(err.Error(), "no authentication method available for host kiosk-01") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// shortTempSockPath returns a path under a short-named temp dir for name.
// Unix socket paths are limited to ~104 bytes on macOS, and t.TempDir()
// embeds the full (often long) test name, so a dedicated short-named temp
// dir is used instead.
func shortTempSockPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "pf-ssh-sock")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

func TestBuildSSHClientConfig_AgentSocketDeadWithPasswordStillBuilds(t *testing.T) {
	withSSHAuthSock(t, shortTempSockPath(t, "does-not-exist.sock"))
	withSSHUserKeyDir(t, t.TempDir())

	cfg, closer, err := buildSSHClientConfig(SSHConfig{Host: "host", Username: "user", Password: "x"})
	defer closeCloser(closer)
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	if len(cfg.Auth) != 1 {
		t.Fatalf("expected 1 auth method (password), got %d", len(cfg.Auth))
	}
}

func TestBuildSSHClientConfig_AgentAddsAuthMethod(t *testing.T) {
	// Use a short-named temp dir (rather than t.TempDir(), whose path embeds
	// the full test name) since unix socket paths are limited to ~104 bytes
	// on macOS.
	dir, err := os.MkdirTemp("", "pf-ssh-agent")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "agent.sock")
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go agent.ServeAgent(agent.NewKeyring(), conn)
		}
	}()

	withSSHAuthSock(t, sockPath)
	withSSHUserKeyDir(t, t.TempDir())

	cfg, closer, err := buildSSHClientConfig(SSHConfig{Host: "host", Username: "user"})
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	if len(cfg.Auth) != 1 {
		t.Fatalf("expected 1 auth method (agent), got %d", len(cfg.Auth))
	}
	if closer == nil {
		t.Fatal("expected a non-nil closer for the dialed agent connection")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("closer.Close() returned unexpected error: %v", err)
	}
}

func TestBuildSSHClientConfig_AgentOnlyCandidateSurfacesDialError(t *testing.T) {
	withSSHAuthSock(t, shortTempSockPath(t, "does-not-exist.sock"))
	withSSHUserKeyDir(t, t.TempDir())

	_, closer, err := buildSSHClientConfig(SSHConfig{Host: "host", Username: "user"})
	defer closeCloser(closer)
	if err == nil {
		t.Fatal("expected error when the agent is the only auth candidate and dialing fails")
	}
	if !strings.Contains(err.Error(), "agent") {
		t.Fatalf("expected error to mention the agent, got: %v", err)
	}
}

// fakeKeepaliveConn is a stub sshKeepaliveConn used to drive sshKeepaliveLoop
// in isolation from a real *ssh.Client.
type fakeKeepaliveConn struct {
	mu       sync.Mutex
	requests int
	fail     bool
}

func (f *fakeKeepaliveConn) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if f.fail {
		return false, nil, fmt.Errorf("send request: connection reset")
	}
	return true, nil, nil
}

func (f *fakeKeepaliveConn) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func TestSSHKeepaliveLoop_SendsRequestsAtIntervalAndStopsOnClose(t *testing.T) {
	conn := &fakeKeepaliveConn{}
	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		sshKeepaliveLoop(conn, 5*time.Millisecond, stop, func() {
			t.Error("onRepeatedFailure should not be called when requests succeed")
		})
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for conn.count() < 3 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for keepalive requests, got %d", conn.count())
		case <-time.After(time.Millisecond):
		}
	}

	close(stop)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sshKeepaliveLoop to return after stop closed")
	}
}

func TestSSHKeepaliveLoop_ClosesClientAfterTwoConsecutiveFailures(t *testing.T) {
	conn := &fakeKeepaliveConn{fail: true}
	stop := make(chan struct{})
	done := make(chan struct{})

	var failureCalls atomic.Int64
	go func() {
		sshKeepaliveLoop(conn, 5*time.Millisecond, stop, func() {
			failureCalls.Add(1)
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sshKeepaliveLoop to return after repeated failure")
	}

	if got := failureCalls.Load(); got != 1 {
		t.Fatalf("expected onRepeatedFailure to be called exactly once, got %d", got)
	}
	if conn.count() < 2 {
		t.Fatalf("expected at least 2 keepalive attempts before giving up, got %d", conn.count())
	}
}

// fakeSSHConnCloser is a fake sshRunner that also implements sshCloser, used
// to test the reconnect path in SSHTarget.run.
type fakeSSHConnCloser struct {
	fakeSSHRunner
	closed atomic.Bool
}

func (f *fakeSSHConnCloser) Close() error {
	f.closed.Store(true)
	return nil
}

func TestSSHTarget_RunReconnectsOnConnectionError(t *testing.T) {
	first := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, io.EOF
		},
	}}
	second := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			if command != "echo hi" {
				t.Fatalf("unexpected command on reconnected runner: %q", command)
			}
			return "hi", "", 0, nil
		},
	}}

	var factoryCalls atomic.Int64
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = first
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		n := factoryCalls.Add(1)
		if n == 1 {
			return second, nil
		}
		t.Fatalf("unexpected extra runnerFactory call #%d", n)
		return nil, nil
	}

	stdout, _, code, err := tgt.run(context.Background(), "echo hi", nil)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if stdout != "hi" || code != 0 {
		t.Fatalf("unexpected result: stdout=%q code=%d", stdout, code)
	}
	if factoryCalls.Load() != 1 {
		t.Fatalf("expected runnerFactory to be called once for reconnect, got %d", factoryCalls.Load())
	}
	if !first.closed.Load() {
		t.Fatal("expected the dead runner to be closed on reconnect")
	}
	if tgt.runner != sshRunner(second) {
		t.Fatal("expected the cached runner to be the reconnected one")
	}
}

// TestSSHTarget_ReconnectAfterConcurrentClose covers the race where Close()
// nils the cached runner while a call is still in flight: reconnect must dial
// a fresh runner rather than returning the nil cached runner (which would
// panic in run's retry).
func TestSSHTarget_ReconnectAfterConcurrentClose(t *testing.T) {
	failed := &fakeSSHConnCloser{}
	fresh := &fakeSSHConnCloser{}

	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = nil // simulates Close() having run mid-call
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		return fresh, nil
	}

	runner, err := tgt.reconnect(failed)
	if err != nil {
		t.Fatalf("reconnect returned error: %v", err)
	}
	if runner == nil {
		t.Fatal("reconnect returned a nil runner")
	}
	if runner != sshRunner(fresh) {
		t.Fatal("expected reconnect to dial a fresh runner")
	}
}

func TestSSHTarget_RunDoesNotRetryOnNonConnectionError(t *testing.T) {
	var runCalls atomic.Int64
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			runCalls.Add(1)
			return "", "boom", 1, nil
		},
	}
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		t.Fatal("runnerFactory should not be called for a plain non-connection error")
		return nil, nil
	}

	_, stderr, code, err := tgt.run(context.Background(), "echo hi", nil)
	if err != nil {
		t.Fatalf("run returned unexpected error: %v", err)
	}
	if stderr != "boom" || code != 1 {
		t.Fatalf("unexpected result: stderr=%q code=%d", stderr, code)
	}
	if runCalls.Load() != 1 {
		t.Fatalf("expected exactly one Run call, got %d", runCalls.Load())
	}
}

func TestSSHTarget_RunDoesNotRetryOnContextCanceled(t *testing.T) {
	var factoryCalls atomic.Int64
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, context.Canceled
		},
	}
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		factoryCalls.Add(1)
		return nil, fmt.Errorf("should not be called")
	}

	_, _, _, err := tgt.run(context.Background(), "echo hi", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if factoryCalls.Load() != 0 {
		t.Fatalf("expected no reconnect attempt for a cancelled context, got %d factory calls", factoryCalls.Load())
	}
}

func TestSSHTarget_RunSurfacesErrorWhenReconnectAlsoFails(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = &fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, io.EOF
		},
	}
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		return nil, fmt.Errorf("connection refused")
	}

	_, _, _, err := tgt.run(context.Background(), "echo hi", nil)
	if err == nil {
		t.Fatal("expected an error when reconnect dialing fails")
	}
	if !strings.Contains(err.Error(), "reconnect") {
		t.Fatalf("expected error to mention reconnect, got: %v", err)
	}
}

func TestSSHTarget_RunSurfacesErrorWhenRetriedCallAlsoFails(t *testing.T) {
	first := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, io.EOF
		},
	}}
	second := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, io.EOF
		},
	}}

	var factoryCalls atomic.Int64
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = first
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		factoryCalls.Add(1)
		return second, nil
	}

	_, _, _, err := tgt.run(context.Background(), "echo hi", nil)
	if err == nil {
		t.Fatal("expected an error when the retried call also fails")
	}
	if factoryCalls.Load() != 1 {
		t.Fatalf("expected exactly one reconnect attempt (no retry loop), got %d", factoryCalls.Load())
	}
}

func TestSSHTarget_CloseClosesReconnectedRunner(t *testing.T) {
	first := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "", "", 0, io.EOF
		},
	}}
	second := &fakeSSHConnCloser{fakeSSHRunner: fakeSSHRunner{
		run: func(context.Context, string, []byte) (string, string, int, error) {
			return "ok", "", 0, nil
		},
	}}

	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"}, nil)
	tgt.runner = first
	tgt.runnerFactory = func(SSHConfig) (sshRunner, error) {
		return second, nil
	}

	if _, _, _, err := tgt.run(context.Background(), "echo hi", nil); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if err := tgt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !second.closed.Load() {
		t.Fatal("expected Close to close the reconnected runner")
	}
}

// TestIsSSHConnectionError covers each branch isSSHConnectionError checks,
// including wrapped variants and the deliberately-excluded context errors.
func TestIsSSHConnectionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "io.EOF", err: io.EOF, want: true},
		{name: "wrapped io.EOF", err: fmt.Errorf("read: %w", io.EOF), want: true},
		{name: "net.OpError", err: &net.OpError{Op: "read", Net: "tcp", Err: errors.New("boom")}, want: true},
		{name: "wrapped net.OpError", err: fmt.Errorf("run command: %w", &net.OpError{Op: "read", Net: "tcp", Err: errors.New("boom")}), want: true},
		{name: "ssh.ExitMissingError", err: &ssh.ExitMissingError{}, want: true},
		{name: "wrapped ssh.ExitMissingError", err: fmt.Errorf("run: %w", &ssh.ExitMissingError{}), want: true},
		{name: "closed network connection string", err: errors.New("read tcp: use of closed network connection"), want: true},
		{name: "ssh disconnect string", err: errors.New("ssh: disconnect, reason 2: connection lost"), want: true},
		{name: "context.Canceled", err: context.Canceled, want: false},
		{name: "wrapped context.Canceled", err: fmt.Errorf("run: %w", context.Canceled), want: false},
		{name: "context.DeadlineExceeded", err: context.DeadlineExceeded, want: false},
		{name: "wrapped context.DeadlineExceeded", err: fmt.Errorf("run: %w", context.DeadlineExceeded), want: false},
		{name: "unrelated exit status error", err: errors.New("exit status 1"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSSHConnectionError(tc.err); got != tc.want {
				t.Errorf("isSSHConnectionError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
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
