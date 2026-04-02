package main

import (
	"math/rand"
	"sync"
	"time"

	"github.com/bluecadet/preflight/internal/output"
)

// emit helpers

func playStart(r output.Renderer, name string) {
	r.Emit(output.Event{Type: output.EventPlayStart, PlayName: name})
}

func taskStart(r output.Renderer, host, id, name string) {
	r.Emit(output.Event{Type: output.EventTaskStart, Target: host, TaskID: id, TaskName: name})
}

func taskDone(r output.Renderer, host, id, name, status, msg string) {
	taskDoneWithOutput(r, host, id, name, status, msg, nil)
}

func taskDoneWithOutput(r output.Renderer, host, id, name, status, msg string, outputLines []string) {
	r.Emit(output.Event{
		Type:     output.EventTaskResult,
		Target:   host,
		TaskID:   id,
		TaskName: name,
		Status:   status,
		Message:  msg,
		Output:   outputLines,
	})
}

func taskOutput(r output.Renderer, host, id, name string, lines ...string) {
	r.Emit(output.Event{
		Type:     output.EventTaskOutput,
		Target:   host,
		TaskID:   id,
		TaskName: name,
		Lines:    lines,
	})
}

func playEnd(r output.Renderer, host string, ok, changed, failed, skipped int) {
	r.Emit(output.Event{
		Type:         output.EventPlayEnd,
		Target:       host,
		OKCount:      ok,
		ChangedCount: changed,
		FailedCount:  failed,
		SkippedCount: skipped,
	})
}

func runTask(r output.Renderer, host, id, name, status, msg string, delay time.Duration) {
	taskStart(r, host, id, name)
	time.Sleep(delay)
	taskDone(r, host, id, name, status, msg)
}

// jitter returns delay scaled by a random factor in [low, high].
func jitter(delay time.Duration, low, high float64) time.Duration {
	f := low + rand.Float64()*(high-low)
	return time.Duration(float64(delay) * f)
}

// ---- scenarios ----

func runBasic(r output.Renderer, delay time.Duration) {
	playStart(r, "setup exhibit-pc-01")

	tasks := []struct{ id, name string }{
		{"ensure-wifi", "Ensure WiFi disabled"},
		{"set-hostname", "Set hostname"},
		{"install-runtime", "Install runtime"},
		{"copy-assets", "Copy assets"},
		{"configure-autostart", "Configure autostart"},
	}

	for _, t := range tasks {
		runTask(r, "exhibit-pc-01", t.id, t.name, "ok", "", delay)
	}

	playEnd(r, "exhibit-pc-01", len(tasks), 0, 0, 0)
}

func runMultiHost(r output.Renderer, delay time.Duration) {
	playStart(r, "gallery rollout")

	sr := output.Synchronized(r)

	hosts := []string{"gallery-01", "gallery-02", "gallery-03"}

	steps := []struct {
		id, name, status string
	}{
		{"preflight-check", "Preflight check", "ok"},
		{"install-deps", "Install dependencies", "changed"},
		{"deploy-app", "Deploy application", "ok"},
		{"reload-service", "Reload service", "changed"},
		{"verify-health", "Verify health check", "ok"},
	}

	var wg sync.WaitGroup
	for _, h := range hosts {
		wg.Go(func() {
			// stagger host start so they don't all begin simultaneously
			time.Sleep(jitter(delay, 0, 0.4))
			ok, changed := 0, 0
			for _, s := range steps {
				taskStart(sr, h, s.id, s.name)
				time.Sleep(jitter(delay, 0.4, 1.8))
				taskDone(sr, h, s.id, s.name, s.status, "")
				if s.status == "ok" {
					ok++
				} else {
					changed++
				}
			}
			playEnd(sr, h, ok, changed, 0, 0)
		})
	}
	wg.Wait()
}

func runFailures(r output.Renderer, delay time.Duration) {
	playStart(r, "deploy with failures")

	host := "exhibit-pc-02"

	runTask(r, host, "check-disk", "Check disk space", "ok", "", delay)
	runTask(r, host, "pull-image", "Pull container image", "changed", "Pulled 3 layers", delay)

	taskStart(r, host, "run-migrations", "Run database migrations")
	time.Sleep(delay * 2)
	taskDone(r, host, "run-migrations", "Run database migrations", "failed", "connection refused: postgres:5432")

	// dependent tasks get skipped
	r.Emit(output.Event{
		Type:     output.EventTaskResult,
		Target:   host,
		TaskID:   "start-service",
		TaskName: "Start service",
		Status:   "skipped",
		Message:  "dependency-failed",
	})
	r.Emit(output.Event{
		Type:     output.EventTaskResult,
		Target:   host,
		TaskID:   "smoke-test",
		TaskName: "Smoke test",
		Status:   "skipped",
		Message:  "dependency-failed",
	})

	r.Emit(output.Event{
		Type:    output.EventError,
		Target:  host,
		Message: "play aborted: 1 task failed",
	})

	playEnd(r, host, 1, 1, 1, 2)
}

func runNested(r output.Renderer, delay time.Duration) {
	playStart(r, "nested action demo")

	host := "kiosk-01"

	// top-level task
	runTask(r, host, "preflight-check", "Preflight check", "ok", "", delay/2)

	// action with nested sub-tasks
	taskStart(r, host, "install-chrome", "Install Chrome")
	time.Sleep(delay / 4)

	nested := []struct{ id, name, status string }{
		{"install-chrome/download", "Download installer", "changed"},
		{"install-chrome/verify-checksum", "Verify checksum", "ok"},
		{"install-chrome/run-installer", "Run installer", "changed"},
		{"install-chrome/cleanup", "Remove installer", "ok"},
	}

	for _, n := range nested {
		taskStart(r, host, n.id, n.name)
		time.Sleep(delay)
		taskDone(r, host, n.id, n.name, n.status, "")
	}

	taskDone(r, host, "install-chrome", "Install Chrome", "changed", "")

	// deeply nested
	taskStart(r, host, "configure-kiosk", "Configure kiosk mode")
	time.Sleep(delay / 4)

	deep := []struct{ id, name, status string }{
		{"configure-kiosk/registry/disable-taskbar", "Disable taskbar", "changed"},
		{"configure-kiosk/registry/disable-hotkeys", "Disable hotkeys", "changed"},
		{"configure-kiosk/registry/set-wallpaper", "Set wallpaper", "ok"},
		{"configure-kiosk/autostart/write-config", "Write autostart config", "changed"},
	}

	for _, d := range deep {
		taskStart(r, host, d.id, d.name)
		time.Sleep(delay)
		taskDone(r, host, d.id, d.name, d.status, "")
	}

	taskDone(r, host, "configure-kiosk", "Configure kiosk mode", "changed", "")

	runTask(r, host, "verify", "Verify configuration", "ok", "", delay/2)

	playEnd(r, host, 3, 6, 0, 0)
}

func runSkipped(r output.Renderer, delay time.Duration) {
	playStart(r, "conditional play")

	host := "media-server-01"

	runTask(r, host, "check-os", "Check OS version", "ok", "", delay/2)

	r.Emit(output.Event{
		Type:     output.EventTaskResult,
		Target:   host,
		TaskID:   "install-directx",
		TaskName: "Install DirectX",
		Status:   "skipped",
		Message:  "when-condition-false",
	})

	runTask(r, host, "install-codec", "Install codec pack", "changed", "", delay)

	r.Emit(output.Event{
		Type:     output.EventTaskResult,
		Target:   host,
		TaskID:   "enable-gpu-debug",
		TaskName: "Enable GPU debug layer",
		Status:   "skipped",
		Message:  "tag-filtered",
	})
	r.Emit(output.Event{
		Type:     output.EventTaskResult,
		Target:   host,
		TaskID:   "install-pix",
		TaskName: "Install PIX profiler",
		Status:   "skipped",
		Message:  "tag-filtered",
	})

	runTask(r, host, "configure-output", "Configure display output", "ok", "", delay)

	playEnd(r, host, 2, 1, 0, 3)
}

func runLarge(r output.Renderer, delay time.Duration) {
	playStart(r, "fleet rollout")

	sr := output.Synchronized(r)

	hosts := []string{
		"exhibit-01", "exhibit-02", "exhibit-03", "exhibit-04",
		"exhibit-05", "exhibit-06", "kiosk-01", "kiosk-02",
	}

	tasks := []struct {
		id, name, status string
	}{
		{"check-connectivity", "Check connectivity", "ok"},
		{"update-time", "Sync system time", "ok"},
		{"pull-configs", "Pull config bundle", "changed"},
		{"stop-service", "Stop exhibit service", "ok"},
		{"backup-state", "Backup state files", "ok"},
		{"install-update", "Install update package", "changed"},
		{"migrate-config", "Migrate configuration", "ok"},
		{"restore-state", "Restore state files", "ok"},
		{"start-service", "Start exhibit service", "changed"},
		{"verify-health", "Verify health endpoint", "ok"},
		{"clear-cache", "Clear asset cache", "ok"},
		{"finalize", "Finalize deployment", "ok"},
	}

	d := delay / 3
	d = max(d, 10*time.Millisecond)

	var wg sync.WaitGroup
	for _, h := range hosts {
		wg.Go(func() {
			time.Sleep(jitter(d, 0, 1.0)) // stagger host start
			ok, changed := 0, 0
			for _, t := range tasks {
				taskStart(sr, h, t.id, t.name)
				time.Sleep(jitter(d, 0.5, 2.0))
				taskDone(sr, h, t.id, t.name, t.status, "")
				if t.status == "ok" {
					ok++
				} else {
					changed++
				}
			}
			playEnd(sr, h, ok, changed, 0, 0)
		})
	}
	wg.Wait()
}

func runChanged(r output.Renderer, delay time.Duration) {
	playStart(r, "incremental update")

	host := "display-01"

	type step struct {
		id, name, status, msg string
	}

	steps := []step{
		{"check-version", "Check installed version", "ok", ""},
		{"pull-release", "Pull release artifact", "changed", "v2.4.1 → v2.5.0"},
		{"verify-sig", "Verify signature", "ok", ""},
		{"stop-app", "Stop application", "ok", "already stopped"},
		{"extract-release", "Extract release archive", "changed", "42 files updated"},
		{"update-config", "Update config template", "changed", "3 values changed"},
		{"run-migrations", "Run schema migrations", "ok", "no migrations pending"},
		{"start-app", "Start application", "changed", "started PID 4821"},
		{"wait-ready", "Wait for ready signal", "ok", "ready in 1.2s"},
		{"smoke-test", "Smoke test", "ok", ""},
	}

	ok, changed := 0, 0
	for _, s := range steps {
		runTask(r, host, s.id, s.name, s.status, s.msg, delay)
		if s.status == "ok" {
			ok++
		} else {
			changed++
		}
	}

	playEnd(r, host, ok, changed, 0, 0)
}

func runStreaming(r output.Renderer, delay time.Duration) {
	playStart(r, "streamed command output")

	host := "exhibit-pc-03"

	taskStart(r, host, "download-package", "Download package")
	time.Sleep(delay / 2)
	taskOutput(r, host, "download-package", "Download package", "Resolving release manifest...")
	time.Sleep(delay / 2)
	taskOutput(r, host, "download-package", "Download package", "Downloading package payload...")
	time.Sleep(delay / 2)
	taskOutput(r, host, "download-package", "Download package", "Verifying checksum...")
	time.Sleep(delay / 2)
	taskDone(r, host, "download-package", "Download package", "changed", "package downloaded")

	taskStart(r, host, "run-smoke-test", "Run smoke test")
	time.Sleep(delay / 2)
	taskOutput(r, host, "run-smoke-test", "Run smoke test", "Launching kiosk application...")
	time.Sleep(delay / 2)
	taskOutput(r, host, "run-smoke-test", "Run smoke test", "Waiting for HTTP listener on :8080...")
	time.Sleep(delay / 2)
	taskDoneWithOutput(r, host, "run-smoke-test", "Run smoke test", "failed", "process exited with code 1", []string{
		"Launching kiosk application...",
		"Waiting for HTTP listener on :8080...",
		"Smoke test timeout after 15s",
	})

	playEnd(r, host, 0, 1, 1, 0)
}
