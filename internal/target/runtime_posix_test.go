package target

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePOSIXBackend is a posixShellBackend stub for unit-testing the POSIX
// reboot/wait/user module logic against fakes. It supports two styles of
// canned responses so each test suite can pick the most convenient:
//
//   - run(command) (stdout, stderr, code, err): used by the reboot/wait
//     tests, which need to propagate errors (e.g. dropped connections) and
//     read the cached Probe signal.
//   - responder(command, stdin) (stdout, stderr, code): used by the user
//     tests, which inspect issued commands/stdins via the recorded slices
//     and do not need per-command errors.
//
// When run is set it takes precedence; otherwise responder is consulted;
// otherwise every command succeeds with empty output. Only RunPOSIXCommand
// and Probe are exercised by these paths; the rest return zero values.
type fakePOSIXBackend struct {
	mu sync.Mutex

	// run is the command handler. It receives the command string and returns
	// stdout, stderr, exit code, and err. When nil, every command succeeds
	// with empty output.
	run func(command string) (stdout, stderr string, code int, err error)

	// responder is the fallback command handler used when run is nil. It
	// returns stdout, stderr, exit code (err is always nil). It receives the
	// stdin bytes so chpasswd-style commands can be asserted.
	responder func(command string, stdin []byte) (stdout, stderr string, code int)

	// commands and stdins record every RunPOSIXCommand invocation, in order,
	// so tests can assert which commands were (or were not) issued.
	commands []string
	stdins   [][]byte

	// probe is the cached detection result returned by Probe().
	probe Probe
}

func (f *fakePOSIXBackend) RunPOSIXCommand(_ context.Context, command string, stdin []byte) (string, string, int, error) {
	f.mu.Lock()
	f.commands = append(f.commands, command)
	f.stdins = append(f.stdins, stdin)
	f.mu.Unlock()
	if f.run != nil {
		return f.run(command)
	}
	if f.responder != nil {
		stdout, stderr, code := f.responder(command, stdin)
		return stdout, stderr, code, nil
	}
	return "", "", 0, nil
}

func (f *fakePOSIXBackend) CopyFile(context.Context, string, string) error   { return nil }
func (f *fakePOSIXBackend) ReadFile(context.Context, string) ([]byte, error) { return nil, nil }
func (f *fakePOSIXBackend) PowerShellBinary() string                         { return "" }
func (f *fakePOSIXBackend) RunPowerShellScript(context.Context, string, OutputFunc) (string, error) {
	return "", nil
}
func (f *fakePOSIXBackend) Probe(context.Context) (Probe, error) { return f.probe, nil }
func (f *fakePOSIXBackend) PackageManager(context.Context) (string, error) {
	return f.probe.PackageManager, nil
}
func (f *fakePOSIXBackend) InitSystem() string { return f.probe.Init }

// ranCommand reports whether a command matching substr was issued.
func (f *fakePOSIXBackend) ranCommand(substr string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.commands {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

// --- wait: service_running -------------------------------------------------

func TestPOSIXWaitCondition_ServiceRunning_Active(t *testing.T) {
	backend := &fakePOSIXBackend{
		probe: Probe{Init: "systemd"},
		run: func(command string) (string, string, int, error) {
			if !strings.Contains(command, "systemctl is-active --quiet") {
				t.Fatalf("unexpected command: %q", command)
			}
			return "", "", 0, nil // active → exit 0
		},
	}
	met, err := posixWaitCondition(context.Background(), backend, "service_running", "nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !met {
		t.Fatal("expected met=true for active service")
	}
}

func TestPOSIXWaitCondition_ServiceRunning_Inactive(t *testing.T) {
	backend := &fakePOSIXBackend{
		probe: Probe{Init: "systemd"},
		run: func(string) (string, string, int, error) {
			return "", "inactive", 3, nil // non-zero → not running
		},
	}
	met, err := posixWaitCondition(context.Background(), backend, "service_running", "nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if met {
		t.Fatal("expected met=false for inactive service")
	}
}

func TestPOSIXWaitCondition_ServiceRunning_NoSystemd(t *testing.T) {
	backend := &fakePOSIXBackend{probe: Probe{Init: ""}}
	_, err := posixWaitCondition(context.Background(), backend, "service_running", "nginx")
	if err == nil {
		t.Fatal("expected error when init signal is empty")
	}
	var mse *ModuleSupportError
	if !errors.As(err, &mse) {
		t.Fatalf("expected *ModuleSupportError, got %T: %v", err, err)
	}
	if mse.Class != ClassMissingPrerequisite {
		t.Fatalf("class = %q, want %q", mse.Class, ClassMissingPrerequisite)
	}
}

// --- reboot: if_needed ----------------------------------------------------

func TestPOSIXRebootCheck_IfNeeded_AptMarkerPresent(t *testing.T) {
	backend := &fakePOSIXBackend{
		probe: Probe{Init: "systemd", PackageManager: "apt"},
		run: func(command string) (string, string, int, error) {
			if strings.Contains(command, "test -f /var/run/reboot-required") {
				return "", "", 0, nil // marker exists
			}
			t.Fatalf("unexpected command: %q", command)
			return "", "", 1, nil
		},
	}
	res, err := checkPOSIXReboot(context.Background(), backend, map[string]any{"condition": "if_needed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.NeedsChange {
		t.Fatal("expected NeedsChange=true when apt reboot-required marker present")
	}
}

func TestPOSIXRebootCheck_IfNeeded_AptMarkerAbsent(t *testing.T) {
	backend := &fakePOSIXBackend{
		probe: Probe{Init: "systemd", PackageManager: "apt"},
		run: func(command string) (string, string, int, error) {
			if strings.Contains(command, "test -f /var/run/reboot-required") {
				return "", "", 1, nil // marker absent
			}
			t.Fatalf("unexpected command: %q", command)
			return "", "", 1, nil
		},
	}
	res, err := checkPOSIXReboot(context.Background(), backend, map[string]any{"condition": "if_needed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NeedsChange {
		t.Fatal("expected NeedsChange=false when apt marker absent")
	}
	// apt's signal is the marker file; its absence means no reboot, not a
	// missing signal, so no special status message is emitted.
	if res.Message != "" {
		t.Fatalf("expected empty message for apt no-reboot, got %q", res.Message)
	}
}

func TestPOSIXRebootCheck_IfNeeded_DnfRebootRequired(t *testing.T) {
	backend := &fakePOSIXBackend{
		probe: Probe{Init: "systemd", PackageManager: "dnf"},
		run: func(command string) (string, string, int, error) {
			if strings.Contains(command, "test -f /var/run/reboot-required") {
				return "", "", 1, nil // marker absent
			}
			if strings.Contains(command, "needs-restarting -r") {
				return "", "", 1, nil // exit 1 → reboot required
			}
			t.Fatalf("unexpected command: %q", command)
			return "", "", 1, nil
		},
	}
	res, err := checkPOSIXReboot(context.Background(), backend, map[string]any{"condition": "if_needed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.NeedsChange {
		t.Fatal("expected NeedsChange=true when dnf needs-restarting says reboot required")
	}
}

func TestPOSIXRebootCheck_IfNeeded_DnfNoReboot(t *testing.T) {
	backend := &fakePOSIXBackend{
		probe: Probe{Init: "systemd", PackageManager: "dnf"},
		run: func(command string) (string, string, int, error) {
			if strings.Contains(command, "test -f /var/run/reboot-required") {
				return "", "", 1, nil // marker absent
			}
			if strings.Contains(command, "needs-restarting -r") {
				return "", "", 0, nil // exit 0 → no reboot
			}
			t.Fatalf("unexpected command: %q", command)
			return "", "", 1, nil
		},
	}
	res, err := checkPOSIXReboot(context.Background(), backend, map[string]any{"condition": "if_needed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NeedsChange {
		t.Fatal("expected NeedsChange=false when dnf says no reboot")
	}
}

// TestPOSIXRebootCheck_IfNeeded_DnfMarkerPlanted: the marker file is honored
// on dnf systems too, so the integration suite can plant/remove it on either
// container.
func TestPOSIXRebootCheck_IfNeeded_DnfMarkerPlanted(t *testing.T) {
	backend := &fakePOSIXBackend{
		probe: Probe{Init: "systemd", PackageManager: "dnf"},
		run: func(command string) (string, string, int, error) {
			if strings.Contains(command, "test -f /var/run/reboot-required") {
				return "", "", 0, nil // marker present
			}
			t.Fatalf("unexpected command: %q", command)
			return "", "", 1, nil
		},
	}
	res, err := checkPOSIXReboot(context.Background(), backend, map[string]any{"condition": "if_needed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.NeedsChange {
		t.Fatal("expected NeedsChange=true when marker planted on dnf")
	}
}

// TestPOSIXRebootCheck_IfNeeded_DnfNeedsRestartingUnavailable: needs-restarting
// absent (127) on a dnf system with no marker → no reboot, stated in output.
func TestPOSIXRebootCheck_IfNeeded_DnfNeedsRestartingUnavailable(t *testing.T) {
	backend := &fakePOSIXBackend{
		probe: Probe{Init: "systemd", PackageManager: "dnf"},
		run: func(command string) (string, string, int, error) {
			if strings.Contains(command, "test -f /var/run/reboot-required") {
				return "", "", 1, nil // marker absent
			}
			if strings.Contains(command, "needs-restarting -r") {
				return "", "command not found", 127, nil
			}
			t.Fatalf("unexpected command: %q", command)
			return "", "", 1, nil
		},
	}
	res, err := checkPOSIXReboot(context.Background(), backend, map[string]any{"condition": "if_needed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NeedsChange {
		t.Fatal("expected NeedsChange=false when no signal available")
	}
	if !strings.Contains(res.Message, "no reboot") {
		t.Fatalf("expected message stating no reboot, got %q", res.Message)
	}
}

// TestPOSIXRebootCheck_IfNeeded_NoPackageManager: neither apt nor dnf detected
// → no reboot signal available. Check returns NeedsChange=false and a message
// stating the situation so the task output says so.
func TestPOSIXRebootCheck_IfNeeded_NoPackageManager(t *testing.T) {
	backend := &fakePOSIXBackend{
		probe: Probe{Init: "systemd", PackageManager: ""},
		run: func(command string) (string, string, int, error) {
			if strings.Contains(command, "test -f /var/run/reboot-required") {
				return "", "", 1, nil // marker absent
			}
			t.Fatalf("unexpected command: %q", command)
			return "", "", 1, nil
		},
	}
	res, err := checkPOSIXReboot(context.Background(), backend, map[string]any{"condition": "if_needed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NeedsChange {
		t.Fatal("expected NeedsChange=false when no reboot signal available")
	}
	if !strings.Contains(res.Message, "no reboot") {
		t.Fatalf("expected message to state no reboot needed, got %q", res.Message)
	}
}

// --- reboot: always + reconnect -------------------------------------------

func TestPOSIXRebootCheck_Always(t *testing.T) {
	backend := &fakePOSIXBackend{probe: Probe{Init: "systemd"}}
	res, err := checkPOSIXReboot(context.Background(), backend, map[string]any{"condition": "always"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.NeedsChange {
		t.Fatal("expected NeedsChange=true for condition always")
	}
}

func TestPOSIXRebootCheck_NoSystemd(t *testing.T) {
	backend := &fakePOSIXBackend{probe: Probe{Init: ""}}
	_, err := checkPOSIXReboot(context.Background(), backend, map[string]any{"condition": "always"})
	if err == nil {
		t.Fatal("expected error when init signal is empty")
	}
	var mse *ModuleSupportError
	if !errors.As(err, &mse) {
		t.Fatalf("expected *ModuleSupportError, got %T: %v", err, err)
	}
	if mse.Class != ClassMissingPrerequisite {
		t.Fatalf("class = %q, want %q", mse.Class, ClassMissingPrerequisite)
	}
}

// TestPOSIXRebootApply_IssuesRebootAndReconnects: the backend records the
// `systemctl reboot` command, and the reconnect poll succeeds on the first
// attempt, so applyPOSIXReboot returns success without any real sleeping.
func TestPOSIXRebootApply_IssuesRebootAndReconnects(t *testing.T) {
	var rebootIssued bool
	backend := &fakePOSIXBackend{
		probe: Probe{Init: "systemd"},
		run: func(command string) (string, string, int, error) {
			if command == "systemctl reboot" {
				rebootIssued = true
				return "", "", 0, errors.New("connection lost") // host going down
			}
			if command == ":" {
				// Reconnected on the first poll.
				return "", "", 0, nil
			}
			return "", "", 0, nil
		},
	}
	res, err := applyPOSIXReboot(context.Background(), backend, map[string]any{"timeout": 300})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rebootIssued {
		t.Fatal("expected systemctl reboot to be issued")
	}
	if !strings.Contains(res.Message, "reconnected") {
		t.Fatalf("expected message mentioning reconnect, got %q", res.Message)
	}
}

// TestPOSIXRebootApply_NoSystemd: apply fails with the typed prerequisite
// error before issuing any command when no init system is detected.
func TestPOSIXRebootApply_NoSystemd(t *testing.T) {
	backend := &fakePOSIXBackend{
		probe: Probe{Init: ""},
		run: func(string) (string, string, int, error) {
			t.Fatal("no commands should run when no init system is detected")
			return "", "", 1, nil
		},
	}
	_, err := applyPOSIXReboot(context.Background(), backend, map[string]any{"condition": "always"})
	if err == nil {
		t.Fatal("expected error when init signal is empty")
	}
	var mse *ModuleSupportError
	if !errors.As(err, &mse) || mse.Class != ClassMissingPrerequisite {
		t.Fatalf("expected missing_prerequisite error, got %v", err)
	}
}

// TestPOSIXRebootReconnect_PollsUntilSuccess: the reconnect helper polls
// (failing) then succeeds once the host is back, with the injected clock and
// sleep so no real waiting occurs.
func TestPOSIXRebootReconnect_PollsUntilSuccess(t *testing.T) {
	back := false
	backend := &fakePOSIXBackend{
		run: func(command string) (string, string, int, error) {
			if command != ":" {
				t.Fatalf("unexpected command: %q", command)
			}
			if !back {
				return "", "", 0, errors.New("connection refused")
			}
			return "", "", 0, nil
		},
	}
	ticks := []time.Time{
		mustParseTime("12:00:00"), // deadline = 12:05
		mustParseTime("12:00:05"), // poll 1 fails
		mustParseTime("12:00:10"), // poll 2 fails
		mustParseTime("12:00:15"), // poll 3 succeeds
	}
	tick := 0
	now := func() time.Time {
		t := ticks[tick]
		if tick < len(ticks)-1 {
			tick++
		}
		return t
	}
	sleeps := 0
	sleep := func(time.Duration) {
		sleeps++
		if sleeps == 2 {
			back = true // host comes back after two failed polls
		}
	}
	err := posixRebootReconnect(context.Background(), backend, 300, now, sleep)
	if err != nil {
		t.Fatalf("expected reconnect to succeed, got %v", err)
	}
	if sleeps != 2 {
		t.Fatalf("expected 2 failed-poll sleeps before reconnect, got %d", sleeps)
	}
}

// TestPOSIXRebootReconnect_TimesOut: when the host never comes back, the
// helper returns a did-not-reconnect error once the deadline passes.
func TestPOSIXRebootReconnect_TimesOut(t *testing.T) {
	backend := &fakePOSIXBackend{
		run: func(string) (string, string, int, error) {
			return "", "", 0, errors.New("connection refused")
		},
	}
	// A 1-second timeout the fake clock steps past with each sleep.
	var ticks int
	now := func() time.Time {
		return mustParseTime("12:00:00").Add(time.Duration(ticks) * time.Second)
	}
	sleep := func(time.Duration) { ticks++ }
	err := posixRebootReconnect(context.Background(), backend, 1, now, sleep)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "did not reconnect") {
		t.Fatalf("expected did-not-reconnect error, got %v", err)
	}
}

func mustParseTime(hms string) time.Time {
	t, err := time.Parse("15:04:05", hms)
	if err != nil {
		panic(err)
	}
	return t
}
