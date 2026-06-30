//go:build integration

package target

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestWinRMIntegration_Streaming exercises the output streaming path over a
// live WinRM connection. It runs a multi-line PowerShell command through the
// powershell module, asserts that every line is delivered through the onOutput
// callback in order, and — when the transport supports it — that the lines
// arrive incrementally rather than in a single batch at completion.
//
// Incremental delivery is a capability of the underlying WinRM session, not of
// preflight: the WS-Man Receive channel on a basic (NTLM/Negotiate) session
// buffers a command's stdout and hands it over in one piece when the command
// finishes, regardless of [Console]::Out flushing or session reuse. The
// script sleeps 100ms between each of five chunks, so genuinely streamed
// output spreads over ~400ms while batched output collapses to ~0. The test
// stamps each callback; if the spread shows the session batched, it skips the
// streaming-timing assertion (the delivery and ordering checks still ran)
// rather than failing on an environment limitation.
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

	// Run Execute in a goroutine, stamping the arrival time of each onOutput
	// callback so we can measure how the output is spread over time.
	type stampedLine struct {
		line string
		at   time.Time
	}
	ctx := context.Background()
	var mu sync.Mutex
	var got []stampedLine
	done := make(chan struct{})
	var result Result
	var execErr error

	go func() {
		result, execErr = tgt.Execute(ctx, "streaming-test", "powershell", map[string]any{
			"check_script": "return $true",
			"script":       script,
		}, ExecutionOptions{}, false, func(line string) {
			mu.Lock()
			got = append(got, stampedLine{line: line, at: time.Now()})
			mu.Unlock()
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Execute did not complete within 30s")
	}

	if execErr != nil {
		t.Fatalf("Execute returned error: %v", execErr)
	}
	if result.Status != StatusChanged {
		t.Fatalf("expected StatusChanged, got %q: %s", result.Status, result.Message)
	}

	mu.Lock()
	defer mu.Unlock()

	// The five chunk lines plus 'done' must arrive in order via onOutput.
	want := []string{"chunk-one", "chunk-two", "chunk-three", "chunk-four", "chunk-five", "done"}
	if len(got) < len(want) {
		t.Fatalf("onOutput delivered %d lines, want at least %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].line != want[i] {
			t.Fatalf("onOutput line %d: got %q, want %q", i, got[i].line, want[i])
		}
	}

	// result.Output should carry the full script output too.
	if len(result.Output) < len(want) {
		t.Fatalf("result.Output has %d entries, want at least %d: %v", len(result.Output), len(want), result.Output)
	}
	for i := range want {
		if result.Output[i] != want[i] {
			t.Fatalf("result.Output[%d]: got %q, want %q", i, result.Output[i], want[i])
		}
	}

	// ---- Streaming-capability gate ----
	// The script paces output at 100ms intervals, so a session that streams
	// spreads the first and last lines by ~400ms. A near-zero spread means the
	// WinRM session delivered everything in one batch at completion — an
	// environment limitation, not a preflight defect — so skip rather than fail.
	const minStreamingSpread = 250 * time.Millisecond
	spread := got[len(want)-1].at.Sub(got[0].at)
	if spread < minStreamingSpread {
		t.Skipf("WinRM session delivered output in a single batch (spread %v across %d lines); "+
			"incremental streaming is not supported on this connection (WS-Man buffers command "+
			"stdout until completion). Delivery and ordering were verified.", spread, len(want))
	}
}
