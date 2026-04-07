package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/action"
)

type validateResolver struct {
	actions map[string]*action.Action
}

func (r *validateResolver) Name() string { return "validate-test" }

func (r *validateResolver) Resolve(_ context.Context, ref string) (*action.Action, error) {
	a, ok := r.actions[ref]
	if !ok {
		return nil, fmt.Errorf("action %q not found", ref)
	}
	return a, nil
}

func writeValidatePlaybook(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "playbook.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}

func TestRunValidateResolvesDirectRefs(t *testing.T) {
	dir := t.TempDir()
	path := writeValidatePlaybook(t, dir, `
name: direct
tasks:
  - name: use-action
    uses: myorg/myaction
`)

	resolver := &validateResolver{
		actions: map[string]*action.Action{
			"myorg/myaction": {Tasks: []action.Task{}},
		},
	}

	oldChain := newActionChain
	newActionChain = func(_ string) action.Chain { return action.Chain{resolver} }
	defer func() { newActionChain = oldChain }()

	out, err := captureStdout(t, func() error {
		return runValidate(nil, []string{path})
	})
	if err != nil {
		t.Fatalf("runValidate: %v", err)
	}
	if !strings.Contains(out, "1 action refs resolved") {
		t.Fatalf("expected 1 ref resolved, got %q", out)
	}
}

func TestRunValidateRecursesNestedRefs(t *testing.T) {
	dir := t.TempDir()
	path := writeValidatePlaybook(t, dir, `
name: nested
tasks:
  - name: use-root
    uses: myorg/root
`)

	resolver := &validateResolver{
		actions: map[string]*action.Action{
			"myorg/root": {
				Tasks: []action.Task{
					{Name: "use-child", Uses: "myorg/child"},
				},
			},
			"myorg/child": {
				Tasks: []action.Task{
					{Name: "use-grandchild", Uses: "myorg/grandchild"},
				},
			},
			"myorg/grandchild": {Tasks: []action.Task{}},
		},
	}

	oldChain := newActionChain
	newActionChain = func(_ string) action.Chain { return action.Chain{resolver} }
	defer func() { newActionChain = oldChain }()

	out, err := captureStdout(t, func() error {
		return runValidate(nil, []string{path})
	})
	if err != nil {
		t.Fatalf("runValidate: %v", err)
	}
	if !strings.Contains(out, "3 action refs resolved") {
		t.Fatalf("expected 3 refs resolved, got %q", out)
	}
}

func TestRunValidateReportsNestedMissingRef(t *testing.T) {
	dir := t.TempDir()
	path := writeValidatePlaybook(t, dir, `
name: broken-nested
tasks:
  - name: use-root
    uses: myorg/root
`)

	resolver := &validateResolver{
		actions: map[string]*action.Action{
			"myorg/root": {
				Tasks: []action.Task{
					{Name: "use-missing", Uses: "myorg/missing"},
				},
			},
		},
	}

	oldChain := newActionChain
	newActionChain = func(_ string) action.Chain { return action.Chain{resolver} }
	defer func() { newActionChain = oldChain }()

	err := runValidate(nil, []string{path})
	if err == nil {
		t.Fatal("expected validation error for missing nested ref")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunValidateCycleDoesNotLoopInfinitely(t *testing.T) {
	dir := t.TempDir()
	path := writeValidatePlaybook(t, dir, `
name: cycle
tasks:
  - name: use-a
    uses: myorg/a
`)

	resolver := &validateResolver{
		actions: map[string]*action.Action{
			"myorg/a": {
				Tasks: []action.Task{
					{Name: "use-b", Uses: "myorg/b"},
				},
			},
			"myorg/b": {
				Tasks: []action.Task{
					{Name: "use-a", Uses: "myorg/a"},
				},
			},
		},
	}

	oldChain := newActionChain
	newActionChain = func(_ string) action.Chain { return action.Chain{resolver} }
	defer func() { newActionChain = oldChain }()

	out, err := captureStdout(t, func() error {
		return runValidate(nil, []string{path})
	})
	if err != nil {
		t.Fatalf("cycle should not cause an error, got: %v", err)
	}
	if !strings.Contains(out, "2 action refs resolved") {
		t.Fatalf("expected 2 refs resolved (cycle visited once each), got %q", out)
	}
}

func TestRunValidateLocalActionRefs(t *testing.T) {
	dir := t.TempDir()
	actionsDir := filepath.Join(dir, "actions", "myorg", "mylocal")
	if err := os.MkdirAll(actionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(actionsDir, "action.yml"), []byte(`
name: mylocal
tasks:
  - name: echo
    shell:
      cmd: echo
      args: ["hello"]
`), 0o644); err != nil {
		t.Fatalf("WriteFile(action.yml): %v", err)
	}

	playbookPath := filepath.Join(dir, "playbook.yml")
	if err := os.WriteFile(playbookPath, []byte(`
name: local-test
tasks:
  - name: use local
    uses: myorg/mylocal
`), 0o644); err != nil {
		t.Fatalf("WriteFile(playbook.yml): %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runValidate(nil, []string{playbookPath})
	})
	if err != nil {
		t.Fatalf("runValidate: %v", err)
	}
	if !strings.Contains(out, "1 action refs resolved") {
		t.Fatalf("expected 1 ref resolved, got %q", out)
	}
}

func TestRunValidateStdlibRefs(t *testing.T) {
	dir := t.TempDir()
	playbookPath := filepath.Join(dir, "playbook.yml")
	if err := os.WriteFile(playbookPath, []byte(`
name: stdlib-test
tasks:
  - name: use stdlib
    uses: preflight/windows-machine
`), 0o644); err != nil {
		t.Fatalf("WriteFile(playbook.yml): %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runValidate(nil, []string{playbookPath})
	})
	if err != nil {
		t.Fatalf("runValidate with stdlib ref: %v", err)
	}
	if !strings.Contains(out, "action refs resolved") {
		t.Fatalf("expected refs resolved message, got %q", out)
	}
}
