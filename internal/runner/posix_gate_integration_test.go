//go:build integration

package runner

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/target"
)

// sshPOSIXConfigFromEnv builds an SSHConfig for a POSIX sacrificial target
// from the PREFLIGHT_TEST_SSH_POSIX_* env vars. Returns ok=false when any
// required var is missing or the host does not resolve, so callers can skip
// cleanly. Mirrors target.sshConfigFromEnv without importing the target test
// harness (the gate is runner orchestration; target cannot import runner).
func sshPOSIXConfigFromEnv() (target.SSHConfig, bool) {
	host := os.Getenv("PREFLIGHT_TEST_SSH_POSIX_HOST")
	user := os.Getenv("PREFLIGHT_TEST_SSH_POSIX_USER")
	pass := os.Getenv("PREFLIGHT_TEST_SSH_POSIX_PASS")
	if host == "" || user == "" || pass == "" {
		return target.SSHConfig{}, false
	}
	resolverCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if addrs, err := net.DefaultResolver.LookupHost(resolverCtx, host); err != nil || len(addrs) == 0 {
		return target.SSHConfig{}, false
	}
	port := 22
	if raw := os.Getenv("PREFLIGHT_TEST_SSH_POSIX_PORT"); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			port = p
		}
	}
	cfg := target.SSHConfig{
		Host:          host,
		Port:          port,
		Username:      user,
		Password:      pass,
		HostKeyPolicy: target.HostKeyPolicyInsecure,
	}
	if key := os.Getenv("PREFLIGHT_TEST_SSH_POSIX_KEY"); key != "" {
		cfg.PrivateKey = key
	}
	return cfg, true
}

// TestIntegration_POSIXSupportGateRefusesPreTask1 drives a full Run() against
// a real POSIX SSH target with a playbook containing a Windows-only module.
// The apply-start support gate must refuse the run after Info() resolves the
// posix-shell runtime and before task 1 executes, emitting a support_gate
// run-log event carrying the typed class and reason code.
func TestIntegration_POSIXSupportGateRefusesPreTask1(t *testing.T) {
	cfg, ok := sshPOSIXConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_SSH_POSIX_HOST / _USER / _PASS not set")
	}

	tgt := target.NewSSHTarget(cfg, nil)
	t.Cleanup(func() { _ = tgt.Close() })

	// registry is a catalog Windows-only module; it passes plan-time on SSH
	// (name-check only) but is unsupported on the posix-shell runtime the gate
	// resolves after Info().
	pb := &action.Playbook{
		Name: "posix-support-gate",
		Tasks: []action.Task{
			{Name: "windows-only task", ModuleName: "registry", ModuleParams: map[string]any{}},
		},
	}

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")
	sink, err := output.NewRunLogSink("posix-gate-run", logPath)
	if err != nil {
		t.Fatalf("NewRunLogSink: %v", err)
	}
	t.Cleanup(sink.Close)

	r := New(tgt, action.Chain{}, Config{
		Renderer:   sink,
		SkipFetch:  true,
		TargetName: "posix-target",
	})

	runErr := r.Run(context.Background(), pb)
	if runErr == nil {
		t.Fatal("expected the support gate to refuse the run, got nil error")
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")

	var gateLine string
	taskStarted := 0
	targetOutcome := ""
	for _, line := range lines {
		if strings.Contains(line, `"type":"support_gate"`) {
			gateLine = line
		}
		if strings.Contains(line, `"type":"task_started"`) {
			taskStarted++
		}
		if strings.Contains(line, `"type":"target_complete"`) {
			targetOutcome = line
		}
	}

	if gateLine == "" {
		t.Fatalf("no support_gate event in run log:\n%s", raw)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(gateLine), &doc); err != nil {
		t.Fatalf("support_gate line is not valid JSON: %v\n%s", err, gateLine)
	}
	if got := doc["reason"]; got != "unsupported_on_runtime" {
		t.Errorf("gate reason = %v, want unsupported_on_runtime", got)
	}
	if got := doc["runtime"]; got != "posix-shell" {
		t.Errorf("gate runtime = %v, want posix-shell", got)
	}
	violations, _ := doc["violations"].([]any)
	if len(violations) != 1 {
		t.Fatalf("violations = %d, want 1", len(violations))
	}
	first, _ := violations[0].(map[string]any)
	if first["module"] != "registry" {
		t.Errorf("violation module = %v, want registry", first["module"])
	}
	if first["reason"] != "unsupported_on_runtime" {
		t.Errorf("violation reason = %v, want unsupported_on_runtime", first["reason"])
	}
	if !strings.Contains(first["message"].(string), "not supported on posix-shell") {
		t.Errorf("violation message = %v, want unsupported-on-runtime wording", first["message"])
	}

	// Refused before task 1: no task ever started.
	if taskStarted != 0 {
		t.Errorf("expected 0 task_started events (refused pre-task-1), got %d", taskStarted)
	}
	// The target is marked failed.
	if !strings.Contains(targetOutcome, `"outcome":"failed"`) {
		t.Errorf("expected target_complete outcome=failed:\n%s", targetOutcome)
	}
}
