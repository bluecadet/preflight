package target

import (
	"context"
	"testing"
	"time"
)

// TestWinRMIntegration_Streaming exercises the output streaming path over a
// live WinRM connection. It runs a multi-line PowerShell command through the
// powershell module and asserts that output arrives incrementally through the
// onOutput callback rather than being batched until the command completes.
//
// The test uses a goroutine + buffered channel to observe interleaving: the
// script sleeps 100ms between lines, so the first line should reach the
// channel ~100ms into a ~500ms execution. A select with a 200ms timeout on
// channel read (before the Execute goroutine finishes) proves streaming. If
// batch fallback occurs, all output arrives after ~500ms and the 200ms
// timeout fires.
//
// Gated by PREFLIGHT_TEST_WINRM and the sacrificial sentinel.
func TestWinRMIntegration_Streaming(t *testing.T) {
	cfg, ok := getWinRMConfigFromEnv()
	if !ok {
		t.Skip("PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS are not set; skipping Windows WinRM integration test")
	}

	tgt := NewWinRMTarget(*cfg)

	// ---- Sacrificial-target guard ----
	assertSacrificialSentinel(t, tgt)

	const script = `$lines = @('chunk-one','chunk-two','chunk-three','chunk-four','chunk-five')
foreach ($l in $lines) { Write-Output $l; Start-Sleep -Milliseconds 100 }
Write-Output 'done'`

	// Run Execute in a goroutine so we can observe whether onOutput fires
	// during execution (streaming) or only after it finishes (batch).
	ctx := context.Background()
	ch := make(chan string, 6)
	done := make(chan struct{})
	var result Result
	var execErr error

	go func() {
		result, execErr = tgt.Execute(ctx, "streaming-test", "powershell", map[string]any{
			"check_script": "return $true",
			"script":       script,
		}, ExecutionOptions{}, false, func(line string) {
			ch <- line
		})
		close(done)
	}()

	// Assert the first line arrives well before the script finishes (~500ms).
	// The script sleeps 100ms before each Write-Output, so the first line
	// hits the channel at ~100ms. If streaming fell back to batch, no data
	// arrives until done fires at ~500ms.
	select {
	case first := <-ch:
		if first != "chunk-one" {
			t.Fatalf("first line via onOutput: got %q, want %q", first, "chunk-one")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no output received within 200ms — output may be batched, not streamed")
	}

	// Collect remaining lines.
	gotLines := []string{"chunk-one"}
	for i := range 5 {
		select {
		case line := <-ch:
			gotLines = append(gotLines, line)
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for line %d of output", i+2)
		}
	}

	<-done // wait for Execute to complete

	if execErr != nil {
		t.Fatalf("Execute returned error: %v", execErr)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	// Both onOutput and result.Output should carry the full script output.
	want := []string{"chunk-one", "chunk-two", "chunk-three", "chunk-four", "chunk-five", "done"}
	for i := range want {
		if gotLines[i] != want[i] {
			t.Fatalf("onOutput line %d: got %q, want %q", i, gotLines[i], want[i])
		}
	}

	if len(result.Output) < len(want) {
		t.Fatalf("result.Output has %d entries, want at least %d: %v", len(result.Output), len(want), result.Output)
	}
	for i := range want {
		if result.Output[i] != want[i] {
			t.Fatalf("result.Output[%d]: got %q, want %q", i, result.Output[i], want[i])
		}
	}
}
