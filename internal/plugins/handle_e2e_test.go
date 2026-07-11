package plugins_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/plugins"
	"github.com/bluecadet/preflight/internal/target"
)

// pluginSourceDir is the standalone module that builds the test plugin.
const pluginSourceDir = "testdata/pluginhandle"

func buildTestPlugin(t *testing.T) string {
	return buildPlugin(t, pluginSourceDir)
}

func buildPlugin(t *testing.T, dir string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping plugin subprocess build in -short mode")
	}
	name := filepath.Base(dir)
	out := filepath.Join(t.TempDir(), exeName("preflight-plugin-"+name))
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build test plugin %s: %v\n%s", dir, err, out)
	}
	return out
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// localTargetWithPlugin builds a controller registry containing the test plugin
// and returns a LocalTarget bound to it.
func localTargetWithPlugin(t *testing.T) (*target.LocalTarget, string) {
	t.Helper()
	pluginPath := buildTestPlugin(t)
	reg := target.ModuleRegistry{
		"testhandle": plugins.NewModule("testhandle", pluginPath),
	}
	tgt := target.NewLocalTarget(reg)
	return tgt, pluginPath
}

func TestPluginHandle_AllOpsAgainstLocalTarget(t *testing.T) {
	tgt, _ := localTargetWithPlugin(t)
	defer func() { _ = tgt.Close() }()

	putPath := filepath.Join(t.TempDir(), "put-bytes")
	// Dry-run: Check runs all ops (RunCommand/PutFile/GetFile/Info) and returns
	// the ops-ok marker without applying.
	res, err := tgt.Execute(context.Background(), "task-1", "testhandle",
		map[string]any{"scenario": "ops", "put_path": putPath},
		target.ExecutionOptions{}, true, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.HasPrefix(res.Message, "ops-ok:") {
		t.Fatalf("Check message = %q, want ops-ok: prefix", res.Message)
	}
	if res.Status != target.StatusChanged {
		t.Errorf("status = %q, want changed", res.Status)
	}
	// PutFile wrote through the local handle backend.
	got, err := os.ReadFile(putPath)
	if err != nil {
		t.Fatalf("read put_path: %v", err)
	}
	if string(got) != "plugin-put-bytes" {
		t.Errorf("put file content = %q, want %q", got, "plugin-put-bytes")
	}
}

func TestPluginHandle_StreamingAgainstLocalTarget(t *testing.T) {
	tgt, _ := localTargetWithPlugin(t)
	defer func() { _ = tgt.Close() }()

	var lines []string
	_, err := tgt.Execute(context.Background(), "task-2", "testhandle",
		map[string]any{"scenario": "streaming"}, target.ExecutionOptions{}, true,
		func(line string) { lines = append(lines, line) })
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []string{"stream-1", "stream-2"}
	if len(lines) != len(want) {
		t.Fatalf("output lines = %v, want %v", lines, want)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
}

func TestPluginHandle_BecomeRefused(t *testing.T) {
	tgt, _ := localTargetWithPlugin(t)
	defer func() { _ = tgt.Close() }()

	_, err := tgt.Execute(context.Background(), "task-3", "testhandle",
		map[string]any{"scenario": "ops"}, target.ExecutionOptions{
			Become: &target.BecomeOptions{Enabled: true, User: "root"},
		}, false, nil)
	if err == nil {
		t.Fatal("expected plugin+become to be refused, got nil")
	}
	var mse *target.ModuleSupportError
	if !errors.As(err, &mse) {
		t.Fatalf("expected *ModuleSupportError, got %T: %v", err, err)
	}
	if mse.Class != target.ClassPluginBecome {
		t.Errorf("expected plugin_become class, got %q", mse.Class)
	}
	if mse.ReasonCode() != "plugin_become" {
		t.Errorf("expected reason plugin_become, got %q", mse.ReasonCode())
	}
}

func TestPluginHandle_ProtocolVersionRejection(t *testing.T) {
	// A pre-v1 plugin responds to initialize with name/version only (no
	// protocol_version). The v1 host must reject it with the plugin_protocol
	// class. Built as a real raw-JSON-RPC binary (not the v1 SDK).
	prev1Path := buildPlugin(t, "testdata/prev1plugin")
	reg := target.ModuleRegistry{
		"prev1": plugins.NewModule("prev1", prev1Path),
	}
	tgt := target.NewLocalTarget(reg)
	defer func() { _ = tgt.Close() }()

	_, err := tgt.Execute(context.Background(), "task-4", "prev1",
		map[string]any{}, target.ExecutionOptions{}, false, nil)
	if err == nil {
		t.Fatal("expected protocol error for pre-v1 plugin, got nil")
	}
	var mse *target.ModuleSupportError
	if !errors.As(err, &mse) || mse.Class != target.ClassPluginProtocol {
		t.Fatalf("expected plugin_protocol class, got %T: %v", err, err)
	}
	if mse.ReasonCode() != "plugin_protocol" {
		t.Errorf("reason = %q, want plugin_protocol", mse.ReasonCode())
	}
}
