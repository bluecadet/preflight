package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// logLevel returns the log level string for an event.
func logLevel(event Event) string {
	if l, ok := event.(Leveled); ok {
		return l.Level()
	}
	return "info"
}

// runLogMsg returns a short human-readable summary for the event.
func runLogMsg(event Event) string {
	if s, ok := event.(Summarizable); ok {
		return s.LogMessage()
	}
	return ""
}

// runLogEnvelope builds the standard envelope fields for a run-log JSON line.
type runLogEnvelope struct {
	Seq    int64   `json:"seq"`
	TS     string  `json:"ts"`
	Type   string  `json:"type"`
	Level  string  `json:"level"`
	RunID  string  `json:"run_id"`
	Target *string `json:"target"`
	TaskID *string `json:"task_id"`
	Msg    string  `json:"msg"`
}

// RunLogSink writes a sequential JSONL run log to disk.
// It implements the Renderer interface.
type RunLogSink struct {
	f       io.WriteCloser
	enc     *json.Encoder
	seq     int64
	runID   string
	dir     string
	summary *RunSummaryEvent
	now     func() time.Time
}

// NewRunLogSink creates a RunLogSink that writes to the given file path.
func NewRunLogSink(runID string, path string) (*RunLogSink, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create run log dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create run log: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return &RunLogSink{f: f, enc: enc, runID: runID, dir: dir, now: time.Now}, nil
}

// Emit writes one JSONL line for the event.
func (s *RunLogSink) Emit(event Event) {
	s.seq++
	ts := s.now().UTC().Format(time.RFC3339Nano)

	// Capture the run summary for final JSON output.
	if summary, ok := event.(RunSummaryEvent); ok {
		s.summary = &summary
	}

	// Extract target and task_id from the event.
	target, taskID := "", ""
	if c, ok := event.(Correlatable); ok {
		target, taskID = c.CorrelationIDs()
	}
	eventTypeStr := string(event.Type())
	msg := runLogMsg(event)
	level := logLevel(event)

	// Build the envelope.
	env := runLogEnvelope{
		Seq:    s.seq,
		TS:     ts,
		Type:   eventTypeStr,
		Level:  level,
		RunID:  s.runID,
		Target: nullableString(target),
		TaskID: nullableString(taskID),
		Msg:    msg,
	}

	// Build the full JSON object = envelope + type-specific fields.
	raw := s.buildJSON(event, env)
	if raw != nil {
		_ = s.enc.Encode(raw)
	}
}

// Close flushes and closes the log file, writing the final run.json summary.
func (s *RunLogSink) Close() {
	if s.f != nil {
		_ = s.f.Close()
	}
	if s.summary != nil && s.dir != "" {
		_ = s.writeRunJSON()
	}
}

// writeRunJSON atomically writes the final run summary to run.json.
func (s *RunLogSink) writeRunJSON() error {
	data, err := json.MarshalIndent(s.buildRunJSON(s.summary), "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, "run.json")
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// buildJSON merges the envelope with type-specific fields into a flat map. map.
func (s *RunLogSink) buildJSON(event Event, env runLogEnvelope) map[string]any {
	m := map[string]any{
		"seq":    env.Seq,
		"ts":     env.TS,
		"type":   env.Type,
		"level":  env.Level,
		"run_id": env.RunID,
		"msg":    env.Msg,
	}
	if env.Target != nil {
		m["target"] = *env.Target
	}
	if env.TaskID != nil {
		m["task_id"] = *env.TaskID
	}

	switch e := event.(type) {
	case VersionEvent:
		m["schema_version"] = e.SchemaVersion
		if e.PreflightVersion != "" {
			m["preflight_version"] = e.PreflightVersion
		}
		if e.PlaybookName != "" {
			m["playbook"] = e.PlaybookName
		}
	case RunStartEvent:
		if e.Mode != "" {
			m["mode"] = e.Mode
		}
		m["target_count"] = len(e.Targets)
		if len(e.Targets) > 0 {
			m["targets"] = e.Targets
		}
		m["dry_run"] = e.DryRun
		if len(e.Tags) > 0 {
			m["tags"] = e.Tags
		}
		if len(e.SkipTags) > 0 {
			m["skip_tags"] = e.SkipTags
		}
	case TargetStartEvent:
		m["transport"] = e.Transport
		if e.Address != "" {
			m["address"] = e.Address
		}
	case TargetCompleteEvent:
		m["outcome"] = e.Outcome
		m["elapsed_ms"] = e.ElapsedMs
		m["counts"] = map[string]int{
			"ok":      e.OKCount,
			"changed": e.ChangedCount,
			"failed":  e.FailedCount,
			"skipped": e.SkippedCount,
		}
		if e.WinRMRoundTrips > 0 {
			m["winrm_round_trips"] = e.WinRMRoundTrips
		}
	case TaskStartedEvent:
		m["name"] = e.TaskName
		if e.Module != "" {
			m["module"] = e.Module
		}
		if e.ActionPath != "" {
			m["action_ref"] = e.ActionPath
		}
	case TaskOKEvent:
		m["elapsed_ms"] = e.ElapsedMs
	case TaskChangedEvent:
		m["elapsed_ms"] = e.ElapsedMs
	case TaskSkippedEvent:
		m["reason"] = e.Reason
	case TaskFailedEvent:
		m["elapsed_ms"] = e.ElapsedMs
		if e.ExitCode != 0 {
			m["exit_code"] = e.ExitCode
		}
		if e.FailMessage != "" {
			m["error"] = e.FailMessage
		}
		if e.Reason != "" {
			m["reason"] = e.Reason
		}
		if len(e.Output) > 0 {
			m["output"] = e.Output
		}
		if e.ActionPath != "" {
			m["action_ref"] = e.ActionPath
		}
	case SupportGateEvent:
		m["runtime"] = e.Runtime
		if e.Reason != "" {
			m["reason"] = e.Reason
		}
		if len(e.Violations) > 0 {
			m["violations"] = e.Violations
		}
	case DiagnosticEvent:
		m["summary"] = e.Summary
		if e.Source != "" {
			m["source"] = e.Source
		}
	case RunSummaryEvent:
		m["status"] = e.Status
		m["tallies"] = e.TargetTallies
		m["counts"] = map[string]int{
			"ok":      e.OKCount,
			"changed": e.ChangedCount,
			"failed":  e.FailedCount,
			"skipped": e.SkippedCount,
		}
		m["elapsed_ms"] = e.ElapsedMs
	case TaskOutputEvent:
		m["lines"] = e.Lines
	case WarningEvent:
		m["message"] = e.Message
	case ActivityStartEvent:
		m["message"] = e.Message
	case ActivityResultEvent:
		m["message"] = e.Message
		m["status"] = e.Status
	case FactsEvent:
		m["facts"] = e.Facts
	case PlanEvent:
		m["play"] = e.PlaybookName
		m["tasks"] = e.Tasks
	case StateEvent:
		m["play"] = e.PlaybookName
		m["state_path"] = e.StatePath
		m["last_applied"] = e.LastApplied
		m["comparisons"] = e.Comparisons
	case ValidationEvent:
		m["play"] = e.PlaybookName
		m["playbook_path"] = e.PlaybookPath
		m["task_count"] = e.TaskCount
		m["visited_refs"] = e.VisitedRefCount
		m["resolved_refs"] = e.ResolvedRefs
		m["error_count"] = e.ErrorCount
	case ActionCatalogEvent:
		m["namespace"] = e.EmbeddedNamespace
		m["embedded_refs"] = e.EmbeddedRefs
		m["local_dir"] = e.LocalDir
		m["local_refs"] = e.LocalRefs
	case ActionInfoEvent:
		m["ref"] = e.Ref
		m["name"] = e.Name
		m["version"] = e.Version
		m["description"] = e.Description
		m["author"] = e.Author
		m["inputs"] = e.Inputs
		m["task_names"] = e.TaskNames
	case ActionFetchEvent:
		m["entries"] = e.Entries
	case PluginListEvent:
		m["plugins"] = e.Entries
	case InventoryListEvent:
		m["hosts"] = e.Hosts
	case SecretListEvent:
		m["secrets"] = e.Entries
	}

	return m
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// buildRunJSON creates the final summary JSON from the captured RunSummaryEvent.
func (s *RunLogSink) buildRunJSON(e *RunSummaryEvent) map[string]any {
	m := map[string]any{
		"status":     e.Status,
		"elapsed_ms": e.ElapsedMs,
		"ok":         e.OKCount,
		"changed":    e.ChangedCount,
		"failed":     e.FailedCount,
		"skipped":    e.SkippedCount,
		"run_id":     s.runID,
	}
	// Target tallies.
	m["tallies"] = map[string]int{
		"ok":          e.TargetTallies.OK,
		"failed":      e.TargetTallies.Failed,
		"unreachable": e.TargetTallies.Unreachable,
	}
	return m
}

// WriteStatusFile writes a status file in the run directory.
func WriteStatusFile(runDir, status string, rc int) error {
	if err := os.WriteFile(filepath.Join(runDir, "status"), []byte(strings.TrimSpace(status)+"\n"), 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, "rc"), fmt.Appendf(nil, "%d\n", rc), 0644)
}
