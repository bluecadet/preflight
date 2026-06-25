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

func runLogGoldenTest(t *testing.T, name string) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.jsonl")

	sink, err := NewRunLogSink("golden-run", path)
	if err != nil {
		t.Fatalf("NewRunLogSink: %v", err)
	}

	// Happy-path fixture: single target, one ok task.
	for _, event := range runLogFixture() {
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

func runLogFixture() []Event {
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
			Target:        "kiosk-01",
			Outcome:       "ok",
			OKCount:       1,
			ChangedCount:  0,
			FailedCount:   0,
			SkippedCount:  0,
			ElapsedMs:     5000,
		},
		RunSummaryEvent{
			Status:   "success",
			OKCount:  1,
			ElapsedMs: 5000,
			TargetTallies: TargetCounts{OK: 1},
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