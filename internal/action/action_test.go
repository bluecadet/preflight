package action_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/bluecadet/preflight/internal/action"
)

const sampleAction = `
name: myorg/test-action
version: "1.0.0"
description: A test action
author: test

inputs:
  app_path:
    type: path
    required: true
    description: Path to the app
  debug:
    type: bool
    default: false

tasks:
  - name: Run shell command
    shell:
      cmd: echo
      args: ["hello"]
`

const samplePlaybook = `
name: test-playbook
description: A test playbook

vars:
  env: production

tasks:
  - name: Apply kiosk mode
    uses: preflight/kiosk-mode
    with:
      shell_app: "C:\\App\\app.exe"
`

func TestParseAction_Valid(t *testing.T) {
	a, err := action.ParseAction([]byte(sampleAction))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name != "myorg/test-action" {
		t.Errorf("expected name myorg/test-action, got %q", a.Name)
	}
	if len(a.Inputs) != 2 {
		t.Errorf("expected 2 inputs, got %d", len(a.Inputs))
	}
	if len(a.Tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(a.Tasks))
	}
}

func TestParseAction_Invalid(t *testing.T) {
	_, err := action.ParseAction([]byte(":\tbad yaml\t:"))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestParsePlaybook_Valid(t *testing.T) {
	p, err := action.ParsePlaybook([]byte(samplePlaybook))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "test-playbook" {
		t.Errorf("expected name test-playbook, got %q", p.Name)
	}
	if p.Vars["env"] != "production" {
		t.Errorf("expected vars.env=production, got %v", p.Vars["env"])
	}
	if len(p.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(p.Tasks))
	}
	if p.Tasks[0].Uses != "preflight/kiosk-mode" {
		t.Errorf("expected uses=preflight/kiosk-mode, got %q", p.Tasks[0].Uses)
	}
}

func TestTask_ResolveModule(t *testing.T) {
	a, _ := action.ParseAction([]byte(sampleAction))
	task := &a.Tasks[0]
	if err := task.ResolveModule(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Module != "shell" {
		t.Errorf("expected module=shell, got %q", task.Module)
	}
	if task.Params == nil {
		t.Error("expected params to be set")
	}
}

func TestTask_ResolveModule_BothUsesAndInline(t *testing.T) {
	yaml := `
name: test
tasks:
  - name: bad task
    uses: some/action
    shell:
      cmd: echo
`
	a, _ := action.ParseAction([]byte(yaml))
	if err := a.Tasks[0].ResolveModule(); err == nil {
		t.Error("expected error when both uses and inline module are set")
	}
}

func TestChain_FallThrough(t *testing.T) {
	// A resolver that always returns nil falls through to the next.
	nilResolver := &nilRes{}
	errResolver := &errRes{}
	chain := action.Chain{nilResolver, errResolver}
	_, err := chain.Resolve(context.Background(), "any/ref")
	if err == nil {
		t.Error("expected error from errResolver after nil fallthrough")
	}
}

func TestChain_NoMatch(t *testing.T) {
	chain := action.Chain{&nilRes{}, &nilRes{}}
	_, err := chain.Resolve(context.Background(), "missing/action")
	if err == nil {
		t.Error("expected error when no resolver matches")
	}
}

// nilRes always returns (nil, nil) — doesn't handle any ref.
type nilRes struct{}

func (r *nilRes) Resolve(_ context.Context, _ string) (*action.Action, error) { return nil, nil }
func (r *nilRes) Name() string                                                  { return "nil" }

// errRes always returns an error.
type errRes struct{}

func (r *errRes) Resolve(_ context.Context, ref string) (*action.Action, error) {
	return nil, fmt.Errorf("errRes: no action for %q", ref)
}
func (r *errRes) Name() string { return "err" }
