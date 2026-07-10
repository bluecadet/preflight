package output

import (
	"bytes"
	"strings"
	"testing"
)

// finishedTaskLine finds the rendered line for a task named taskName in out.
func finishedTaskLine(t *testing.T, out, taskName string) string {
	t.Helper()
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if strings.Contains(line, taskName) {
			return line
		}
	}
	t.Fatalf("no rendered line contains %q in:\n%s", taskName, out)
	return ""
}

// TestTextRenderer_RightAlignsElapsedToWidth verifies that finished-task
// elapsed times are right-aligned to the renderer's target width, not a
// fixed constant. PadLine produces left + spaces + right whose total byte
// length equals the width when the content fits, so asserting the line
// length equals the width is a direct, deterministic check of alignment.
func TestTextRenderer_RightAlignsElapsedToWidth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		width int
	}{
		{"wide", 100},
		{"narrow", 60},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			r := NewTextRendererWithOptions(&buf, Options{
				Mode:  "apply",
				Width: tc.width,
			})
			r.Emit(RunStartEvent{
				Mode:         "apply",
				PlaybookPath: "kiosk-provision.yml",
				PlaybookName: "kiosk-provision",
				Targets:      []string{"kiosk-01"},
			})
			r.Emit(TargetStartEvent{Target: "kiosk-01", Transport: "local"})
			r.Emit(TaskStartedEvent{
				Target:   "kiosk-01",
				TaskID:   "install-drivers",
				TaskName: "install display drivers",
			})
			r.Emit(TaskOKEvent{
				Target:    "kiosk-01",
				TaskID:    "install-drivers",
				TaskName:  "install display drivers",
				ElapsedMs: 500,
			})

			line := finishedTaskLine(t, buf.String(), "install display drivers")
			if len(line) != tc.width {
				t.Fatalf("expected finished-task line right-aligned to width %d, got length %d: %q", tc.width, len(line), line)
			}
		})
	}
}

// TestDetectWidth_NonTTYFallback verifies that when output is piped (no
// TTY), width detection falls back to the fixed default so output stays
// greppable and deterministic.
func TestDetectWidth_NonTTYFallback(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if w := detectWidth(&buf); w != lineWidth {
		t.Fatalf("detectWidth(non-TTY buffer) = %d, want %d", w, lineWidth)
	}
	if w := detectWidth(bytes.NewBuffer(nil)); w != lineWidth {
		t.Fatalf("detectWidth(non-TTY reader) = %d, want %d", w, lineWidth)
	}
}

// TestTextRenderer_WideWidth_Snapshot golden-locks the new behavior: at a
// non-default width, finished-task and run-header elapsed times right-align
// to that width rather than the fixed 80. It reuses the shared ok-task
// fixture rendered through Options.Width so the alignment is exercised end
// to end rather than via the time-based TTY detection path.
func TestTextRenderer_WideWidth_Snapshot(t *testing.T) {
	t.Parallel()

	var events []Event
	var opts Options
	for _, c := range newEventSnapshotCases() {
		if c.name == "run-with-one-ok-task" {
			events, opts = c.events, c.opts
			break
		}
	}
	if events == nil {
		t.Fatal("fixture run-with-one-ok-task not found")
	}
	opts.Width = 120

	var buf bytes.Buffer
	r := NewTextRendererWithOptions(&buf, opts)
	for _, event := range events {
		r.Emit(event)
	}
	r.Close()

	assertSnapshot(t, snapshotPath("text", "run-with-one-ok-task-wide"), normalizeSnapshot(buf.String()))
}
