package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestNewTUIRenderer_NoPanel(t *testing.T) {
	var buf bytes.Buffer
	r := NewTUIRenderer(&buf)
	if r == nil {
		t.Fatal("NewTUIRenderer returned nil")
	}
	if r.program == nil {
		t.Error("expected program to be non-nil")
	}
	if r.events == nil {
		t.Error("expected events channel to be non-nil")
	}
	if r.done == nil {
		t.Error("expected done channel to be non-nil")
	}
	// Close cleanly without sending any events.
	done := make(chan struct{})
	go func() {
		r.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("TUIRenderer.Close() timed out")
	}
}

func TestAutoDetect_NonTTY(t *testing.T) {
	var buf bytes.Buffer
	f := AutoDetect(&buf)
	if f != FormatText {
		t.Errorf("AutoDetect with bytes.Buffer: expected FormatText, got %q", f)
	}
}

func TestAutoDetect_AnotherNonTTY(t *testing.T) {
	w := &bytes.Buffer{}
	got := AutoDetect(w)
	if got != FormatText {
		t.Errorf("expected FormatText for non-TTY writer, got %q", got)
	}
}

func TestTUIModel_ApplyEvent_RunStart(t *testing.T) {
	events := make(chan Event, 1)
	m := newTUIModel(events)
	m, _ = m.applyEvent(RunStartEvent{
		PlaybookName: "my-play",
		Targets:      []string{"host-a"},
	})
	if m.projection.PlayName != "my-play" {
		t.Errorf("expected PlayName=my-play, got %q", m.projection.PlayName)
	}
}

func TestTUIModel_ApplyEvent_TaskOK(t *testing.T) {
	events := make(chan Event, 1)
	m := newTUIModel(events)

	m, _ = m.applyEvent(TaskOKEvent{
		TaskName:  "do-thing",
		Target:    "host-a",
		TaskID:    "task-1",
		ElapsedMs: 100,
	})

	if m.projection.OkCount != 1 {
		t.Errorf("expected OkCount=1, got %d", m.projection.OkCount)
	}
	if m.projection.ChangedCount != 0 {
		t.Errorf("expected ChangedCount=0, got %d", m.projection.ChangedCount)
	}
}

func TestTUIModel_TaskOutputKeepsLastThreeLines(t *testing.T) {
	events := make(chan Event, 1)
	m := newTUIModel(events)

	m, _ = m.applyEvent(TaskStartedEvent{
		Target:   "host-a",
		TaskID:   "task-1",
		TaskName: "stream logs",
	})
	m, _ = m.applyEvent(TaskOutputEvent{
		Target: "host-a",
		TaskID: "task-1",
		Lines:  []string{"line1", "line2", "line3", "line4"},
	})

	at := m.projection.OrderedRunningTasks()
	if len(at) != 1 {
		t.Fatalf("expected 1 active task, got %d", len(at))
	}
	if len(at[0].recentLines) != maxTaskPreviewLines {
		t.Fatalf("expected %d preview lines, got %d", maxTaskPreviewLines, len(at[0].recentLines))
	}
	want := []string{"line2", "line3", "line4"}
	for i, line := range want {
		if at[0].recentLines[i] != line {
			t.Fatalf("recentLines[%d] = %q, want %q", i, at[0].recentLines[i], line)
		}
	}
}

func TestTUIModel_WrapsLongCommittedFailureOutput(t *testing.T) {
	events := make(chan Event, 1)
	m := newTUIModel(events)
	m.width = 48
	m, _ = m.applyEvent(TaskStartedEvent{
		Target:   "host-a",
		TaskID:   "task-1",
		TaskName: "Run smoke test",
	})
	m, _ = m.applyEvent(TaskFailedEvent{
		Target:      "host-a",
		TaskID:      "task-1",
		TaskName:    "Run smoke test",
		FailMessage: strings.Repeat("failure-message ", 6),
		Output:      []string{strings.Repeat("verbose-output ", 6)},
	})
	m.done = true

	for line := range strings.SplitSeq(strings.TrimSpace(m.View()), "\n") {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("expected wrapped line <= %d cells, got %d: %q\n%s", m.width, lipgloss.Width(line), line, m.View())
		}
	}
}

func TestTUIModel_FactsPrintsImmediatelyForLocalTarget(t *testing.T) {
	events := make(chan Event, 1)
	m := newTUIModel(events)

	m, cmd := m.applyEvent(FactsEvent{
		Target: "localhost",
		Facts: map[string]any{
			"hostname": "LOCALHOST",
		},
	})

	blocks := collectPrintedBlocks(cmd)
	if len(blocks) != 1 {
		t.Fatalf("expected one printed facts block, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0], "◌ Facts") {
		t.Fatalf("expected printed facts card, got %q", blocks[0])
	}
	if view := m.View(); view != "" {
		t.Fatalf("expected facts output to be committed to scrollback, got live view %q", view)
	}
}

func TestTUIModel_FactsPrintsAfterActivityCompletes(t *testing.T) {
	events := make(chan Event, 1)
	m := newTUIModel(events)

	m, _ = m.applyEvent(ActivityStartEvent{Target: "remote-host", Message: "connecting"})
	m, _ = m.applyEvent(ActivityResultEvent{Target: "remote-host", Message: "connecting", Status: "ok"})
	_, cmd := m.applyEvent(FactsEvent{
		Target: "remote-host",
		Facts: map[string]any{
			"hostname": "REMOTE-HOST",
		},
	})

	blocks := collectPrintedBlocks(cmd)
	if len(blocks) != 1 {
		t.Fatalf("expected one printed facts block, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0], "remote-host") {
		t.Fatalf("expected printed facts for remote host, got %q", blocks[0])
	}
}

func TestTUIModel_RunStartThenTaskEndsCleanly(t *testing.T) {
	var buf bytes.Buffer
	r := NewTUIRenderer(&buf)

	r.Emit(RunStartEvent{
		Mode:         "apply",
		PlaybookPath: "test.yml",
		PlaybookName: "test-play",
		Targets:      []string{"test-host"},
	})
	r.Emit(TaskStartedEvent{
		Target:   "test-host",
		TaskID:   "t1",
		TaskName: "Configure firewall",
	})
	r.Emit(TaskOKEvent{
		Target:   "test-host",
		TaskID:   "t1",
		TaskName: "Configure firewall",
	})
	r.Emit(RunSummaryEvent{
		Status:        "success",
		OKCount:       1,
		ElapsedMs:     100,
		TargetTallies: TargetCounts{OK: 1},
	})

	done := make(chan struct{})
	go func() {
		r.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("TUIRenderer.Close() timed out after run_start+task_ok")
	}
}

func TestTUIModel_MultiTargetTaskFinished(t *testing.T) {
	// Source events from the shared fixture to exercise both
	// text and TUI surfaces from the same input.
	var fixture *snapshotCase
	for _, tc := range newEventSnapshotCases() {
		if tc.name == "two-targets-task-finished" {
			f := tc
			fixture = &f
			break
		}
	}
	if fixture == nil {
		t.Fatal("shared fixture 'two-targets-task-finished' not found")
	}

	savedS := S
	defer func() { S = savedS }()
	S = NewTUIStyles(DefaultPalette(), true)

	events := make(chan Event, len(fixture.events))
	m := newTUIModelWithOptions(events, Options{Color: ColorAlways})
	m.width = 80

	type namedBlock struct {
		name  string
		block string
	}
	var blocks []namedBlock

	eventNames := []string{
		"run-start",
		"target-start-a",
		"target-start-b",
		"task-started-a",
		"task-ok",
		"task-started-b",
		"task-skipped",
	}
	for i, evt := range fixture.events {
		_, cmd := m.applyEvent(evt)
		if cmd != nil {
			for _, b := range collectPrintedBlocks(cmd) {
				name := eventNames[i]
				if i < len(eventNames) {
					name = eventNames[i]
				}
				blocks = append(blocks, namedBlock{name: name, block: b})
			}
		}
	}

	var sb strings.Builder
	for _, b := range blocks {
		sb.WriteString("[" + b.name + "]\n")
		sb.WriteString(b.block)
		sb.WriteByte('\n')
	}
	got := normalizeSnapshot(sb.String())
	assertSnapshot(t, snapshotPath("tui", "two-targets-task-finished"), got)
}

func TestTUIModel_CardSnapshots(t *testing.T) {
	savedS := S
	defer func() { S = savedS }()
	S = NewTUIStyles(DefaultPalette(), true)

	tests := []struct {
		name string
		evt  Event
	}{
		{
			name: "facts-card",
			evt: FactsEvent{
				Target: "kiosk-01",
				Facts: map[string]any{
					"hostname": "kiosk-01-prod",
					"os":       "Windows Server 2022",
					"kernel":   "10.0.20348",
					"cpu":      "Intel(R) Xeon(R) Platinum 8375C",
					"memory":   "8GB",
				},
			},
		},
		{
			name: "plan-card",
			evt: PlanEvent{
				Target:       "kiosk-01",
				PlaybookName: "kiosk-provision",
				Tasks: []PlanTaskEntry{
					{Number: 1, Module: "command", Name: "install display drivers", Tags: []string{"drivers"}},
					{Number: 2, Module: "registry", Name: "configure autologin", When: "os == 'windows'", Tags: []string{"login"}},
					{Number: 3, Module: "file", Name: "copy wallpaper", Tags: []string{}},
				},
			},
		},
		{
			name: "state-card",
			evt: StateEvent{
				Target:       "kiosk-01",
				PlaybookName: "kiosk-provision",
				StatePath:    "/var/lib/preflight/kiosk-provision.json",
				LastApplied:  "2026-07-01T12:00:00Z",
				Comparisons: []StateComparison{
					{Status: "UNCHANGED", TaskName: "install display drivers", Module: "command", RecordedStatus: "ok"},
					{Status: "CHANGED", TaskName: "configure autologin", Module: "registry", RecordedStatus: "changed"},
					{Status: "NEW", TaskName: "copy wallpaper", Module: "file", RecordedStatus: "ok"},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			events := make(chan Event, 1)
			m := newTUIModelWithOptions(events, Options{Color: ColorAlways})
			m.width = 80
			_, cmd := m.applyEvent(tc.evt)
			blocks := collectPrintedBlocks(cmd)
			if len(blocks) == 0 {
				t.Fatalf("no printed blocks for %s", tc.name)
			}
			got := normalizeSnapshot(blocks[0])
			assertSnapshot(t, snapshotPath("tui-card", tc.name), got)
		})
	}
}

func TestTUIModel_MultipleTaskStatuses(t *testing.T) {
	var buf bytes.Buffer
	r := NewTUIRenderer(&buf)

	r.Emit(RunStartEvent{
		Mode:         "apply",
		PlaybookPath: "test.yml",
		PlaybookName: "test-play",
		Targets:      []string{"host"},
	})

	statuses := []struct{ id, name, status string }{
		{"t1", "task-ok", "ok"},
		{"t2", "task-changed", "changed"},
		{"t3", "task-failed", "failed"},
		{"t4", "task-skipped", "skipped"},
	}
	for _, s := range statuses {
		r.Emit(TaskStartedEvent{
			Target:   "host",
			TaskID:   s.id,
			TaskName: s.name,
		})
		switch s.status {
		case "ok":
			r.Emit(TaskOKEvent{Target: "host", TaskID: s.id, TaskName: s.name})
		case "changed":
			r.Emit(TaskChangedEvent{Target: "host", TaskID: s.id, TaskName: s.name})
		case "failed":
			r.Emit(TaskFailedEvent{Target: "host", TaskID: s.id, TaskName: s.name, FailMessage: "error"})
		case "skipped":
			r.Emit(TaskSkippedEvent{Target: "host", TaskID: s.id, TaskName: s.name, Reason: "filtered"})
		}
	}

	r.Emit(RunSummaryEvent{
		Status:        "failed",
		OKCount:       1,
		ChangedCount:  1,
		FailedCount:   1,
		SkippedCount:  1,
		ElapsedMs:     500,
		TargetTallies: TargetCounts{Failed: 1},
	})

	done := make(chan struct{})
	go func() {
		r.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("TUIRenderer.Close() timed out")
	}
}

// TestTUIModel_Snapshots runs every shared event fixture through the TUI
// model and snapshots the scroll-region output. Each fixture produces a
// tui-{name}.golden file alongside the text-{name}.golden from
// TestTextRendererNewEventTypes_Snapshots.
//
// The "two-targets-task-finished" fixture is excluded because it has a
// dedicated labeled-block test (TestTUIModel_MultiTargetTaskFinished)
// that shares its golden path.
func TestTUIModel_Snapshots(t *testing.T) {
	for _, tc := range newEventSnapshotCases() {
		if tc.name == "two-targets-task-finished" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			got := tuiRenderSnapshot(tc.events)
			assertSnapshot(t, snapshotPath("tui", tc.name), got)
		})
	}
}
