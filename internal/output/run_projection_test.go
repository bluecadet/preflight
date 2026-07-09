package output

import (
	"testing"
	"time"
)

func TestRunProjection_RunStart(t *testing.T) {
	p := NewRunProjection()
	descs := p.Apply(RunStartEvent{
		PlaybookPath: "test.yml",
		PlaybookName: "test-play",
		Targets:      []string{"web-01"},
	})

	if !p.PlayStarted {
		t.Fatal("expected PlayStarted=true")
	}
	if p.Playbook != "test.yml" {
		t.Errorf("expected Playbook=test.yml, got %q", p.Playbook)
	}
	if p.PlayName != "test-play" {
		t.Errorf("expected PlayName=test-play, got %q", p.PlayName)
	}
	if len(p.Targets) != 1 || p.Targets[0] != "web-01" {
		t.Errorf("expected Targets=[web-01], got %v", p.Targets)
	}
	if p.Mode != "apply" {
		t.Errorf("expected Mode=apply, got %q", p.Mode)
	}

	if len(descs) != 1 {
		t.Fatalf("expected 1 descriptor, got %d", len(descs))
	}
	desc, ok := descs[0].(RunStartDescriptor)
	if !ok {
		t.Fatalf("expected RunStartDescriptor, got %T", descs[0])
	}
	if desc.PlaybookPath != "test.yml" {
		t.Errorf("expected PlaybookPath=test.yml, got %q", desc.PlaybookPath)
	}
	if len(desc.Targets) != 1 || desc.Targets[0] != "web-01" {
		t.Errorf("expected Targets=[web-01], got %v", desc.Targets)
	}
}

func TestRunProjection_RunStartIdempotent(t *testing.T) {
	p := NewRunProjection()
	p.Apply(RunStartEvent{PlaybookName: "first", Targets: []string{"host-a"}})
	descs := p.Apply(RunStartEvent{PlaybookName: "second", Targets: []string{"host-b"}})

	if p.PlayName != "first" {
		t.Errorf("expected PlayName=first (unchanged), got %q", p.PlayName)
	}
	if len(descs) != 0 {
		t.Errorf("expected 0 descriptors for second RunStart, got %d", len(descs))
	}
}

func TestRunProjection_TargetTransport(t *testing.T) {
	p := NewRunProjection()
	p.Apply(TargetStartEvent{Target: "host-a", Transport: "ssh", Address: "192.168.1.1"})
	p.Apply(TargetStartEvent{Target: "host-b", Transport: "winrm", Address: "192.168.1.2"})

	if got := p.TargetTransport("host-a"); got != "ssh" {
		t.Errorf("TargetTransport(host-a) = %q, want %q", got, "ssh")
	}
	if got := p.TargetTransport("host-b"); got != "winrm" {
		t.Errorf("TargetTransport(host-b) = %q, want %q", got, "winrm")
	}
	if got := p.TargetTransport("unknown"); got != "" {
		t.Errorf("TargetTransport(unknown) = %q, want %q", got, "")
	}
}

func TestRunProjection_TaskOK(t *testing.T) {
	p := NewRunProjection()
	p.Apply(TaskStartedEvent{Target: "host-a", TaskID: "t1", TaskName: "task-1"})
	descs := p.Apply(TaskOKEvent{Target: "host-a", TaskID: "t1", TaskName: "task-1", ElapsedMs: 100})

	if p.OkCount != 1 {
		t.Errorf("expected OkCount=1, got %d", p.OkCount)
	}
	if p.Total() != 1 {
		t.Errorf("expected Total=1, got %d", p.Total())
	}

	if len(descs) != 1 {
		t.Fatalf("expected 1 descriptor, got %d", len(descs))
	}
	desc, ok := descs[0].(TaskFinishedDescriptor)
	if !ok {
		t.Fatalf("expected TaskFinishedDescriptor, got %T", descs[0])
	}
	if desc.Status != "ok" {
		t.Errorf("expected Status=ok, got %q", desc.Status)
	}
	if desc.TaskName != "task-1" {
		t.Errorf("expected TaskName=task-1, got %q", desc.TaskName)
	}
	if desc.Elapsed != 100*time.Millisecond {
		t.Errorf("expected Elapsed=100ms, got %v", desc.Elapsed)
	}
}

func TestRunProjection_TaskChanged(t *testing.T) {
	p := NewRunProjection()
	p.Apply(TaskStartedEvent{Target: "host-a", TaskID: "t1", TaskName: "apply-config"})
	descs := p.Apply(TaskChangedEvent{Target: "host-a", TaskID: "t1", TaskName: "apply-config", ElapsedMs: 200})

	if p.ChangedCount != 1 {
		t.Errorf("expected ChangedCount=1, got %d", p.ChangedCount)
	}

	desc := descs[0].(TaskFinishedDescriptor)
	if desc.Status != "changed" {
		t.Errorf("expected Status=changed, got %q", desc.Status)
	}
}

func TestRunProjection_TaskSkipped(t *testing.T) {
	p := NewRunProjection()
	p.Apply(TaskStartedEvent{Target: "host-a", TaskID: "t1", TaskName: "optional"})
	descs := p.Apply(TaskSkippedEvent{Target: "host-a", TaskID: "t1", TaskName: "optional", Reason: "when-condition-false"})

	if p.SkippedCount != 1 {
		t.Errorf("expected SkippedCount=1, got %d", p.SkippedCount)
	}

	desc := descs[0].(TaskFinishedDescriptor)
	if desc.Status != "skipped" {
		t.Errorf("expected Status=skipped, got %q", desc.Status)
	}
	if desc.Message != "when-condition-false" {
		t.Errorf("expected Message=when-condition-false, got %q", desc.Message)
	}
}

func TestRunProjection_TaskFailed(t *testing.T) {
	p := NewRunProjection()
	p.Apply(TaskStartedEvent{Target: "host-a", TaskID: "t1", TaskName: "risky"})
	descs := p.Apply(TaskFailedEvent{
		Target:      "host-a",
		TaskID:      "t1",
		TaskName:    "risky",
		ElapsedMs:   5000,
		ExitCode:    1,
		FailMessage: "process exited with code 1",
		Output:      []string{"line1", "line2"},
	})

	if p.FailedCount != 1 {
		t.Errorf("expected FailedCount=1, got %d", p.FailedCount)
	}

	failedTasks := p.FailedTasks()
	if len(failedTasks) != 1 {
		t.Fatalf("expected 1 failedTask, got %d", len(failedTasks))
	}
	if failedTasks[0].name != "risky" {
		t.Errorf("expected failedTask name=risky, got %q", failedTasks[0].name)
	}
	if len(failedTasks[0].output) != 2 {
		t.Errorf("expected 2 output lines, got %d", len(failedTasks[0].output))
	}

	desc := descs[0].(TaskFinishedDescriptor)
	if desc.Status != "failed" {
		t.Errorf("expected Status=failed, got %q", desc.Status)
	}
	if desc.Message != "process exited with code 1" {
		t.Errorf("expected message, got %q", desc.Message)
	}
	if len(desc.Output) != 2 {
		t.Errorf("expected 2 output lines, got %d", len(desc.Output))
	}
}

func TestRunProjection_MultipleTaskStatuses(t *testing.T) {
	p := NewRunProjection()

	statuses := []struct {
		taskID   string
		taskName string
		status   string
		elapsed  int64
	}{
		{"t1", "task-ok", "ok", 100},
		{"t2", "task-changed", "changed", 200},
		{"t3", "task-failed", "failed", 300},
		{"t4", "task-skipped", "skipped", 0},
	}

	for _, s := range statuses {
		p.Apply(TaskStartedEvent{Target: "host-a", TaskID: s.taskID, TaskName: s.taskName})
	}

	for _, s := range statuses {
		switch s.status {
		case "ok":
			p.Apply(TaskOKEvent{Target: "host-a", TaskID: s.taskID, TaskName: s.taskName, ElapsedMs: s.elapsed})
		case "changed":
			p.Apply(TaskChangedEvent{Target: "host-a", TaskID: s.taskID, TaskName: s.taskName, ElapsedMs: s.elapsed})
		case "failed":
			p.Apply(TaskFailedEvent{Target: "host-a", TaskID: s.taskID, TaskName: s.taskName, ElapsedMs: s.elapsed, FailMessage: "error"})
		case "skipped":
			p.Apply(TaskSkippedEvent{Target: "host-a", TaskID: s.taskID, TaskName: s.taskName})
		}
	}

	if p.OkCount != 1 {
		t.Errorf("OkCount: expected 1, got %d", p.OkCount)
	}
	if p.ChangedCount != 1 {
		t.Errorf("ChangedCount: expected 1, got %d", p.ChangedCount)
	}
	if p.FailedCount != 1 {
		t.Errorf("FailedCount: expected 1, got %d", p.FailedCount)
	}
	if p.SkippedCount != 1 {
		t.Errorf("SkippedCount: expected 1, got %d", p.SkippedCount)
	}
	if p.Total() != 4 {
		t.Errorf("Total: expected 4, got %d", p.Total())
	}
}

func TestRunProjection_ActiveTasks(t *testing.T) {
	p := NewRunProjection()

	if p.RunningTaskCount() != 0 {
		t.Errorf("expected 0 active tasks initially, got %d", p.RunningTaskCount())
	}

	p.Apply(TaskStartedEvent{Target: "host-a", TaskID: "t1", TaskName: "task-1"})
	if p.RunningTaskCount() != 1 {
		t.Errorf("expected 1 active task, got %d", p.RunningTaskCount())
	}

	p.Apply(TaskStartedEvent{Target: "host-b", TaskID: "t2", TaskName: "task-2"})
	if p.RunningTaskCount() != 2 {
		t.Errorf("expected 2 active tasks, got %d", p.RunningTaskCount())
	}

	// Complete one task — it should be removed from active.
	p.Apply(TaskOKEvent{Target: "host-a", TaskID: "t1", TaskName: "task-1", ElapsedMs: 50})
	if p.RunningTaskCount() != 1 {
		t.Errorf("expected 1 active task after completion, got %d", p.RunningTaskCount())
	}

	running := p.OrderedRunningTasks()
	if len(running) != 1 {
		t.Fatalf("expected 1 running task, got %d", len(running))
	}
	if running[0].id != "t2" {
		t.Errorf("expected remaining task id=t2, got %q", running[0].id)
	}
}

func TestRunProjection_TaskOutputTruncation(t *testing.T) {
	p := NewRunProjection()
	p.Apply(TaskStartedEvent{Target: "host-a", TaskID: "t1", TaskName: "stream"})
	p.Apply(TaskOutputEvent{
		Target: "host-a",
		TaskID: "t1",
		Lines:  []string{"line1", "line2", "line3", "line4", "line5"},
	})

	running := p.OrderedRunningTasks()
	if len(running) != 1 {
		t.Fatalf("expected 1 running task, got %d", len(running))
	}
	recent := running[0].recentLines
	if len(recent) != maxTaskPreviewLines {
		t.Fatalf("expected %d recent lines, got %d: %v", maxTaskPreviewLines, len(recent), recent)
	}
	// Should keep the last 3 lines.
	expected := []string{"line3", "line4", "line5"}
	for i, line := range expected {
		if recent[i] != line {
			t.Errorf("recentLines[%d] = %q, want %q", i, recent[i], line)
		}
	}
}

func TestRunProjection_AlertDetection(t *testing.T) {
	p := NewRunProjection()
	p.Apply(TaskStartedEvent{Target: "host-a", TaskID: "t1", TaskName: "monitor"})

	// A line containing keywords like "error" should set the alert flag.
	p.Apply(TaskOutputEvent{
		Target: "host-a",
		TaskID: "t1",
		Lines:  []string{"something went wrong: stderr output"},
	})

	running := p.OrderedRunningTasks()
	if len(running) != 1 {
		t.Fatalf("expected 1 running task, got %d", len(running))
	}
	if !running[0].alert {
		t.Error("expected alert=true after stderr keyword")
	}
}

func TestRunProjection_Warning(t *testing.T) {
	p := NewRunProjection()
	descs := p.Apply(WarningEvent{Message: "deprecated feature"})

	if p.WarningCount != 1 {
		t.Errorf("expected WarningCount=1, got %d", p.WarningCount)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 descriptor, got %d", len(descs))
	}
	_, ok := descs[0].(WarningDescriptor)
	if !ok {
		t.Fatalf("expected WarningDescriptor, got %T", descs[0])
	}
}

func TestRunProjection_StaticCards(t *testing.T) {
	tests := []struct {
		name string
		kind string
		evt  Event
	}{
		{"FactsEvent", "facts", FactsEvent{Target: "host-a", Facts: map[string]any{"k": "v"}}},
		{"PlanEvent", "plan", PlanEvent{Target: "host-a", Tasks: []PlanTaskEntry{}}},
		{"StateEvent", "state", StateEvent{Target: "host-a", StatePath: "state.json"}},
		{"ValidationEvent", "validate", ValidationEvent{PlaybookPath: "test.yml"}},
		{"ActionCatalogEvent", "action_catalog", ActionCatalogEvent{EmbeddedRefs: []string{"preflight/test"}}},
		{"ActionInfoEvent", "action_info", ActionInfoEvent{Ref: "test"}},
		{"ActionFetchEvent", "action_fetch", ActionFetchEvent{Entries: []ActionFetchEntry{{Ref: "test", SHA: "abc123"}}}},
	}

	p := NewRunProjection()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			descs := p.Apply(tc.evt)
			if len(descs) != 1 {
				t.Fatalf("expected 1 descriptor, got %d", len(descs))
			}
			card, ok := descs[0].(CardDescriptor)
			if !ok {
				t.Fatalf("expected CardDescriptor, got %T", descs[0])
			}
			if card.Kind != tc.kind {
				t.Errorf("expected Kind=%q, got %q", tc.kind, card.Kind)
			}
		})
	}
}

func TestRunProjection_Activities(t *testing.T) {
	p := NewRunProjection()

	descs := p.Apply(ActivityStartEvent{Target: "host-a", Message: "connecting"})
	if !p.HadActivity {
		t.Error("expected HadActivity=true")
	}
	if len(p.OrderedActivities()) != 1 {
		t.Errorf("expected 1 activity, got %d", len(p.OrderedActivities()))
	}
	if len(descs) != 0 {
		t.Errorf("expected 0 descriptors from ActivityStart, got %d", len(descs))
	}

	// Duplicate activity should be ignored.
	p.Apply(ActivityStartEvent{Target: "host-a", Message: "connecting"})
	if len(p.OrderedActivities()) != 1 {
		t.Errorf("expected still 1 activity after duplicate, got %d", len(p.OrderedActivities()))
	}

	// Activity result removes it.
	descs = p.Apply(ActivityResultEvent{Target: "host-a", Message: "connecting", Status: "ok"})
	if len(p.OrderedActivities()) != 0 {
		t.Errorf("expected 0 activities after result, got %d", len(p.OrderedActivities()))
	}
	if len(descs) != 0 {
		t.Errorf("expected 0 descriptors from ActivityResult, got %d", len(descs))
	}
}

func TestRunProjection_TargetComplete(t *testing.T) {
	p := NewRunProjection()
	descs := p.Apply(TargetCompleteEvent{Target: "host-a", Outcome: "ok"})
	done, failed := p.TargetCounts()

	if done != 1 {
		t.Errorf("expected 1 done target, got %d", done)
	}
	if failed != 0 {
		t.Errorf("expected 0 failed targets, got %d", failed)
	}
	if len(descs) != 0 {
		t.Errorf("expected 0 descriptors from TargetComplete, got %d", len(descs))
	}

	p.Apply(TargetCompleteEvent{Target: "host-b", Outcome: "failed"})
	done, failed = p.TargetCounts()
	if done != 1 {
		t.Errorf("expected 1 done target, got %d", done)
	}
	if failed != 1 {
		t.Errorf("expected 1 failed target, got %d", failed)
	}
}

func TestRunProjection_ShouldShowHostLabels(t *testing.T) {
	p := NewRunProjection()
	// Before RunStart, always show labels.
	if !p.ShouldShowHostLabels() {
		t.Error("expected ShouldShowHostLabels=true before RunStart")
	}

	p.Apply(RunStartEvent{Targets: []string{"single-host"}})
	if p.ShouldShowHostLabels() {
		t.Error("expected ShouldShowHostLabels=false for single target")
	}

	// Create a new projection for multi-target case (RunStart is idempotent).
	p2 := NewRunProjection()
	p2.Apply(RunStartEvent{Targets: []string{"host-a", "host-b"}})
	if !p2.ShouldShowHostLabels() {
		t.Error("expected ShouldShowHostLabels=true for multiple targets")
	}
}

func TestRunProjection_DisplayTarget(t *testing.T) {
	p := NewRunProjection()
	if p.DisplayTarget("") != "local" {
		t.Errorf("expected DisplayTarget('')='local', got %q", p.DisplayTarget(""))
	}
	if p.DisplayTarget("host-a") != "host-a" {
		t.Errorf("expected DisplayTarget('host-a')='host-a', got %q", p.DisplayTarget("host-a"))
	}

	// Single local target — map "localhost" to "local".
	p.Apply(RunStartEvent{Targets: []string{"local"}})
	if p.DisplayTarget("localhost") != "local" {
		t.Errorf("expected DisplayTarget('localhost')='local' for single local target, got %q", p.DisplayTarget("localhost"))
	}
}

func TestRunProjection_IsCheckMode(t *testing.T) {
	p := NewRunProjection()
	// Default is apply mode.
	if p.IsCheckMode() {
		t.Error("expected IsCheckMode=false by default")
	}

	p.Apply(RunStartEvent{Mode: "check"})
	if !p.IsCheckMode() {
		t.Error("expected IsCheckMode=true after check RunStart")
	}
}

func TestRunProjection_ActiveTargetCount(t *testing.T) {
	p := NewRunProjection()
	p.Apply(TaskStartedEvent{Target: "host-a", TaskID: "t1", TaskName: "task"})
	p.Apply(ActivityStartEvent{Target: "host-b", Message: "connecting"})

	if p.ActiveTargetCount() != 2 {
		t.Errorf("expected ActiveTargetCount=2, got %d", p.ActiveTargetCount())
	}
}

func TestRunProjection_ElapsedZeroWhenNotStarted(t *testing.T) {
	p := NewRunProjection()
	if p.Elapsed() != 0 {
		t.Errorf("expected zero elapsed before RunStart, got %v", p.Elapsed())
	}
}

func TestRunProjection_TaskForEmptyTargetOrTaskID(t *testing.T) {
	p := NewRunProjection()
	// TaskStarted with empty target should be a no-op.
	p.Apply(TaskStartedEvent{Target: "", TaskID: "t1", TaskName: "no-op"})
	if p.RunningTaskCount() != 0 {
		t.Errorf("expected 0 active tasks for empty target, got %d", p.RunningTaskCount())
	}

	// TaskOutput with empty target should be a no-op.
	p.Apply(TaskStartedEvent{Target: "host-a", TaskID: "t1", TaskName: "real"})
	p.Apply(TaskOutputEvent{Target: "", TaskID: "", Lines: []string{"data"}})
	if p.RunningTaskCount() != 1 {
		t.Errorf("expected still 1 active task after empty TaskOutput, got %d", p.RunningTaskCount())
	}
}

func TestRunProjection_ApplyUnsupportedEventType(t *testing.T) {
	p := NewRunProjection()
	descs := p.Apply(VersionEvent{SchemaVersion: "1.0", PreflightVersion: "0.1.0", PlaybookName: "test"})
	if len(descs) != 0 {
		t.Errorf("expected 0 descriptors for unsupported event, got %d", len(descs))
	}
}

func TestRunProjection_ActionPathPreservedOnTaskCompletion(t *testing.T) {
	p := NewRunProjection()
	p.Apply(TaskStartedEvent{Target: "host-a", TaskID: "t1", TaskName: "task", ActionPath: "preflight/kiosk/configure"})
	descs := p.Apply(TaskOKEvent{Target: "host-a", TaskID: "t1", TaskName: "task", ElapsedMs: 100})

	desc := descs[0].(TaskFinishedDescriptor)
	if desc.ActionPath != "preflight/kiosk/configure" {
		t.Errorf("expected ActionPath=preflight/kiosk/configure, got %q", desc.ActionPath)
	}
}

func TestRunProjection_VisibleLiveEntries(t *testing.T) {
	tasks := []*activeTask{
		{id: "t1", name: "task-1"},
		{id: "t2", name: "task-2"},
		{id: "t3", name: "task-3"},
	}
	activities := []*activeActivity{
		{key: "a1", message: "connecting"},
		{key: "a2", message: "gathering facts"},
	}

	// Both activities visible, only 1 task visible; 2 hidden.
	visibleActs, visibleTasks, hidden := visibleLiveEntries(activities, tasks, 3)
	if len(visibleActs) != 2 {
		t.Errorf("expected 2 visible activities, got %d", len(visibleActs))
	}
	if len(visibleTasks) != 1 {
		t.Errorf("expected 1 visible task, got %d", len(visibleTasks))
	}
	if hidden != 2 {
		t.Errorf("expected 2 hidden, got %d", hidden)
	}

	// Activities alone exceed the limit.
	_, _, hidden = visibleLiveEntries(activities, tasks, 1)
	if hidden != 4 { // 1 activity hidden + all 3 tasks
		t.Errorf("expected 4 hidden with limit=1, got %d", hidden)
	}
}

func TestRunProjection_NewRunProjectionWithOptions(t *testing.T) {
	p := NewRunProjectionWithOptions(Options{Mode: "check", RunDir: "/tmp/runs/123"})
	if p.Mode != "check" {
		t.Errorf("expected Mode=check, got %q", p.Mode)
	}
	if p.RunDir != "/tmp/runs/123" {
		t.Errorf("expected RunDir=/tmp/runs/123, got %q", p.RunDir)
	}
}

func TestRunProjection_RemoveOrderedValue(t *testing.T) {
	vals := []string{"a", "b", "c"}
	result := removeOrderedValue(vals, "b")
	if len(result) != 2 || result[0] != "a" || result[1] != "c" {
		t.Errorf("expected [a c], got %v", result)
	}

	// No-op for missing value.
	result = removeOrderedValue(vals, "z")
	if len(result) != 3 {
		t.Errorf("expected unchanged slice, got %v", result)
	}
}

func TestRunProjection_ActivityKey(t *testing.T) {
	// With a non-empty target, the key includes the target name.
	key := activityKey("host-a", " connecting ")
	if key != "host-a\x00connecting" {
		t.Errorf("expected host-a\\x00connecting, got %q", key)
	}

	// With an empty target, fallback to "localhost".
	key = activityKey("", "connecting")
	if key != "localhost\x00connecting" {
		t.Errorf("expected localhost\\x00connecting, got %q", key)
	}
}

func TestRunProjection_ActionPathTakenFromActiveTask(t *testing.T) {
	// When ActionPath is not set in the completion event but was set in
	// TaskStartedEvent, the projection should propagate it from the active task.
	p := NewRunProjection()
	p.Apply(TaskStartedEvent{Target: "host-a", TaskID: "t1", TaskName: "do-thing", ActionPath: "my-action"})

	// Use TaskOKEvent without ActionPath (as it doesn't have one).
	descs := p.Apply(TaskOKEvent{Target: "host-a", TaskID: "t1", TaskName: "do-thing", ElapsedMs: 150})
	desc := descs[0].(TaskFinishedDescriptor)
	if desc.ActionPath != "my-action" {
		t.Errorf("expected ActionPath inherited from active task, got %q", desc.ActionPath)
	}
}
