package main

import (
	"math/rand"
	"sync"
	"time"

	"github.com/bluecadet/preflight/internal/output"
)

// targetDecl describes a target with its transport and address for simulator scenarios.
type targetDecl struct {
	name      string
	transport string
	address   string
}

// emit helpers

func playStart(r output.Renderer, name string) {
	r.Emit(output.RunStartEvent{
		PlaybookName: name,
		Targets:      []string{"exhibit-pc-01"},
	})
}

func playStartN(r output.Renderer, name string, targets []targetDecl) {
	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = t.name
	}
	r.Emit(output.RunStartEvent{
		PlaybookName: name,
		Targets:      names,
	})
}

func emitTargetStart(r output.Renderer, t targetDecl) {
	r.Emit(output.TargetStartEvent{
		Target:    t.name,
		Transport: t.transport,
		Address:   t.address,
	})
}

func emitRunSummary(r output.Renderer, ok, changed, failed, skipped int) {
	r.Emit(output.RunSummaryEvent{
		Status:       "ok",
		OKCount:      ok,
		ChangedCount: changed,
		FailedCount:  failed,
		SkippedCount: skipped,
		ElapsedMs:    1000,
	})
}

func taskStart(r output.Renderer, host, id, name string) {
	r.Emit(output.TaskStartedEvent{Target: host, TaskID: id, TaskName: name, Module: "command", ActionPath: ""})
}

func taskDone(r output.Renderer, host, id, name, status, msg string) {
	taskDoneWithOutput(r, host, id, name, status, msg, nil, nil)
}

func taskDoneWithOutput(r output.Renderer, host, id, name, status, msg string, outputLines, liveLines []string) {
	switch status {
	case "ok":
		r.Emit(output.TaskOKEvent{Target: host, TaskID: id, TaskName: name, ElapsedMs: 100})
	case "changed":
		r.Emit(output.TaskChangedEvent{Target: host, TaskID: id, TaskName: name, ElapsedMs: 100})
	case "failed":
		r.Emit(output.TaskFailedEvent{Target: host, TaskID: id, TaskName: name, ElapsedMs: 100, FailMessage: msg, Output: outputLines})
	case "skipped":
		r.Emit(output.TaskSkippedEvent{Target: host, TaskID: id, TaskName: name, Reason: msg})
	default:
		r.Emit(output.TaskOKEvent{Target: host, TaskID: id, TaskName: name, ElapsedMs: 100})
	}
}

func taskOutput(r output.Renderer, host, id, name string, lines ...string) {
	r.Emit(output.TaskOutputEvent{
		Target:   host,
		TaskID:   id,
		TaskName: name,
		Lines:    lines,
	})
}

func playEnd(r output.Renderer, host string, ok, changed, failed, skipped int) {
	outcome := "ok"
	if failed > 0 {
		outcome = "failed"
	}
	r.Emit(output.TargetCompleteEvent{
		Target:       host,
		Outcome:      outcome,
		OKCount:      ok,
		ChangedCount: changed,
		FailedCount:  failed,
		SkippedCount: skipped,
		ElapsedMs:    1000,
	})
}

func runTask(r output.Renderer, host, id, name, status, msg string, delay time.Duration) {
	taskStart(r, host, id, name)
	time.Sleep(delay)
	taskDone(r, host, id, name, status, msg)
}

func runStreamingTask(r output.Renderer, host, id, name, status, msg string, liveLines, outputLines []string, delay time.Duration) {
	taskStart(r, host, id, name)

	stepDelay := delay
	if steps := len(liveLines) + 1; steps > 0 {
		stepDelay = delay / time.Duration(steps)
	}
	for _, line := range liveLines {
		time.Sleep(stepDelay)
		taskOutput(r, host, id, name, line)
	}
	time.Sleep(stepDelay)
	if outputLines == nil {
		outputLines = append([]string(nil), liveLines...)
	}
	taskDoneWithOutput(r, host, id, name, status, msg, outputLines, liveLines)
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
	targets := []targetDecl{
		{name: "gallery-01", transport: "ssh", address: "[[IP_ADDRESS]]:22"},
		{name: "gallery-02", transport: "ssh", address: "[[IP_ADDRESS]]:22"},
		{name: "gallery-03", transport: "ssh", address: "[[IP_ADDRESS]]:22"},
	}
	playStartN(r, "gallery rollout", targets)

	sr := output.Synchronized(r)

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
	for _, t := range targets {
		wg.Go(func() {
			emitTargetStart(sr, t)
			h := t.name
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
	runStreamingTask(r, host, "run-migrations", "Run database migrations", "failed", "connection refused: postgres:5432", []string{
		"Connecting to postgres...",
		"Applying migration 20260402_add_sessions...",
		"Retrying connection after transient failure...",
	}, []string{
		"Connecting to postgres...",
		"Applying migration 20260402_add_sessions...",
		"Retrying connection after transient failure...",
		"Migration aborted: connection refused: postgres:5432",
	}, delay*2)

	// dependent tasks get skipped
	taskDone(r, host, "start-service", "Start service", "skipped", "dependency-failed")
	taskDone(r, host, "smoke-test", "Smoke test", "skipped", "dependency-failed")

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

	taskDone(r, host, "install-directx", "Install DirectX", "skipped", "when-condition-false")

	runTask(r, host, "install-codec", "Install codec pack", "changed", "", delay)

	taskDone(r, host, "enable-gpu-debug", "Enable GPU debug layer", "skipped", "tag-filtered")
	taskDone(r, host, "install-pix", "Install PIX profiler", "skipped", "tag-filtered")

	runTask(r, host, "configure-output", "Configure display output", "ok", "", delay)

	playEnd(r, host, 2, 1, 0, 3)
}

func runLarge(r output.Renderer, delay time.Duration) {
	targets := []targetDecl{
		{name: "exhibit-01", transport: "ssh", address: "[[IP_ADDRESS]]:22"},
		{name: "exhibit-02", transport: "ssh", address: "[[IP_ADDRESS]]:22"},
		{name: "exhibit-03", transport: "ssh", address: "[[IP_ADDRESS]]:22"},
		{name: "exhibit-04", transport: "ssh", address: "[[IP_ADDRESS]]:22"},
		{name: "exhibit-05", transport: "winrm", address: "[[IP_ADDRESS]]:5986"},
		{name: "exhibit-06", transport: "winrm", address: "[[IP_ADDRESS]]:5986"},
		{name: "kiosk-01", transport: "local", address: ""},
		{name: "kiosk-02", transport: "local", address: ""},
	}
	playStartN(r, "fleet rollout", targets)

	sr := output.Synchronized(r)

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
	for _, t := range targets {
		wg.Go(func() {
			emitTargetStart(sr, t)
			h := t.name
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

	runStreamingTask(r, host, "download-package", "Download package", "changed", "package downloaded", []string{
		"Resolving release manifest...",
		"Downloading package metadata...",
		"Downloading package payload...",
		"Verifying checksum...",
		"Extracting artifact into staging directory...",
	}, nil, delay)

	runStreamingTask(r, host, "run-smoke-test", "Run smoke test", "failed", "process exited with code 1", []string{
		"Launching kiosk application...",
		"Waiting for HTTP listener on :8080...",
		"Checking splash-screen readiness signal...",
		"Capturing failure diagnostics bundle...",
	}, []string{
		"Launching kiosk application...",
		"Waiting for HTTP listener on :8080...",
		"Checking splash-screen readiness signal...",
		"Capturing failure diagnostics bundle...",
		"Smoke test timeout after 15s",
	}, delay)

	playEnd(r, host, 0, 1, 1, 0)
}

func runRoster(r output.Renderer, delay time.Duration) {
	targets := []targetDecl{
		{name: "edge-gateway", transport: "ssh", address: "[10.0.1.10]:22"},
		{name: "kiosk-01", transport: "local", address: ""},
		{name: "dc-controller", transport: "winrm", address: "[10.0.2.50]:5986"},
		{name: "media-player-03", transport: "ssh", address: "[10.0.1.85]:22"},
	}
	playStartN(r, "multi-transport rollout", targets)

	sr := output.Synchronized(r)

	steps := []struct {
		id, name, status string
	}{
		{"connect", "Establish connection", "ok"},
		{"gather-facts", "Gather system facts", "ok"},
		{"install-agent", "Install monitoring agent", "changed"},
		{"configure-logs", "Configure log shipping", "changed"},
		{"verify-service", "Verify agent service", "ok"},
	}

	var (
		wg          sync.WaitGroup
		totalOK     int
		totalChg    int
		totalFailed int
		totalSkip   int
		mu          sync.Mutex
	)

	for _, t := range targets {
		wg.Go(func() {
			emitTargetStart(sr, t)
			time.Sleep(jitter(delay, 0.1, 0.3))

			ok, changed := 0, 0
			for _, s := range steps {
				taskStart(sr, t.name, s.id, s.name)
				time.Sleep(jitter(delay, 0.3, 1.2))
				taskDone(sr, t.name, s.id, s.name, s.status, "")
				if s.status == "ok" {
					ok++
				} else {
					changed++
				}
			}
			playEnd(sr, t.name, ok, changed, 0, 0)

			mu.Lock()
			totalOK += ok
			totalChg += changed
			mu.Unlock()
		})
	}
	wg.Wait()

	emitRunSummary(sr, totalOK, totalChg, totalFailed, totalSkip)
}

func runInlinePrefixes(r output.Renderer, delay time.Duration) {
	targets := []targetDecl{
		{name: "web-01", transport: "ssh", address: "[10.0.0.10]:22"},
		{name: "web-02", transport: "ssh", address: "[10.0.0.11]:22"},
		{name: "win-srv", transport: "winrm", address: "[10.0.0.50]:5986"},
		{name: "local-node", transport: "local", address: ""},
	}
	playStartN(r, "mixed transport deployment", targets)

	sr := output.Synchronized(r)

	type stepDef struct {
		id, name, status, msg string
	}

	steps := []stepDef{
		{"ping", "Ping target", "ok", ""},
		{"os-check", "Check OS version", "ok", ""},
		{"install-runtime", "Install runtime", "changed", "v3.2.1 installed"},
		{"deploy-config", "Deploy configuration", "changed", "42 values synced"},
		{"restart-service", "Restart service", "ok", ""},
		{"health-check", "Health check", "ok", ""},
	}

	var (
		wg          sync.WaitGroup
		totalOK     int
		totalChg    int
		totalFailed int
		totalSkip   int
		mu          sync.Mutex
	)

	for _, t := range targets {
		wg.Go(func() {
			emitTargetStart(sr, t)
			time.Sleep(jitter(delay, 0.1, 0.4))

			ok, changed := 0, 0
			for _, s := range steps {
				taskStart(sr, t.name, s.id, s.name)
				time.Sleep(jitter(delay, 0.4, 1.5))
				taskDone(sr, t.name, s.id, s.name, s.status, s.msg)
				if s.status == "ok" {
					ok++
				} else {
					changed++
				}
			}
			playEnd(sr, t.name, ok, changed, 0, 0)

			mu.Lock()
			totalOK += ok
			totalChg += changed
			mu.Unlock()
		})
	}
	wg.Wait()

	emitRunSummary(sr, totalOK, totalChg, totalFailed, totalSkip)
}

func runStreamingMultiHost(r output.Renderer, delay time.Duration) {
	playStartN(r, "streaming multi-host rollout", []targetDecl{
		{name: "gallery-01", transport: "ssh", address: "[[IP_ADDRESS]]:22"},
		{name: "gallery-02", transport: "ssh", address: "[[IP_ADDRESS]]:22"},
		{name: "gallery-03", transport: "ssh", address: "[[IP_ADDRESS]]:22"},
	})

	sr := output.Synchronized(r)

	hosts := []struct {
		name string
		fail bool
	}{
		{name: "gallery-01"},
		{name: "gallery-02", fail: true},
		{name: "gallery-03"},
	}

	var wg sync.WaitGroup
	for _, h := range hosts {
		wg.Go(func() {
			emitTargetStart(sr, targetDecl{name: h.name, transport: "ssh", address: "[[IP_ADDRESS]]:22"})
			runStreamingTask(sr, h.name, "sync-assets", "Sync assets", "changed", "assets synchronized", []string{
				"Inspecting existing asset manifest...",
				"Downloading changed assets...",
				"Verifying transferred files...",
				"Activating new asset bundle...",
			}, nil, jitter(delay, 0.6, 1.4))

			if h.fail {
				runStreamingTask(sr, h.name, "smoke-test", "Smoke test", "failed", "HTTP 500 from kiosk app", []string{
					"Launching kiosk runtime...",
					"Waiting for health endpoint...",
					"Fetching diagnostics from /debug/status...",
				}, []string{
					"Launching kiosk runtime...",
					"Waiting for health endpoint...",
					"Fetching diagnostics from /debug/status...",
					"Smoke test failed: HTTP 500 from kiosk app",
				}, jitter(delay, 0.6, 1.4))
				playEnd(sr, h.name, 0, 1, 1, 0)
				return
			}

			runTask(sr, h.name, "smoke-test", "Smoke test", "ok", "", jitter(delay, 0.6, 1.4))
			playEnd(sr, h.name, 1, 1, 0, 0)
		})
	}
	wg.Wait()
}
