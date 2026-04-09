package output

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTextRendererSnapshots(t *testing.T) {
	t.Parallel()

	for _, tc := range snapshotCases() {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := NewTextRendererWithOptions(&buf, tc.opts)
			for _, event := range tc.events {
				r.Emit(event)
			}
			r.Close()

			assertSnapshot(t, snapshotPath("text", tc.name), normalizeSnapshot(buf.String()))
		})
	}
}

func TestTUIRendererSnapshots(t *testing.T) {
	t.Parallel()

	for _, tc := range snapshotCases() {
		t.Run(tc.name, func(t *testing.T) {
			assertSnapshot(t, snapshotPath("tui", tc.name), normalizeTUISnapshot(renderTUISnapshot(tc)))
		})
	}
}

type snapshotCase struct {
	name   string
	opts   Options
	events []Event
}

func snapshotCases() []snapshotCase {
	return []snapshotCase{
		{
			name: "play-summary",
			events: []Event{
				PlayStartEvent{PlayName: "gallery rollout"},
				TaskResultEvent{
					Target:   "lobby-pc-01",
					TaskID:   "preflight-check",
					TaskName: "Preflight check",
					Status:   "ok",
				},
				TaskResultEvent{
					Target:   "lobby-pc-01",
					TaskID:   "install-runtime",
					TaskName: "Install runtime",
					Status:   "changed",
				},
				TaskResultEvent{
					Target:   "lobby-pc-01",
					TaskID:   "smoke-test",
					TaskName: "Smoke test",
					Status:   "failed",
					Message:  "timeout",
					Output:   []string{"Ping kiosk app", "Timed out waiting for response"},
				},
				TaskResultEvent{
					Target:   "lobby-pc-01",
					TaskID:   "cleanup",
					TaskName: "Cleanup",
					Status:   "skipped",
					Message:  "dependency-failed",
				},
				PlayEndEvent{
					Target:       "lobby-pc-01",
					OKCount:      1,
					ChangedCount: 1,
					FailedCount:  1,
					SkippedCount: 1,
				},
			},
		},
		{
			name: "facts",
			events: []Event{
				FactsEvent{
					Target: "lobby-pc-01",
					Facts: map[string]any{
						"hostname": "LOBBY-PC-01",
						"os": map[string]any{
							"name":     "Windows 11",
							"version":  "10.0.26200",
							"build":    26200,
							"arch":     "arm64",
							"hostname": "LOBBY-PC-01",
						},
						"disks": []any{
							map[string]any{
								"path":     "C:",
								"total_gb": 63.055660247802734,
								"free_gb":  23.208858489990234,
								"used_gb":  39.8468017578125,
							},
						},
						"env": map[string]any{
							"Path": "C:\\WINDOWS\\system32;C:\\WINDOWS;C:\\WINDOWS\\System32\\WindowsPowerShell\\v1.0\\",
						},
					},
				},
			},
		},
		{
			name: "plan",
			events: []Event{
				PlanEvent{
					Target:       "lobby-pc-01",
					PlaybookName: "lobby",
					Tasks: []PlanTaskEntry{
						{Number: 1, Module: "shell", Name: "Preflight check"},
						{Number: 2, Module: "directory", Name: "Create content root", Tags: []string{"content"}},
					},
				},
			},
		},
		{
			name: "state",
			events: []Event{
				StateEvent{
					Target:       "lobby-pc-01",
					PlaybookName: "lobby",
					StatePath:    "state/targets/lobby-pc-01.json",
					LastApplied:  "2026-04-09 12:00:00 UTC",
					Comparisons: []StateComparison{
						{Status: "UNCHANGED", TaskName: "Preflight check", Module: "shell", RecordedStatus: "ok"},
						{Status: "CHANGED", TaskName: "Create content root", Module: "directory", RecordedStatus: "changed"},
					},
				},
			},
		},
		{
			name: "validate",
			events: []Event{
				ValidationEvent{
					PlaybookPath:    "playbooks/lobby.yml",
					PlaybookName:    "lobby",
					TaskCount:       3,
					VisitedRefCount: 2,
					ResolvedRefs: []string{
						"preflight/windows-machine",
						"preflight/windows-quiet-mode",
					},
				},
			},
		},
		{
			name: "action-list",
			events: []Event{
				ActionCatalogEvent{
					EmbeddedNamespace: "preflight/",
					EmbeddedRefs: []string{
						"preflight/autologin",
						"preflight/windows-machine",
					},
					LocalDir: "actions",
					LocalRefs: []string{
						"museum/bootstrap",
					},
				},
			},
		},
		{
			name: "action-info",
			events: []Event{
				ActionInfoEvent{
					Ref:         "preflight/autologin",
					Name:        "autologin",
					Version:     "1.2.0",
					Description: "Configure kiosk autologin",
					Author:      "Bluecadet",
					Inputs: []ActionInputEntry{
						{
							Name:        "username",
							Type:        "string",
							Description: "Account used for sign-in",
							Required:    true,
						},
						{
							Name:        "password",
							Type:        "string",
							Description: "Password for the account",
							Default:     "(prompted)",
						},
					},
					TaskNames: []string{
						"Enable autologin",
						"Restart shell",
					},
				},
			},
		},
		{
			name: "action-fetch",
			events: []Event{
				ActionFetchEvent{
					Entries: []ActionFetchEntry{
						{Ref: "github.com/acme/root@v1", SHA: "abc123"},
						{Ref: "github.com/acme/child@v2", SHA: "def456"},
					},
				},
			},
		},
	}
}

func snapshotPath(prefix, name string) string {
	return filepath.Join("testdata", prefix+"-"+name+".golden")
}

func assertSnapshot(t *testing.T, path, got string) {
	t.Helper()

	if os.Getenv("UPDATE_SNAPSHOTS") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	if diff := compareSnapshot(string(want), got); diff != "" {
		t.Fatalf("snapshot mismatch for %s:\n%s", path, diff)
	}
}

func compareSnapshot(want, got string) string {
	if want == got {
		return ""
	}
	return "want:\n" + want + "\n\ngot:\n" + got
}

func normalizeSnapshot(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}

var (
	durationRE = regexp.MustCompile(`\b\d+(?:\.\d+)?(?:ms|s|m|h)\b`)
)

func normalizeTUISnapshot(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	s = durationRE.ReplaceAllString(s, "<elapsed>")
	return normalizeSnapshot(s)
}

func renderTUISnapshot(tc snapshotCase) string {
	model := newTUIModelWithOptions(make(chan Event), tc.opts)
	var blocks []string
	for _, event := range tc.events {
		next, cmd := model.applyEvent(event)
		model = next
		blocks = append(blocks, collectPrintedBlocks(cmd)...)
	}
	model.done = true
	final := strings.TrimSpace(model.View())
	if final != "" {
		blocks = append(blocks, final)
	}
	return strings.Join(blocks, "\n")
}

func collectPrintedBlocks(cmd tea.Cmd) []string {
	if cmd == nil {
		return nil
	}
	return collectPrintedMessages(cmd())
}

func collectPrintedMessages(msg tea.Msg) []string {
	if msg == nil {
		return nil
	}

	if m, ok := msg.(tea.BatchMsg); ok {
		var blocks []string
		for _, cmd := range m {
			blocks = append(blocks, collectPrintedBlocks(cmd)...)
		}
		return blocks
	}

	rv := reflect.ValueOf(msg)
	if !rv.IsValid() {
		return nil
	}

	rt := rv.Type()
	if rt.PkgPath() == "github.com/charmbracelet/bubbletea" && rt.Name() == "printLineMessage" {
		field := rv.FieldByName("messageBody")
		if field.IsValid() && field.Kind() == reflect.String {
			return []string{field.String()}
		}
		return nil
	}

	if rt.PkgPath() == "github.com/charmbracelet/bubbletea" && rt.Name() == "sequenceMsg" && rv.Kind() == reflect.Slice {
		var blocks []string
		for i := 0; i < rv.Len(); i++ {
			if cmd, ok := rv.Index(i).Interface().(tea.Cmd); ok {
				blocks = append(blocks, collectPrintedBlocks(cmd)...)
			}
		}
		return blocks
	}

	return nil
}
