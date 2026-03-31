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

	r.Emit(Event{Type: EventPlayStart, PlayName: "test-play"})
	r.Emit(Event{
		Type:     EventTaskResult,
		TaskName: "Configure firewall",
		Target:   "test-host",
		Status:   "ok",
	})
	r.Emit(Event{
		Type:         EventPlayEnd,
		Target:       "test-host",
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
			Type:     EventTaskResult,
			TaskName: "task-" + s,
			Target:   "host",
			Status:   s,
		})
		_ = i
	}
	r.Emit(Event{
		Type:         EventPlayEnd,
		Target:       "host",
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
	m := newTUIModel(events)
	m = m.applyEvent(Event{Type: EventPlayStart, PlayName: "my-play"})
	if m.playName != "my-play" {
		t.Errorf("expected playName=my-play, got %q", m.playName)
	}
}

func TestTUIModel_ApplyEvent_TaskResult(t *testing.T) {
	events := make(chan Event, 1)
	m := newTUIModel(events)

	m = m.applyEvent(Event{
		Type:     EventTaskResult,
		TaskName: "do-thing",
		Status:   "ok",
	})

	if len(m.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(m.tasks))
	}
	if m.tasks[0].name != "do-thing" {
		t.Errorf("expected task name do-thing, got %q", m.tasks[0].name)
	}
	if m.tasks[0].status != "ok" {
		t.Errorf("expected status ok, got %q", m.tasks[0].status)
	}
	if m.recap.ok != 1 {
		t.Errorf("expected recap.ok=1, got %d", m.recap.ok)
	}
}

func TestTUIModel_ApplyEvent_PlayEnd(t *testing.T) {
	events := make(chan Event, 1)
	m := newTUIModel(events)

	m = m.applyEvent(Event{
		Type:         EventPlayEnd,
		OKCount:      3,
		ChangedCount: 2,
		FailedCount:  1,
		SkippedCount: 0,
	})

	if m.recap.ok != 3 {
		t.Errorf("expected recap.ok=3, got %d", m.recap.ok)
	}
	if m.recap.changed != 2 {
		t.Errorf("expected recap.changed=2, got %d", m.recap.changed)
	}
	if m.recap.failed != 1 {
		t.Errorf("expected recap.failed=1, got %d", m.recap.failed)
	}
}
