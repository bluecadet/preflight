package output

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRunLogGolden(t *testing.T) {
	t.Parallel()
	runLogGoldenTest(t, "run-log")
}

func TestRunLogGolden_Failure(t *testing.T) {
	t.Parallel()
	runLogGoldenTest(t, "run-log-failure")
}

func runLogGoldenTest(t *testing.T, name string) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.jsonl")

	sink, err := NewRunLogSink("golden-run", path)
	if err != nil {
		t.Fatalf("NewRunLogSink: %v", err)
	}

	var events []Event
	switch name {
	case "run-log":
		events = runLogFixtureHappy()
	case "run-log-failure":
		events = runLogFixtureFailure()
	default:
		t.Fatalf("unknown run log fixture: %s", name)
	}

	for _, event := range events {
		sink.Emit(event)
	}
	sink.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}

	got := normalizeRunLogJSONL(string(raw))

	goldenPath := filepath.Join("testdata", name+".golden")
	if os.Getenv("UPDATE_SNAPSHOTS") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", goldenPath, err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", goldenPath, err)
		}
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", goldenPath, err)
	}

	if got != string(want) {
		t.Fatalf("golden mismatch for %s:\nwant:\n%s\n\ngot:\n%s", goldenPath, string(want), got)
	}
}

func runLogFixtureHappy() []Event {
	return []Event{
		VersionEvent{
			SchemaVersion:    "1.0",
			PreflightVersion: "1.4.0",
			PlaybookName:     "kiosk-provision.yml",
		},
		RunStartEvent{
			Mode:         "apply",
			PlaybookPath: "kiosk-provision.yml",
			PlaybookName: "kiosk-provision",
			Targets:      []string{"kiosk-01"},
			DryRun:       false,
		},
		TargetStartEvent{
			Target:    "kiosk-01",
			Transport: "local",
		},
		TaskStartedEvent{
			Target:   "kiosk-01",
			TaskID:   "install-drivers",
			TaskName: "install display drivers",
			Module:   "command",
		},
		TaskOKEvent{
			Target:    "kiosk-01",
			TaskID:    "install-drivers",
			TaskName:  "install display drivers",
			ElapsedMs: 500,
		},
		TargetCompleteEvent{
			Target:       "kiosk-01",
			Outcome:      "ok",
			OKCount:      1,
			ChangedCount: 0,
			FailedCount:  0,
			SkippedCount: 0,
			ElapsedMs:    5000,
		},
		RunSummaryEvent{
			Status:        "success",
			OKCount:       1,
			ElapsedMs:     5000,
			TargetTallies: TargetCounts{OK: 1},
		},
	}
}

func runLogFixtureFailure() []Event {
	return []Event{
		VersionEvent{
			SchemaVersion:    "1.0",
			PreflightVersion: "1.4.0",
			PlaybookName:     "kiosk-provision.yml",
		},
		RunStartEvent{
			Mode:         "apply",
			PlaybookPath: "kiosk-provision.yml",
			PlaybookName: "kiosk-provision",
			Targets:      []string{"kiosk-01"},
			DryRun:       false,
		},
		TargetStartEvent{
			Target:    "kiosk-01",
			Transport: "local",
		},
		TaskStartedEvent{
			Target:   "kiosk-01",
			TaskID:   "install-drivers",
			TaskName: "install display drivers",
			Module:   "command",
		},
		TaskFailedEvent{
			Target:      "kiosk-01",
			TaskID:      "install-drivers",
			TaskName:    "install display drivers",
			ElapsedMs:   14500,
			ExitCode:    1,
			FailMessage: "DISM.exe failed: 0x800f0954",
			Output:      []string{"source files could not be downloaded", "check network connectivity"},
		},
		DiagnosticEvent{
			Target:  "kiosk-01",
			TaskID:  "install-drivers",
			Summary: "DISM.exe failed: 0x800f0954",
			Detail:  "source files could not be downloaded",
			Source:  "command",
		},
		TargetCompleteEvent{
			Target:       "kiosk-01",
			Outcome:      "failed",
			OKCount:      0,
			ChangedCount: 0,
			FailedCount:  1,
			SkippedCount: 0,
			ElapsedMs:    47000,
		},
		RunSummaryEvent{
			Status:        "failed",
			OKCount:       0,
			ElapsedMs:     47000,
			TargetTallies: TargetCounts{Failed: 1},
		},
	}
}

// normalizeRunLogJSONL normalizes timestamps (ts field) to a fixed value so
// golden comparisons are deterministic.
func normalizeRunLogJSONL(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, line := range lines {
		// Replace ts (RFC3339Nano) with a fixed placeholder.
		// TS is the 2nd field after seq in the envelope.
		lines[i] = tsRE.ReplaceAllString(line, `"ts":"<timestamp>"`)
	}
	return strings.Join(lines, "\n") + "\n"
}

var tsRE = regexp.MustCompile(`"ts":"[^"]+"`)
