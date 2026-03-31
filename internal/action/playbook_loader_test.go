package action_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/action"
)

func TestLoadPlaybookFileSingleImport(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.yml")
	rootPath := filepath.Join(dir, "root.yml")

	writePlaybook(t, basePath, `
name: base
description: base
vars:
  imported: yes
tasks:
  - name: imported task
    shell:
      cmd: echo
`)
	writePlaybook(t, rootPath, `
name: root
description: root description
import:
  - base.yml
vars:
  root_only: true
tasks:
  - name: root task
    shell:
      cmd: echo
`)

	playbook, err := action.LoadPlaybookFile(rootPath)
	if err != nil {
		t.Fatalf("LoadPlaybookFile returned error: %v", err)
	}

	if playbook.Name != "root" {
		t.Fatalf("expected root name to win, got %q", playbook.Name)
	}
	if playbook.Description != "root description" {
		t.Fatalf("expected root description to win, got %q", playbook.Description)
	}
	if len(playbook.Tasks) != 2 {
		t.Fatalf("expected 2 tasks after merge, got %d", len(playbook.Tasks))
	}
	if playbook.Tasks[0].Name != "imported task" || playbook.Tasks[1].Name != "root task" {
		t.Fatalf("unexpected task order after merge: %q, %q", playbook.Tasks[0].Name, playbook.Tasks[1].Name)
	}
}

func TestLoadPlaybookFileNestedImportsAndVarPrecedence(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.yml")
	extraPath := filepath.Join(dir, "extra.yml")
	midDir := filepath.Join(dir, "nested")
	midPath := filepath.Join(midDir, "mid.yml")
	rootPath := filepath.Join(dir, "root.yml")

	writePlaybook(t, basePath, `
name: base
vars:
  shared: base
  from_base: yes
tasks:
  - name: base task
    shell:
      cmd: echo
`)
	writePlaybook(t, extraPath, `
name: extra
vars:
  shared: extra
  from_extra: yes
tasks:
  - name: extra task
    shell:
      cmd: echo
`)
	writePlaybook(t, midPath, `
name: mid
import:
  - ../base.yml
vars:
  shared: mid
  from_mid: yes
tasks:
  - name: mid task
    shell:
      cmd: echo
`)
	writePlaybook(t, rootPath, `
name: root
import:
  - nested/mid.yml
  - extra.yml
vars:
  shared: root
  from_root: yes
tasks:
  - name: root task
    shell:
      cmd: echo
`)

	playbook, err := action.LoadPlaybookFile(rootPath)
	if err != nil {
		t.Fatalf("LoadPlaybookFile returned error: %v", err)
	}

	if got := playbook.Vars["shared"]; got != "root" {
		t.Fatalf("expected importing playbook to win var precedence, got %#v", got)
	}
	for key := range map[string]bool{
		"from_base":  true,
		"from_mid":   true,
		"from_extra": true,
		"from_root":  true,
	} {
		if _, ok := playbook.Vars[key]; !ok {
			t.Fatalf("expected merged vars to include %q", key)
		}
	}

	wantOrder := []string{"base task", "mid task", "extra task", "root task"}
	if len(playbook.Tasks) != len(wantOrder) {
		t.Fatalf("expected %d tasks after nested merge, got %d", len(wantOrder), len(playbook.Tasks))
	}
	for i, want := range wantOrder {
		if playbook.Tasks[i].Name != want {
			t.Fatalf("task %d: expected %q, got %q", i, want, playbook.Tasks[i].Name)
		}
	}
}

func TestLoadPlaybookFileDetectsImportCycles(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.yml")
	bPath := filepath.Join(dir, "b.yml")

	writePlaybook(t, aPath, `
name: a
import:
  - b.yml
tasks: []
`)
	writePlaybook(t, bPath, `
name: b
import:
  - a.yml
tasks: []
`)

	_, err := action.LoadPlaybookFile(aPath)
	if err == nil {
		t.Fatal("expected import cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "import cycle detected") {
		t.Fatalf("expected cycle error, got %v", err)
	}
	if !strings.Contains(err.Error(), "a.yml") || !strings.Contains(err.Error(), "b.yml") {
		t.Fatalf("expected cycle chain in error, got %v", err)
	}
}

func TestLoadPlaybookFileMissingImportIncludesFile(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "root.yml")

	writePlaybook(t, rootPath, `
name: root
import:
  - missing.yml
tasks: []
`)

	_, err := action.LoadPlaybookFile(rootPath)
	if err == nil {
		t.Fatal("expected missing import error, got nil")
	}
	if !strings.Contains(err.Error(), "missing.yml") {
		t.Fatalf("expected missing import path in error, got %v", err)
	}
}

func writePlaybook(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
