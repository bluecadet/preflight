package schema_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schemafiles "github.com/bluecadet/preflight/schema"
)

func TestRunLogSchemaCompiles(t *testing.T) {
	compileRunLogSchema(t)
}

func TestRunLogSchema_TypeCompleteFixtures(t *testing.T) {
	schema := compileRunLogSchema(t)

	for _, tc := range runLogFixtureCases() {
		t.Run(tc.name, func(t *testing.T) {
			// Each fixture is a complete JSON object (a JSONL line).
			var doc any
			if err := json.Unmarshal([]byte(tc.jsonl), &doc); err != nil {
				t.Fatalf("invalid fixture JSON: %v", err)
			}
			if err := schema.Validate(doc); err != nil {
				t.Fatalf("schema validation failed for %q:\n%v\ninput: %s", tc.name, err, tc.jsonl)
			}
		})
	}
}

func TestRunLogSchema_RejectsBadEvents(t *testing.T) {
	schema := compileRunLogSchema(t)

	tests := []struct {
		name  string
		jsonl string
	}{
		{
			name:  "missing required seq",
			jsonl: `{"ts":"2026-06-24T14:12:33.001Z","type":"version","level":"info","run_id":"x","msg":"test"}`,
		},
		{
			name:  "missing type",
			jsonl: `{"seq":1,"ts":"2026-06-24T14:12:33.001Z","level":"info","run_id":"x","msg":"test"}`,
		},
		{
			name:  "unknown event type",
			jsonl: `{"seq":1,"ts":"2026-06-24T14:12:33.001Z","type":"unknown_type","level":"info","run_id":"x","msg":"test"}`,
		},
		{
			name:  "version missing schema_version",
			jsonl: `{"seq":1,"ts":"2026-06-24T14:12:33.001Z","type":"version","level":"info","run_id":"x","target":null,"task_id":null,"msg":"preflight"}`,
		},
		{
			name:  "bad level value",
			jsonl: `{"seq":1,"ts":"2026-06-24T14:12:33.001Z","type":"version","level":"critical","run_id":"x","msg":"test","schema_version":"1.0"}`,
		},
		{
			name:  "seq not integer",
			jsonl: `{"seq":"one","ts":"2026-06-24T14:12:33.001Z","type":"version","level":"info","run_id":"x","msg":"test","schema_version":"1.0"}`,
		},
		{
			name:  "run_start missing target_count",
			jsonl: `{"seq":1,"ts":"2026-06-24T14:12:33.001Z","type":"run_start","level":"info","run_id":"x","msg":"1 target","targets":["a"],"dry_run":false}`,
		},
		{
			name:  "target_complete missing outcome",
			jsonl: `{"seq":1,"ts":"2026-06-24T14:12:33.001Z","type":"target_complete","level":"info","run_id":"x","target":"a","msg":"done","counts":{"ok":1,"changed":0,"failed":0,"skipped":0},"elapsed_ms":100}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var doc any
			if err := json.Unmarshal([]byte(tc.jsonl), &doc); err != nil {
				t.Fatalf("invalid fixture JSON: %v", err)
			}
			if err := schema.Validate(doc); err == nil {
				t.Fatalf("expected validation error for %q, but event was accepted", tc.name)
			}
		})
	}
}

// runLogFixtureCases returns one fixture per event type from the run-log catalog.
func runLogFixtureCases() []struct {
	name  string
	jsonl string
} {
	return []struct {
		name  string
		jsonl string
	}{
		{
			name:  "version-full",
			jsonl: `{"seq":1,"ts":"2026-06-24T14:12:33.001Z","type":"version","level":"info","run_id":"r01","target":null,"task_id":null,"msg":"preflight 1.4.0","schema_version":"1.0","preflight_version":"1.4.0","playbook":"kiosk-provision.yml"}`,
		},
		{
			name:  "version-minimal",
			jsonl: `{"seq":1,"ts":"2026-06-24T14:12:33.001Z","type":"version","level":"info","run_id":"r01","target":null,"task_id":null,"msg":"preflight","schema_version":"1.0"}`,
		},
		{
			name:  "run-start-full",
			jsonl: `{"seq":2,"ts":"2026-06-24T14:12:33.002Z","type":"run_start","level":"info","run_id":"r01","target":null,"task_id":null,"msg":"4 targets","target_count":4,"targets":["kiosk-01","kiosk-02","kiosk-03","kiosk-04"],"dry_run":false,"tags":[],"skip_tags":[]}`,
		},
		{
			name:  "run-start-minimal",
			jsonl: `{"seq":2,"ts":"2026-06-24T14:12:33.002Z","type":"run_start","level":"info","run_id":"r01","target":null,"task_id":null,"msg":"1 target","target_count":1,"targets":["kiosk-01"],"dry_run":true}`,
		},
		{
			name:  "target-start-local",
			jsonl: `{"seq":10,"ts":"2026-06-24T14:12:34.000Z","type":"target_start","level":"info","run_id":"r01","target":"kiosk-01","task_id":null,"msg":"connecting","transport":"local"}`,
		},
		{
			name:  "target-start-winrm",
			jsonl: `{"seq":11,"ts":"2026-06-24T14:12:34.100Z","type":"target_start","level":"info","run_id":"r01","target":"kiosk-03","task_id":null,"msg":"connecting","transport":"winrm","address":"10.0.0.1"}`,
		},
		{
			name:  "activity-start",
			jsonl: `{"seq":12,"ts":"2026-06-24T14:12:35.000Z","type":"activity_start","level":"info","run_id":"r01","target":"kiosk-01","task_id":null,"msg":"gathering facts","phase":"facts","activity_id":"act-01"}`,
		},
		{
			name:  "activity-end",
			jsonl: `{"seq":13,"ts":"2026-06-24T14:12:38.000Z","type":"activity_end","level":"info","run_id":"r01","target":"kiosk-01","task_id":null,"msg":"facts done","activity_id":"act-01","elapsed_ms":3000}`,
		},
		{
			name:  "task-started",
			jsonl: `{"seq":20,"ts":"2026-06-24T14:12:40.000Z","type":"task_started","level":"info","run_id":"r01","target":"kiosk-01","task_id":"drivers","msg":"install display drivers","name":"install display drivers","module":"command","action_ref":"preflight/windows-machine"}`,
		},
		{
			name:  "task-output-stdout",
			jsonl: `{"seq":21,"ts":"2026-06-24T14:12:41.000Z","type":"task_output","level":"info","run_id":"r01","target":"kiosk-01","task_id":"drivers","msg":"output","stream":"stdout","lines":["Downloading driver package...","Extracting..."]}`,
		},
		{
			name:  "task-output-stderr",
			jsonl: `{"seq":22,"ts":"2026-06-24T14:12:42.000Z","type":"task_output","level":"info","run_id":"r01","target":"kiosk-01","task_id":"drivers","msg":"stderr","stream":"stderr","lines":["warning: signature not verified"]}`,
		},
		{
			name:  "task-ok",
			jsonl: `{"seq":30,"ts":"2026-06-24T14:12:45.000Z","type":"task_ok","level":"info","run_id":"r01","target":"kiosk-01","task_id":"drivers","msg":"install display drivers ok","elapsed_ms":5000}`,
		},
		{
			name:  "task-changed",
			jsonl: `{"seq":31,"ts":"2026-06-24T14:12:46.000Z","type":"task_changed","level":"info","run_id":"r01","target":"kiosk-02","task_id":"set-wallpaper","msg":"set wallpaper changed","elapsed_ms":1200}`,
		},
		{
			name:  "task-skipped-tag-filtered",
			jsonl: `{"seq":32,"ts":"2026-06-24T14:12:47.000Z","type":"task_skipped","level":"info","run_id":"r01","target":"kiosk-02","task_id":"beta-feature","msg":"beta feature skipped","reason":"tag-filtered"}`,
		},
		{
			name:  "task-skipped-when-false",
			jsonl: `{"seq":33,"ts":"2026-06-24T14:12:48.000Z","type":"task_skipped","level":"info","run_id":"r01","target":"kiosk-02","task_id":"optional","msg":"optional skipped","reason":"when-condition-false"}`,
		},
		{
			name:  "task-failed",
			jsonl: `{"seq":40,"ts":"2026-06-24T14:13:05.900Z","type":"task_failed","level":"error","run_id":"r01","target":"kiosk-03","task_id":"drivers","msg":"install display drivers failed","elapsed_ms":14500,"exit_code":1}`,
		},
		{
			name:  "task-failed-minimal",
			jsonl: `{"seq":41,"ts":"2026-06-24T14:13:06.000Z","type":"task_failed","level":"error","run_id":"r01","target":"kiosk-03","task_id":"drivers","msg":"failed"}`,
		},
		{
			name:  "diagnostic",
			jsonl: `{"seq":42,"ts":"2026-06-24T14:13:06.001Z","type":"diagnostic","level":"error","run_id":"r01","target":"kiosk-03","task_id":"drivers","msg":"DISM failed","summary":"DISM.exe failed: 0x800f0954","detail":"source files could not be downloaded","source":"command"}`,
		},
		{
			name:  "target-unreachable",
			jsonl: `{"seq":50,"ts":"2026-06-24T14:13:10.000Z","type":"target_unreachable","level":"error","run_id":"r01","target":"kiosk-04","task_id":null,"msg":"connection refused","reason":"connection refused"}`,
		},
		{
			name:  "support-gate",
			jsonl: `{"seq":49,"ts":"2026-06-24T14:13:09.000Z","type":"support_gate","level":"error","run_id":"r01","target":"posix-host","task_id":null,"msg":"gate: 1 task(s) cannot run on this target (posix-shell)","runtime":"posix-shell","reason":"unsupported_on_runtime","violations":[{"task":"install tools","module":"registry","reason":"unsupported_on_runtime","message":"module \"registry\" is not supported on posix-shell (supported: windows-powershell)"}]}`,
		},
		{
			name:  "target-complete-ok",
			jsonl: `{"seq":60,"ts":"2026-06-24T14:13:20.000Z","type":"target_complete","level":"info","run_id":"r01","target":"kiosk-01","task_id":null,"msg":"ok","outcome":"ok","counts":{"ok":5,"changed":1,"failed":0,"skipped":2},"elapsed_ms":46000}`,
		},
		{
			name:  "target-complete-failed",
			jsonl: `{"seq":61,"ts":"2026-06-24T14:13:22.000Z","type":"target_complete","level":"info","run_id":"r01","target":"kiosk-03","task_id":null,"msg":"failed","outcome":"failed","counts":{"ok":18,"changed":0,"failed":1,"skipped":5},"elapsed_ms":47000}`,
		},
		{
			name:  "warning",
			jsonl: `{"seq":70,"ts":"2026-06-24T14:13:30.000Z","type":"warning","level":"warn","run_id":"r01","target":null,"task_id":null,"msg":"deprecated parameter used"}`,
		},
		{
			name:  "run-summary-success",
			jsonl: `{"seq":80,"ts":"2026-06-24T14:14:12.000Z","type":"run_summary","level":"info","run_id":"r01","target":null,"task_id":null,"msg":"success","status":"success","tallies":{"ok":2,"failed":0,"unreachable":0},"elapsed_ms":99000}`,
		},
		{
			name:  "run-summary-partial",
			jsonl: `{"seq":81,"ts":"2026-06-24T14:14:13.000Z","type":"run_summary","level":"info","run_id":"r01","target":null,"task_id":null,"msg":"2 ok, 1 failed, 1 unreachable","status":"partial","tallies":{"ok":2,"failed":1,"unreachable":1},"elapsed_ms":99000}`,
		},
		{
			name:  "run-summary-failed",
			jsonl: `{"seq":82,"ts":"2026-06-24T14:14:14.000Z","type":"run_summary","level":"info","run_id":"r01","target":null,"task_id":null,"msg":"all targets failed","status":"failed","tallies":{"ok":0,"failed":3,"unreachable":1},"elapsed_ms":120000}`,
		},
	}
}

func compileRunLogSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()

	raw, err := schemafiles.FS.ReadFile("runlog.schema.json")
	if err != nil {
		t.Fatalf("ReadFile(runlog.schema.json): %v", err)
	}

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("https://preflight.dev/schema/runlog.schema.json", doc); err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	schema, err := compiler.Compile("https://preflight.dev/schema/runlog.schema.json")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return schema
}

// TestRunLogSchema_ValidatesFixtureStream validates the example from run-log-format.md
// as a complete stream (every line validated).
func TestRunLogSchema_ValidatesFixtureStream(t *testing.T) {
	schema := compileRunLogSchema(t)

	// The example stream from .afk/docs/run-log-format.md
	stream := strings.TrimSpace(`
{"seq":1,"ts":"2026-06-24T14:12:33.001Z","type":"version","level":"info","run_id":"20260624-141233-9f2a","target":null,"task_id":null,"msg":"preflight 1.4.0","schema_version":"1.0","preflight_version":"1.4.0","playbook":"kiosk-provision.yml"}
{"seq":2,"ts":"2026-06-24T14:12:33.002Z","type":"run_start","level":"info","run_id":"20260624-141233-9f2a","target":null,"task_id":null,"msg":"4 targets","target_count":4,"targets":["kiosk-01","kiosk-02","kiosk-03","kiosk-04"],"dry_run":false,"tags":[],"skip_tags":[]}
{"seq":7,"ts":"2026-06-24T14:12:34.100Z","type":"target_start","level":"info","run_id":"20260624-141233-9f2a","target":"kiosk-03","task_id":null,"msg":"connecting","transport":"winrm","address":"[IP_ADDRESS]"}
{"seq":22,"ts":"2026-06-24T14:12:51.400Z","type":"task_started","level":"info","run_id":"20260624-141233-9f2a","target":"kiosk-03","task_id":"drivers","msg":"install display drivers","name":"install display drivers","module":"command","action_ref":"preflight/windows-machine"}
{"seq":24,"ts":"2026-06-24T14:13:05.900Z","type":"task_failed","level":"error","run_id":"20260624-141233-9f2a","target":"kiosk-03","task_id":"drivers","msg":"install display drivers failed","elapsed_ms":14500,"exit_code":1}
{"seq":25,"ts":"2026-06-24T14:13:05.901Z","type":"diagnostic","level":"error","run_id":"20260624-141233-9f2a","target":"kiosk-03","task_id":"drivers","msg":"DISM failed","summary":"DISM.exe failed: 0x800f0954","detail":"source files could not be downloaded ...","source":"command"}
{"seq":40,"ts":"2026-06-24T14:13:20.000Z","type":"target_complete","level":"info","run_id":"20260624-141233-9f2a","target":"kiosk-03","task_id":null,"msg":"failed","outcome":"failed","counts":{"ok":18,"changed":0,"failed":1,"skipped":5},"elapsed_ms":47000}
{"seq":58,"ts":"2026-06-24T14:14:12.000Z","type":"run_summary","level":"info","run_id":"20260624-141233-9f2a","target":null,"task_id":null,"msg":"2 ok, 1 failed, 1 unreachable","status":"partial","tallies":{"ok":2,"failed":1,"unreachable":1},"elapsed_ms":99000}
`)
	lines := strings.Split(stream, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var doc any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("line %d: invalid JSON: %v\n%s", i+1, err, line)
		}
		if err := schema.Validate(doc); err != nil {
			t.Fatalf("line %d: schema validation failed: %v\n%s", i+1, err, line)
		}
	}
}

// TestRunLogSchema_IgnoresUnknownFields ensures forward-compatibility with
// additive changes (consumers MUST ignore unknown fields).
func TestRunLogSchema_IgnoresUnknownFields(t *testing.T) {
	schema := compileRunLogSchema(t)

	// A version event with an extra unknown field should still validate.
	jsonl := `{"seq":1,"ts":"2026-06-24T14:12:33.001Z","type":"version","level":"info","run_id":"r01","target":null,"task_id":null,"msg":"preflight 1.5.0","schema_version":"1.0","preflight_version":"1.5.0","playbook":"test.yml","unknown_field":"should be ignored","extra_nested":{"a":1}}`

	var doc any
	if err := json.Unmarshal([]byte(jsonl), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if err := schema.Validate(doc); err != nil {
		t.Fatalf("schema rejected valid event with unknown fields: %v", err)
	}
}

func TestRunLogSchema_ValidatesEmittedRunLog(t *testing.T) {
	schema := compileRunLogSchema(t)

	// Validate the golden output from the existing RunLogSink emitter.
	// This ensures the schema agrees with what the running code actually produces.
	raw, err := os.ReadFile("../internal/output/testdata/run-log.golden")
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Restore the placeholder timestamp with a valid RFC3339 string
		// so the schema's date-time format check passes.
		line = strings.ReplaceAll(line, "\"ts\":\"<timestamp>\"", "\"ts\":\"2026-06-24T14:12:33.001Z\"")

		var doc any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("golden line %d: invalid JSON: %v\n%s", i+1, err, line)
		}
		if err := schema.Validate(doc); err != nil {
			t.Fatalf("golden line %d: schema validation failed: %v\n%s", i+1, err, line)
		}
	}
}
