package module

import (
	"strings"
	"testing"
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
