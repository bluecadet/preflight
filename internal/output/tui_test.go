package output

import (
	"bytes"
	"testing"
	"time"
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

func TestTUIRenderer_PlayStartTaskResultPlayEnd(t *testing.T) {
	var buf bytes.Buffer
	r := NewTUIRenderer(&buf)

	r.Emit(Event{Type: EventPlayStart, PlayName: "test-play", Target: "test-host", TaskTotal: 1})
	r.Emit(Event{Type: EventTaskStart, TaskName: "Configure firewall", Module: "shell", Target: "test-host", TaskIndex: 1, TaskTotal: 1})
	r.Emit(Event{
		Type:      EventTaskResult,
		TaskName:  "Configure firewall",
		Target:    "test-host",
		Status:    "ok",
		TaskIndex: 1,
		TaskTotal: 1,
	})
	r.Emit(Event{
		Type:         EventPlayEnd,
		Target:       "test-host",
		TaskTotal:    1,
		OKCount:      1,
		ChangedCount: 0,
		FailedCount:  0,
		SkippedCount: 0,
	})

	done := make(chan struct{})
	go func() {
		r.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("TUIRenderer.Close() timed out after play_start+task_result+play_end")
	}
}

func TestTUIRenderer_MultipleStatuses(t *testing.T) {
	var buf bytes.Buffer
	r := NewTUIRenderer(&buf)

	statuses := []string{"ok", "changed", "failed", "skipped"}
	for i, s := range statuses {
		r.Emit(Event{
			Type:      EventTaskStart,
			TaskName:  "task-" + s,
			Target:    "host",
			TaskIndex: i + 1,
			TaskTotal: len(statuses),
		})
		r.Emit(Event{
			Type:      EventTaskResult,
			TaskName:  "task-" + s,
			Target:    "host",
			Status:    s,
			TaskIndex: i + 1,
			TaskTotal: len(statuses),
		})
	}
	r.Emit(Event{
		Type:         EventPlayEnd,
		Target:       "host",
		TaskTotal:    len(statuses),
		OKCount:      1,
		ChangedCount: 1,
		FailedCount:  1,
		SkippedCount: 1,
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

func TestAutoDetect_NonTTY(t *testing.T) {
	var buf bytes.Buffer
	f := AutoDetect(&buf)
	if f != FormatText {
		t.Errorf("AutoDetect with bytes.Buffer: expected FormatText, got %q", f)
	}
}

func TestAutoDetect_AnotherNonTTY(t *testing.T) {
	// Any non-os.Stdout writer that is not a TTY should return FormatText.
	w := &bytes.Buffer{}
	got := AutoDetect(w)
	if got != FormatText {
		t.Errorf("expected FormatText for non-TTY writer, got %q", got)
	}
}

func TestTUIModel_ApplyEvent_PlayStart(t *testing.T) {
	events := make(chan Event, 1)
	var buf bytes.Buffer
	m := newTUIModel(events, &buf)
	m.applyEvent(Event{Type: EventPlayStart, PlayName: "my-play", Target: "host-a", TaskTotal: 3})
	host := m.hosts["host-a"]
	if host.playName != "my-play" {
		t.Errorf("expected playName=my-play, got %q", host.playName)
	}
	if host.totalTasks != 3 {
		t.Errorf("expected totalTasks=3, got %d", host.totalTasks)
	}
}

func TestTUIModel_ApplyEvent_TaskLifecycle(t *testing.T) {
	events := make(chan Event, 1)
	var buf bytes.Buffer
	m := newTUIModel(events, &buf)

	m.applyEvent(Event{
		Type:      EventTaskStart,
		TaskName:  "do-thing",
		Module:    "shell",
		Target:    "host-a",
		TaskIndex: 1,
		TaskTotal: 2,
	})
	host := m.hosts["host-a"]
	if !host.running {
		t.Fatal("expected host to be running after task_start")
	}
	if host.activeTask != "do-thing" {
		t.Errorf("expected active task do-thing, got %q", host.activeTask)
	}

	m.applyEvent(Event{
		Type:      EventTaskResult,
		TaskName:  "do-thing",
		Target:    "host-a",
		Status:    "ok",
		TaskIndex: 1,
		TaskTotal: 2,
	})
	host = m.hosts["host-a"]
	if host.running {
		t.Fatal("expected host to stop running after task_result")
	}
	if host.recap.ok != 1 {
		t.Errorf("expected recap.ok=1, got %d", host.recap.ok)
	}
	if host.completed != 1 {
		t.Errorf("expected completed=1, got %d", host.completed)
	}
}

func TestTUIModel_ApplyEvent_PlayEnd(t *testing.T) {
	events := make(chan Event, 1)
	var buf bytes.Buffer
	m := newTUIModel(events, &buf)

	m.applyEvent(Event{
		Type:         EventPlayEnd,
		Target:       "host-a",
		TaskTotal:    6,
		OKCount:      3,
		ChangedCount: 2,
		FailedCount:  1,
		SkippedCount: 0,
	})

	host := m.hosts["host-a"]
	if host.recap.ok != 3 {
		t.Errorf("expected recap.ok=3, got %d", host.recap.ok)
	}
	if host.recap.changed != 2 {
		t.Errorf("expected recap.changed=2, got %d", host.recap.changed)
	}
	if host.recap.failed != 1 {
		t.Errorf("expected recap.failed=1, got %d", host.recap.failed)
	}
	if !host.done {
		t.Fatal("expected host to be marked done")
	}
}
