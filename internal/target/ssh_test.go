package target

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/tasklog"
)

type fakeSSHRunner struct {
	run func(context.Context, string, []byte) (string, string, int, error)
}

type taskLogSink struct {
	entries []tasklog.Entry
}

func (f *fakeSSHRunner) Run(ctx context.Context, command string, stdin []byte) (string, string, int, error) {
	return f.run(ctx, command, stdin)
}

func (s *taskLogSink) EmitTaskLog(entry tasklog.Entry) {
	s.entries = append(s.entries, entry)
}

func TestSSHTarget_ExecuteShell(t *testing.T) {
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"})
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			if !strings.Contains(command, `"echo" "hello"`) {
				t.Fatalf("unexpected command %q", command)
			}
			return "", "", 0, nil
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
		t.Fatalf("expected changed result, got %q", result.Status)
	}
}

func TestSSHTarget_CopyReadReachableAndInfo(t *testing.T) {
	var commands []string
	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"})
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, stdin []byte) (string, string, int, error) {
			commands = append(commands, command)
			switch {
			case strings.HasPrefix(command, "mkdir -p"):
				if decoded, err := base64.StdEncoding.DecodeString(string(stdin)); err != nil || string(decoded) != "hello" {
					t.Fatalf("unexpected stdin %q err=%v", string(stdin), err)
				}
				return "", "", 0, nil
			case strings.HasPrefix(command, "base64 <"):
				return base64.StdEncoding.EncodeToString([]byte("hello")), "", 0, nil
			case command == "echo preflight":
				return "preflight", "", 0, nil
			default:
				return "kiosk-a|Linux|x86_64", "", 0, nil
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
	if len(commands) < 4 {
		t.Fatalf("expected several SSH commands, got %d", len(commands))
	}
}

func TestSSHTarget_ExecuteEmitsBufferedTaskLogs(t *testing.T) {
	logs := &taskLogSink{}
	ctx := tasklog.WithTask(context.Background(), logs, tasklog.Entry{
		Target:   "host",
		TaskID:   "task-1",
		TaskName: "shell",
		Module:   "shell",
	})

	tgt := NewSSHTarget(SSHConfig{Host: "host", Username: "user"})
	tgt.runner = &fakeSSHRunner{
		run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
			if !strings.Contains(command, `"echo" "hello"`) {
				t.Fatalf("unexpected command %q", command)
			}
			return "hello\n", "warn\n", 0, nil
		},
	}

	if _, err := tgt.Execute(ctx, "task-1", "shell", map[string]any{
		"cmd":  "echo",
		"args": []any{"hello"},
	}, false); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(logs.entries) != 2 {
		t.Fatalf("expected 2 buffered SSH logs, got %d", len(logs.entries))
	}
	if logs.entries[0].Stream != "stdout" || logs.entries[0].Line != "hello" {
		t.Fatalf("unexpected first log entry: %#v", logs.entries[0])
	}
	if logs.entries[1].Stream != "stderr" || logs.entries[1].Line != "warn" {
		t.Fatalf("unexpected second log entry: %#v", logs.entries[1])
	}
}
