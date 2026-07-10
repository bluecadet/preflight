package output

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTextRendererNewEventTypes_Snapshots(t *testing.T) {
	t.Parallel()

	for _, tc := range newEventSnapshotCases() {
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

func snapshotPath(prefix, name string) string {
	return filepath.Join("testdata", prefix+"-"+name+".golden")
}

func assertSnapshot(t *testing.T, path, got string) {
	t.Helper()

	if os.Getenv("UPDATE_SNAPSHOTS") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", path, err)
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
