package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluecadet/preflight/internal/action"
)

type commandFetchResolver struct {
	results map[string]*action.FetchResult
	calls   []string
}

func (r *commandFetchResolver) Name() string { return "command-fetch" }

func (r *commandFetchResolver) Resolve(_ context.Context, _ string) (*action.Action, error) {
	return nil, nil
}

func (r *commandFetchResolver) Fetch(_ context.Context, ref string) (*action.FetchResult, error) {
	r.calls = append(r.calls, ref)
	return r.results[ref], nil
}

func TestRunActionFetchRecursesRemoteDependencies(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	projectDir := t.TempDir()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir(%q): %v", projectDir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("Chdir(%q): %v", cwd, err)
		}
	})

	oldChain := newActionChain
	defer func() { newActionChain = oldChain }()

	rootRef := "github.com/acme/actions/root@v1"
	childRef := "github.com/acme/actions/child@v2"
	resolver := &commandFetchResolver{
		results: map[string]*action.FetchResult{
			rootRef: {
				Entry: action.LockEntry{Ref: rootRef, SHA: "a", Pinned: "github.com/acme/actions/root@a"},
				Action: &action.Action{Tasks: []action.Task{
					{Name: "child", Uses: childRef},
				}},
			},
			childRef: {
				Entry:  action.LockEntry{Ref: childRef, SHA: "b", Pinned: "github.com/acme/actions/child@b"},
				Action: &action.Action{},
			},
		},
	}
	newActionChain = func(_ string) action.Chain {
		return action.Chain{resolver}
	}

	if err := runActionFetch(nil, []string{rootRef}); err != nil {
		t.Fatalf("runActionFetch: %v", err)
	}
	if len(resolver.calls) != 2 {
		t.Fatalf("expected 2 fetch calls, got %d", len(resolver.calls))
	}
	if resolver.calls[0] != rootRef || resolver.calls[1] != childRef {
		t.Fatalf("unexpected fetch order: %#v", resolver.calls)
	}
}

func TestPlaybookDirFindsProjectRootAbovePlaybooksDirectory(t *testing.T) {
	projectDir := t.TempDir()
	playbookPath := filepath.Join(projectDir, "playbooks", "lobby.yml")
	if err := os.MkdirAll(filepath.Dir(playbookPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "preflight.yml"), []byte("project: test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(preflight.yml): %v", err)
	}
	if err := os.WriteFile(playbookPath, []byte("name: lobby\ntasks: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(playbook): %v", err)
	}

	got, err := playbookDir(playbookPath)
	if err != nil {
		t.Fatalf("playbookDir: %v", err)
	}
	if got != projectDir {
		t.Fatalf("project dir: got %q want %q", got, projectDir)
	}
}
