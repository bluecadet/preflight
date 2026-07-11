package target

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakePOSIXServiceBackend is a minimal posixShellBackend for unit-testing the
// POSIX service module's decision logic without a real target. It returns the
// canned active/enabled states for the systemctl is-active/is-enabled query and
// records every command issued so Apply assertions can inspect what converged.
type fakePOSIXServiceBackend struct {
	init         string
	activeState  string // returned by "systemctl is-active"
	enabledState string // returned by "systemctl is-enabled"
	commands     []string
	failNextWith string // when non-empty, the next RunPOSIXCommand returns this error message
}

func (b *fakePOSIXServiceBackend) RunPowerShellScript(context.Context, string, OutputFunc) (string, error) {
	return "", nil
}

func (b *fakePOSIXServiceBackend) RunPOSIXCommand(_ context.Context, command string, _ []byte) (string, string, int, error) {
	b.commands = append(b.commands, command)
	if b.failNextWith != "" {
		msg := b.failNextWith
		b.failNextWith = ""
		return "", "", 1, errString(msg)
	}
	// The state query command contains both is-active and is-enabled.
	if strings.Contains(command, "is-active") {
		return strings.TrimSpace(strings.Join([]string{
			"active=" + defaultStr(b.activeState, "unknown"),
			"enabled=" + defaultStr(b.enabledState, "unknown"),
		}, "\n")), "", 0, nil
	}
	return "", "", 0, nil
}

func (b *fakePOSIXServiceBackend) CopyFile(context.Context, string, string) error   { return nil }
func (b *fakePOSIXServiceBackend) ReadFile(context.Context, string) ([]byte, error) { return nil, nil }
func (b *fakePOSIXServiceBackend) PowerShellBinary() string                         { return "" }
func (b *fakePOSIXServiceBackend) InitSystem() string                               { return b.init }

func (b *fakePOSIXServiceBackend) Probe(context.Context) (Probe, error) {
	return Probe{Init: b.init}, nil
}
func (b *fakePOSIXServiceBackend) PackageManager(context.Context) (string, error) {
	return "", nil
}

func defaultStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

type errString string

func (e errString) Error() string { return string(e) }

func TestPOSIXService_RequiresSystemd(t *testing.T) {
	backend := &fakePOSIXServiceBackend{init: "", activeState: "inactive", enabledState: "disabled"}

	_, err := checkPOSIXService(context.Background(), backend, map[string]any{"name": "nginx", "state": "running"})
	if err == nil {
		t.Fatal("expected missing_prerequisite error when systemd is absent")
	}
	var mse *ModuleSupportError
	if !errors.As(err, &mse) || mse.Class != ClassMissingPrerequisite {
		t.Fatalf("expected missing_prerequisite, got %v", err)
	}
	if !strings.Contains(mse.Detail, "systemd") {
		t.Fatalf("error detail should name systemd, got %q", mse.Detail)
	}
}

func TestPOSIXService_NoParamsToConverge(t *testing.T) {
	backend := &fakePOSIXServiceBackend{init: "systemd"}
	res, err := checkPOSIXService(context.Background(), backend, map[string]any{"name": "nginx"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NeedsChange {
		t.Fatal("expected no change when neither state nor startup_type is set")
	}
}

func TestPOSIXService_StateAlreadyMatches(t *testing.T) {
	backend := &fakePOSIXServiceBackend{init: "systemd", activeState: "active", enabledState: "enabled"}
	res, err := checkPOSIXService(context.Background(), backend, map[string]any{"name": "nginx", "state": "running"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NeedsChange {
		t.Fatal("expected no change when service is already running")
	}
}

func TestPOSIXService_StateNeedsChange(t *testing.T) {
	backend := &fakePOSIXServiceBackend{init: "systemd", activeState: "inactive", enabledState: "enabled"}
	res, err := checkPOSIXService(context.Background(), backend, map[string]any{"name": "nginx", "state": "running"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.NeedsChange {
		t.Fatal("expected change when service is inactive but should be running")
	}
}

func TestPOSIXService_StartupTypeNeedsChange(t *testing.T) {
	backend := &fakePOSIXServiceBackend{init: "systemd", activeState: "active", enabledState: "disabled"}
	res, err := checkPOSIXService(context.Background(), backend, map[string]any{"name": "nginx", "startup_type": "automatic"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.NeedsChange {
		t.Fatal("expected change when startup_type is automatic but unit is disabled")
	}
}

func TestPOSIXService_DisabledStateNeedsChange(t *testing.T) {
	// state=disabled wants inactive + masked. A running, enabled unit needs change.
	backend := &fakePOSIXServiceBackend{init: "systemd", activeState: "active", enabledState: "enabled"}
	res, err := checkPOSIXService(context.Background(), backend, map[string]any{"name": "nginx", "state": "disabled"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.NeedsChange {
		t.Fatal("expected change for state=disabled when unit is active+enabled")
	}
}

func TestPOSIXService_DisabledStateAlreadyConverged(t *testing.T) {
	// state=disabled, unit already inactive + masked -> no change.
	backend := &fakePOSIXServiceBackend{init: "systemd", activeState: "inactive", enabledState: "masked"}
	res, err := checkPOSIXService(context.Background(), backend, map[string]any{"name": "nginx", "state": "disabled"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NeedsChange {
		t.Fatal("expected no change for state=disabled when already inactive+masked")
	}
}

func TestPOSIXService_ParamValidation(t *testing.T) {
	backend := &fakePOSIXServiceBackend{init: "systemd"}
	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"missing name", map[string]any{"state": "running"}, "name"},
		{"bad state", map[string]any{"name": "x", "state": "restarting"}, "state"},
		{"bad startup_type", map[string]any{"name": "x", "startup_type": "boot"}, "startup_type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := checkPOSIXService(context.Background(), backend, tc.params)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error mentioning %q, got %v", tc.want, err)
			}
		})
	}
}

func TestPOSIXService_ApplyDisabledStopsAndMasks(t *testing.T) {
	backend := &fakePOSIXServiceBackend{init: "systemd", activeState: "active", enabledState: "enabled"}
	if err := applyPOSIXService(context.Background(), backend, map[string]any{"name": "nginx", "state": "disabled"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(backend.commands, " || ")
	if !strings.Contains(joined, "systemctl stop") || !strings.Contains(joined, "systemctl mask") {
		t.Fatalf("expected stop + mask for state=disabled, got %v", backend.commands)
	}
	// state=disabled short-circuits: startup_type is ignored.
	for _, cmd := range backend.commands {
		if strings.Contains(cmd, "systemctl enable") || strings.Contains(cmd, "systemctl disable") {
			t.Fatalf("state=disabled should not issue enable/disable, got %q", cmd)
		}
	}
}

func TestPOSIXService_ApplyAutomaticStartupThenRunning(t *testing.T) {
	backend := &fakePOSIXServiceBackend{init: "systemd", activeState: "inactive", enabledState: "disabled"}
	if err := applyPOSIXService(context.Background(), backend, map[string]any{
		"name": "nginx", "state": "running", "startup_type": "automatic",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(backend.commands, " || ")
	if !strings.Contains(joined, "systemctl enable") {
		t.Fatalf("expected enable for startup_type=automatic, got %v", backend.commands)
	}
	if !strings.Contains(joined, "systemctl start") {
		t.Fatalf("expected start for state=running, got %v", backend.commands)
	}
}

func TestPOSIXService_ApplyMaskedStartupType(t *testing.T) {
	backend := &fakePOSIXServiceBackend{init: "systemd", activeState: "active", enabledState: "enabled"}
	if err := applyPOSIXService(context.Background(), backend, map[string]any{
		"name": "nginx", "startup_type": "disabled",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(backend.commands, " || ")
	if !strings.Contains(joined, "systemctl mask") {
		t.Fatalf("expected mask for startup_type=disabled, got %v", backend.commands)
	}
}

func TestPOSIXService_ApplyRequiresSystemd(t *testing.T) {
	backend := &fakePOSIXServiceBackend{init: "", activeState: "inactive", enabledState: "disabled"}
	err := applyPOSIXService(context.Background(), backend, map[string]any{"name": "nginx", "state": "running"})
	if err == nil {
		t.Fatal("expected missing_prerequisite error when systemd is absent")
	}
	var mse *ModuleSupportError
	if !errors.As(err, &mse) || mse.Class != ClassMissingPrerequisite {
		t.Fatalf("expected missing_prerequisite, got %v", err)
	}
}
