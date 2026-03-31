package target

import (
	"context"
	"encoding/base64"
	"os"
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
	}, false)
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
