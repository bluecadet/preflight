package bundle

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractRejectsAbsoluteEntryPath(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "bundle.zip")
	victim := filepath.Join(t.TempDir(), "victim.txt")
	entryName := filepath.ToSlash(victim)
	manifest := &Manifest{
		Checksums: map[string]string{
			entryName: checksum([]byte("owned")),
		},
	}

	writeRawBundle(t, bundlePath, manifest, map[string][]byte{
		entryName: []byte("owned"),
	})

	_, err := Extract(bundlePath)
	if err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("expected absolute path error, got %v", err)
	}
	if _, statErr := os.Stat(victim); !os.IsNotExist(statErr) {
		t.Fatalf("expected victim path to remain untouched, stat err=%v", statErr)
	}
}

func TestExtractRejectsChecksumMismatch(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "bundle.zip")
	manifest := &Manifest{
		Checksums: map[string]string{
			PlanPath: checksum([]byte(`{"ok":true}`)),
		},
	}

	writeRawBundle(t, bundlePath, manifest, map[string][]byte{
		PlanPath: []byte(`{"ok":false}`),
	})

	_, err := Extract(bundlePath)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestExtractRejectsMissingExpectedFile(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "bundle.zip")
	manifest := &Manifest{
		Checksums: map[string]string{
			PlanPath: checksum([]byte(`{}`)),
		},
	}

	writeRawBundle(t, bundlePath, manifest, nil)

	_, err := Extract(bundlePath)
	if err == nil || !strings.Contains(err.Error(), "missing expected file") {
		t.Fatalf("expected missing expected file error, got %v", err)
	}
}

func TestExtractSucceedsForValidBundle(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "bundle.zip")
	if err := Write(bundlePath, &Manifest{
		PlaybookName:  "test",
		RuntimeBinary: "runtime/preflight",
	}, []FileSpec{
		{Path: PlanPath, Data: []byte(`{"tasks":[]}`)},
		{Path: "runtime/preflight", Data: []byte("binary"), Mode: 0o755},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	extracted, err := Extract(bundlePath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	t.Cleanup(func() {
		if err := extracted.Cleanup(); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
	})

	if extracted.Manifest == nil || extracted.Manifest.PlaybookName != "test" {
		t.Fatalf("unexpected manifest: %#v", extracted.Manifest)
	}
	if extracted.PlanPath == "" {
		t.Fatal("expected extracted plan path")
	}
	if extracted.RuntimePath == "" {
		t.Fatal("expected extracted runtime path")
	}
}

func writeRawBundle(t *testing.T, path string, manifest *Manifest, files map[string][]byte) {
	t.Helper()

	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%q): %v", path, err)
	}
	defer func() {
		if err := out.Close(); err != nil {
			t.Fatalf("Close(%q): %v", path, err)
		}
	}()

	zw := zip.NewWriter(out)
	defer func() {
		if err := zw.Close(); err != nil {
			t.Fatalf("Close zip writer: %v", err)
		}
	}()

	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal manifest: %v", err)
	}
	if err := writeZipFile(zw, ManifestPath, 0o644, manifestBytes); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	for name, data := range files {
		if err := writeZipFile(zw, name, 0o644, data); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}
}
