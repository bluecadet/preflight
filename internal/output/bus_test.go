package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestBus_ScrubRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)

	bus := NewBus(r)
	bus.Scrub([]string{"my-secret-token", "sensitive-data"})

	bus.Emit(TaskFailedEvent{
		TaskID:      "t1",
		TaskName:    "test-task",
		FailMessage: "error using sensitive-data",
		Output:      []string{"login with my-secret-token", "normal line"},
	})
	bus.Close()

	out := buf.String()
	if strings.Contains(out, "my-secret-token") {
		t.Errorf("my-secret-token should be redacted, got: %q", out)
	}
	if strings.Contains(out, "sensitive-data") {
		t.Errorf("sensitive-data should be redacted, got: %q", out)
	}
	if !strings.Contains(out, "***") {
		t.Errorf("expected redaction markers (***) in output, got: %q", out)
	}
	if !strings.Contains(out, "normal line") {
		t.Errorf("expected non-secret content to remain, got: %q", out)
	}
}

func TestBus_FanOut(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	r1 := NewTextRendererWithOptions(&buf1, Options{Mode: "apply"})
	r2 := NewTextRendererWithOptions(&buf2, Options{Mode: "apply"})

	bus := NewBus(r1, r2)
	bus.Emit(RunStartEvent{
		Mode:         "apply",
		PlaybookPath: "test.yml",
		PlaybookName: "test",
		Targets:      []string{"local"},
	})
	// Single-target: header is buffered until TargetStartEvent.
	bus.Emit(TargetStartEvent{
		Target:    "local",
		Transport: "local",
	})
	bus.Close()

	out1 := buf1.String()
	out2 := buf2.String()
	if !strings.Contains(out1, "RUN") {
		t.Errorf("sink 1 expected RUN heading, got: %q", out1)
	}
	if !strings.Contains(out2, "RUN") {
		t.Errorf("sink 2 expected RUN heading, got: %q", out2)
	}
}

func TestRunLogSink_WritesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/run.jsonl"

	sink, err := NewRunLogSink("test-run-id", path)
	if err != nil {
		t.Fatalf("NewRunLogSink: %v", err)
	}

	// Emit the lifecycle events for a happy-path run.
	sink.Emit(VersionEvent{
		SchemaVersion:    "1.0",
		PreflightVersion: "1.4.0",
		PlaybookName:     "kiosk-provision.yml",
	})
	sink.Emit(RunStartEvent{
		Mode:         "apply",
		PlaybookPath: "kiosk-provision.yml",
		PlaybookName: "kiosk-provision",
		Targets:      []string{"kiosk-01"},
		DryRun:       false,
	})
	sink.Emit(TargetStartEvent{
		Target:    "kiosk-01",
		Transport: "local",
	})
	sink.Emit(TaskStartedEvent{
		Target:   "kiosk-01",
		TaskID:   "task-1",
		TaskName: "install display drivers",
		Module:   "command",
	})
	sink.Emit(TaskOKEvent{
		Target:    "kiosk-01",
		TaskID:    "task-1",
		TaskName:  "install display drivers",
		ElapsedMs: 500,
	})
	sink.Emit(TargetCompleteEvent{
		Target:       "kiosk-01",
		Outcome:      "ok",
		OKCount:      1,
		ChangedCount: 0,
		FailedCount:  0,
		SkippedCount: 0,
		ElapsedMs:    5000,
	})
	sink.Emit(RunSummaryEvent{
		Status:        "success",
		OKCount:       1,
		ElapsedMs:     5000,
		TargetTallies: TargetCounts{OK: 1},
	})
	sink.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 7 {
		t.Fatalf("expected 7 JSONL lines, got %d", len(lines))
	}

	// Verify envelope fields on every line.
	var lastSeq float64
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %d: invalid JSON: %v — %q", i, err, line)
		}
		// Check envelope fields exist.
		for _, field := range []string{"seq", "ts", "type", "level", "run_id", "msg"} {
			if _, ok := m[field]; !ok {
				t.Errorf("line %d: missing envelope field %q", i, field)
			}
		}
		// Check seq is monotonic.
		seq, ok := m["seq"].(float64)
		if !ok {
			t.Errorf("line %d: seq is not a number: %v", i, m["seq"])
		} else if seq <= lastSeq {
			t.Errorf("line %d: seq %v <= previous %v", i, seq, lastSeq)
		}
		lastSeq = seq
		// Check run_id is consistent.
		if runID, ok := m["run_id"].(string); ok && runID != "test-run-id" {
			t.Errorf("line %d: unexpected run_id %q", i, runID)
		}
	}

	// First line is version.
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("unmarshal first line: %v", err)
	}
	if first["type"] != "version" {
		t.Errorf("first line type=%q, want %q", first["type"], "version")
	}
	if first["schema_version"] != "1.0" {
		t.Errorf("schema_version=%q, want %q", first["schema_version"], "1.0")
	}
}

// allEventTypes returns a zero-value instance of every Event implementation.
func allEventTypes() []Event {
	return []Event{
		VersionEvent{},
		RunStartEvent{},
		TaskOutputEvent{},
		WarningEvent{},
		ActivityStartEvent{},
		ActivityResultEvent{},
		FactsEvent{},
		PlanEvent{},
		StateEvent{},
		ValidationEvent{},
		ActionCatalogEvent{},
		ActionInfoEvent{},
		ActionFetchEvent{},
		PluginListEvent{},
		InventoryListEvent{},
		SecretListEvent{},
		TargetStartEvent{},
		TargetCompleteEvent{},
		TaskStartedEvent{},
		TaskOKEvent{},
		TaskChangedEvent{},
		TaskSkippedEvent{},
		TaskFailedEvent{},
		DiagnosticEvent{},
		RunSummaryEvent{},
	}
}

// TestEventRedact_NoSentinelSurvives creates every event type with a sentinel
// secret planted in every string, []string, and map[string]any field, then
// asserts that Redact scrubs all occurrences.
func TestEventRedact_NoSentinelSurvives(t *testing.T) {
	sentinel := "SENTINEL_SECRET_XYZ"

	for _, e := range allEventTypes() {
		t.Run(fmt.Sprintf("%T", e), func(t *testing.T) {
			seeded := seedSentinel(e, sentinel)
			redacted := seeded.Redact([]string{sentinel})

			// Walk the redacted event's fields ensuring no sentinel survives.
			checkNoSentinel(t, "", reflect.ValueOf(redacted), sentinel)
		})
	}
}

// seedSentinel sets every string field to sentinel, every []string element to sentinel,
// and every map[string]any field to map[sentinel]sentinel.
func seedSentinel(e Event, sentinel string) Event {
	v := reflect.ValueOf(e)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	// Create a settable copy
	copy := reflect.New(v.Type()).Elem()
	copy.Set(v)

	for i := 0; i < v.NumField(); i++ {
		f := copy.Field(i)
		if !f.CanSet() {
			continue
		}
		switch f.Kind() {
		case reflect.String:
			f.SetString(sentinel)
		case reflect.Slice:
			if f.Type().Elem().Kind() == reflect.String {
				// []string slice
				s := make([]string, f.Len())
				for j := 0; j < f.Len(); j++ {
					s[j] = sentinel
				}
				f.Set(reflect.ValueOf(s))
			}
		case reflect.Map:
			if f.Type() == reflect.TypeFor[map[string]any]() {
				f.Set(reflect.ValueOf(map[string]any{sentinel: sentinel}))
			}
		}
	}
	return copy.Interface().(Event)
}

// checkNoSentinel walks a reflect.Value and fails if sentinel is found.
func checkNoSentinel(t *testing.T, prefix string, v reflect.Value, sentinel string) {
	t.Helper()

	switch v.Kind() {
	case reflect.String:
		if v.String() == sentinel {
			t.Errorf("%s: sentinel %q survived redaction", prefix, sentinel)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			item := v.Index(i)
			if item.Kind() == reflect.String && item.String() == sentinel {
				t.Errorf("%s[%d]: sentinel %q survived redaction", prefix, i, sentinel)
			}
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			val := v.MapIndex(key)
			if key.String() == sentinel {
				t.Errorf("%s[%q]: map key sentinel %q survived redaction", prefix, key.String(), sentinel)
			}
			if val.Kind() == reflect.String && val.String() == sentinel {
				t.Errorf("%s[%q]: sentinel %q survived redaction", prefix, key.String(), sentinel)
			}
			// Recurse into nested map values.
			if val.Kind() == reflect.Map || val.Kind() == reflect.Slice {
				checkNoSentinel(t, fmt.Sprintf("%s[%q]", prefix, key.String()), val, sentinel)
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			checkNoSentinel(t, fmt.Sprintf("%s.%s", prefix, field.Name), v.Field(i), sentinel)
		}
	}
}
