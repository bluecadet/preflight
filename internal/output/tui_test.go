package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestNewTUIRenderer_NoPanel(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTUIRenderer(&buf)
	if renderer == nil {
		t.Fatal("NewTUIRenderer returned nil")
	}
	if renderer.program == nil {
		t.Error("expected program to be non-nil")
	}
	if renderer.events == nil {
		t.Error("expected events channel to be non-nil")
	}
	if renderer.done == nil {
		t.Error("expected done channel to be non-nil")
	}

	done := make(chan struct{})
	go func() {
		renderer.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("TUIRenderer.Close() timed out")
	}
}

func TestTUIRenderer_PlayStartTaskLifecycle(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTUIRenderer(&buf)

	renderer.Emit(Event{Type: EventPlayStart, PlayName: "test-play", Target: "host-a"})
	renderer.Emit(Event{Type: EventPhaseStart, Target: "host-a", Phase: "plan"})
	renderer.Emit(Event{Type: EventPhaseEnd, Target: "host-a", Phase: "plan", Status: "ok", TaskTotal: 1})
	renderer.Emit(Event{Type: EventTaskStart, Target: "host-a", TaskID: "task-1", TaskName: "Configure firewall", Module: "shell"})
	renderer.Emit(Event{Type: EventTaskLog, Target: "host-a", TaskID: "task-1", Stream: "stdout", Line: "hello"})
	renderer.Emit(Event{Type: EventTaskResult, Target: "host-a", TaskID: "task-1", TaskName: "Configure firewall", Status: "ok"})
	renderer.Emit(Event{Type: EventPlayEnd, Target: "host-a", OKCount: 1})

	done := make(chan struct{})
	go func() {
		renderer.Close()
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

func TestTUIModel_ApplyEventLifecycle(t *testing.T) {
	events := make(chan Event, 1)
	model := newTUIModel(events, Options{})

	model = model.applyEvent(Event{Type: EventPlayStart, PlayName: "my-play", Target: "host-a"})
	model = model.applyEvent(Event{Type: EventPhaseStart, Target: "host-a", Phase: "plan"})
	model = model.applyEvent(Event{Type: EventPhaseEnd, Target: "host-a", Phase: "plan", Status: "ok", TaskTotal: 2})
	model = model.applyEvent(Event{Type: EventTaskStart, Target: "host-a", TaskID: "task-1", TaskName: "do-thing", Module: "shell", TaskTotal: 2})
	model = model.applyEvent(Event{Type: EventTaskLog, Target: "host-a", TaskID: "task-1", Stream: "stdout", Line: "stream line"})
	model = model.applyEvent(Event{Type: EventTaskResult, Target: "host-a", TaskID: "task-1", TaskName: "do-thing", Module: "shell", Status: "failed", Message: "boom"})
	model = model.applyEvent(Event{Type: EventPlayEnd, Target: "host-a", FailedCount: 1})

	host := model.hosts["host-a"]
	if host == nil {
		t.Fatal("expected host state to be created")
	}
	if host.playName != "my-play" {
		t.Fatalf("expected playName my-play, got %q", host.playName)
	}
	if host.totalTasks != 2 {
		t.Fatalf("expected totalTasks=2, got %d", host.totalTasks)
	}
	task := host.tasks["task-1"]
	if task == nil {
		t.Fatal("expected task state to be created")
	}
	if task.status != "failed" {
		t.Fatalf("expected failed task status, got %q", task.status)
	}
	if !task.expanded {
		t.Fatal("expected failed task to auto-expand")
	}
	if len(task.logs) != 1 {
		t.Fatalf("expected one task log, got %d", len(task.logs))
	}
	if host.recap.failed != 1 {
		t.Fatalf("expected failed recap count 1, got %d", host.recap.failed)
	}
	if !host.done {
		t.Fatal("expected host to be marked done")
	}
}

func TestTUIModel_KeyNavigationAndFilters(t *testing.T) {
	events := make(chan Event, 1)
	model := newTUIModel(events, Options{})
	model = model.applyEvent(Event{Type: EventPlayStart, PlayName: "play", Target: "host-a"})
	model = model.applyEvent(Event{Type: EventPlayStart, PlayName: "play", Target: "host-b"})
	model = model.applyEvent(Event{Type: EventTaskStart, Target: "host-a", TaskID: "task-1", TaskName: "ok task"})
	model = model.applyEvent(Event{Type: EventTaskResult, Target: "host-a", TaskID: "task-1", TaskName: "ok task", Status: "ok"})
	model = model.applyEvent(Event{Type: EventTaskStart, Target: "host-a", TaskID: "task-2", TaskName: "bad task"})
	model = model.applyEvent(Event{Type: EventTaskResult, Target: "host-a", TaskID: "task-2", TaskName: "bad task", Status: "failed"})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	model = next.(tuiModel)
	if model.selectedHost != 1 {
		t.Fatalf("expected host selection to move to index 1, got %d", model.selectedHost)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = next.(tuiModel)
	if model.selectedHost != 0 {
		t.Fatalf("expected left key to return to host index 0, got %d", model.selectedHost)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	model = next.(tuiModel)
	visible := model.visibleTasks(model.currentHost())
	if len(visible) != 1 || visible[0] != "task-2" {
		t.Fatalf("expected failed-only filter to leave task-2, got %#v", visible)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(tuiModel)
	if host := model.currentHost(); host == nil || host.selectedTask != 0 {
		t.Fatal("expected failed-only mode to clamp the host selection to the single visible task")
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model = next.(tuiModel)
	if !model.collapseCompleted {
		t.Fatal("expected collapseCompleted toggle to be enabled")
	}
}

func TestTaskView_LogBufferIsBounded(t *testing.T) {
	task := &taskView{}
	for range maxTaskLogLines + 25 {
		task.appendLog("stdout", "line")
	}
	if len(task.logs) != maxTaskLogLines {
		t.Fatalf("expected log buffer capped at %d lines, got %d", maxTaskLogLines, len(task.logs))
	}
}

func TestRenderTaskCard_ExpandedDoesNotDuplicatePreviewLogs(t *testing.T) {
	model := newTUIModel(make(chan Event, 1), Options{})
	task := &taskView{
		name:     "stream logs",
		module:   "shell",
		status:   "failed",
		message:  "boom",
		expanded: true,
		logs: []taskLogLine{
			{stream: "stdout", line: "alpha"},
			{stream: "stderr", line: "boom"},
		},
	}

	rendered := model.renderTaskCard(task, false, 80)
	if strings.Count(rendered, "alpha") != 1 {
		t.Fatalf("expected expanded card to render stdout log once, got %q", rendered)
	}
	if strings.Count(rendered, "boom") != 1 {
		t.Fatalf("expected expanded card to avoid repeating the failure message, got %q", rendered)
	}
}

func TestTUIModel_ViewRespectsWindowHeight(t *testing.T) {
	model := newTUIModel(make(chan Event, 1), Options{})
	model.width = 60
	model.height = 12
	model = model.applyEvent(Event{Type: EventPlayStart, PlayName: "play", Target: "host-a"})
	model = model.applyEvent(Event{Type: EventPhaseStart, Target: "host-a", Phase: "plan"})
	model = model.applyEvent(Event{Type: EventPhaseEnd, Target: "host-a", Phase: "plan", Status: "ok", TaskTotal: 8})
	for i := range 8 {
		taskID := "task-" + string(rune('a'+i))
		model = model.applyEvent(Event{Type: EventTaskStart, Target: "host-a", TaskID: taskID, TaskName: "task", Module: "shell"})
		model = model.applyEvent(Event{Type: EventTaskResult, Target: "host-a", TaskID: taskID, TaskName: "task", Status: "ok"})
	}

	rendered := model.View()
	if lipgloss.Height(rendered) > model.height {
		t.Fatalf("expected rendered view height <= %d, got %d\n%s", model.height, lipgloss.Height(rendered), rendered)
	}
}

func TestStaticScreenModel_ViewRespectsWindowHeight(t *testing.T) {
	model := newStaticScreenModel(Screen{
		Command: "plan",
		Subject: "play: demo",
		Status:  "ready",
		Content: ScreenContent{
			Kind: ScreenKindList,
			Items: []ScreenItem{
				{Title: "one", Status: "ok"},
				{Title: "two", Status: "ok"},
				{Title: "three", Status: "ok"},
				{Title: "four", Status: "ok"},
			},
		},
	})
	model.width = 60
	model.height = 10
	model.initialized = true

	rendered := model.View()
	if lipgloss.Height(rendered) > model.height {
		t.Fatalf("expected static rendered view height <= %d, got %d\n%s", model.height, lipgloss.Height(rendered), rendered)
	}
}
