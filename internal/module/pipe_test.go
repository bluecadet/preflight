package module

import (
	"strings"
	"testing"
	"time"
)

func TestNewOutputPipe_AllowsLongSingleLine(t *testing.T) {
	pw, done := NewOutputPipe(nil)
	longLine := strings.Repeat("x", 2<<20)
	if _, err := pw.Write([]byte(longLine + "\n")); err != nil {
		t.Fatalf("write output pipe: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("close output pipe: %v", err)
	}

	result := <-done
	if result.ScanErr != nil {
		t.Fatalf("unexpected scan error: %v", result.ScanErr)
	}
	if len(result.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(result.Lines))
	}
	if result.Lines[0] != longLine {
		t.Fatalf("long output line was not preserved: got %d bytes, want %d", len(result.Lines[0]), len(longLine))
	}
}

// TestNewOutputPipe_RecoversOnOutputPanic verifies that a panic inside the
// caller-supplied onOutput callback is converted into a ScanErr on the done
// channel instead of crashing the process. A misbehaving callback (e.g. from
// output rendering several layers up) must fail only the command whose
// output it was formatting.
func TestNewOutputPipe_RecoversOnOutputPanic(t *testing.T) {
	pw, done := NewOutputPipe(func(line string) {
		panic("boom: simulated onOutput panic")
	})
	if _, err := pw.Write([]byte("first line\n")); err != nil {
		t.Fatalf("write output pipe: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("close output pipe: %v", err)
	}

	select {
	case result := <-done:
		if result.ScanErr == nil {
			t.Fatal("expected ScanErr to report the recovered panic, got nil")
		}
		if !strings.Contains(result.ScanErr.Error(), "boom: simulated onOutput panic") {
			t.Fatalf("ScanErr = %q, want it to describe the recovered panic", result.ScanErr.Error())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("done channel never received a result; recovery goroutine appears to have hung")
	}
}
