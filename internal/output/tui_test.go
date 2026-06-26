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
	if m.playName != "my-play" {
		t.Errorf("expected playName=my-play, got %q", m.playName)
	}
}

func TestTUIModel_ApplyEvent_TaskOK(t *testing.T) {
	events := make(chan Event, 1)
	m := newTUIModel(events)

	m, _ = m.applyEvent(TaskOKEvent{
		TaskName: "do-thing",
		Target:   "host-a",
		TaskID:   "task-1",
		ElapsedMs: 100,
	})

	if m.okCount != 1 {
		t.Errorf("expected okCount=1, got %d", m.okCount)
	}
	if m.changedCount != 0 {
		t.Errorf("expected changedCount=0, got %d", m.changedCount)
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

	at := m.hosts["host-a"]["task-1"]
	if at == nil {
		t.Fatal("expected active task to exist")
	}
	if len(at.recentLines) != maxTaskPreviewLines {
		t.Fatalf("expected %d preview lines, got %d", maxTaskPreviewLines, len(at.recentLines))
	}
	want := []string{"line2", "line3", "line4"}
	for i, line := range want {
		if at.recentLines[i] != line {
			t.Fatalf("recentLines[%d] = %q, want %q", i, at.recentLines[i], line)
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