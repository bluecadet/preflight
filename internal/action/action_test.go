package action_test

import (
	"context"
	"fmt"
	"testing"

	"gopkg.in/yaml.v3"

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
    id: shell-step
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
	if a.Tasks[0].Module != "shell" {
		t.Errorf("expected canonical module shell, got %q", a.Tasks[0].Module)
	}
	if a.Tasks[0].Params == nil {
		t.Fatal("expected canonical params to be populated")
	}
	if a.Tasks[0].Key() != "shell-step" {
		t.Fatalf("expected stable key shell-step, got %q", a.Tasks[0].Key())
	}
	if got := a.Tasks[0].InlineModules["shell"]["cmd"]; got != "echo" {
		t.Fatalf("expected inline shell cmd=echo, got %#v", got)
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
	if _, err := action.ParseAction([]byte(yaml)); err == nil {
		t.Error("expected parse error when both uses and inline module are set")
	}
}

func TestParseAction_ExplicitModuleCanonicalizes(t *testing.T) {
	a, err := action.ParseAction([]byte(`
name: myorg/test-action
tasks:
  - name: explicit
    module: shell
    params:
      cmd: echo
      args: ["hello"]
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	task := a.Tasks[0]
	if task.Module != "shell" {
		t.Fatalf("expected module shell, got %q", task.Module)
	}
	if got := task.Params["cmd"]; got != "echo" {
		t.Fatalf("expected params.cmd=echo, got %#v", got)
	}
}

func TestTask_UnmarshalYAML_CollectsInlineModules(t *testing.T) {
	var task action.Task
	err := yaml.Unmarshal([]byte(`
name: create file
file:
  dest: /foo
`), &task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := task.InlineModules["file"]["dest"]; got != "/foo" {
		t.Fatalf("expected inline file dest=/foo, got %#v", got)
	}
}

func TestParseAction_RejectsParamsWithoutModule(t *testing.T) {
	_, err := action.ParseAction([]byte(`
name: myorg/test-action
tasks:
  - name: broken
    params:
      cmd: echo
`))
	if err == nil {
		t.Fatal("expected error when params are set without module")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected non-empty error")
	}
}

func TestValidateActionYAML_SchemaValidationFailure(t *testing.T) {
	err := action.ValidateActionYAML([]byte(`
name: myorg/bad-action
tasks:
  - shell:
      cmd: echo
`))
	if err == nil {
		t.Fatal("expected schema validation error")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected non-empty error")
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
func (r *nilRes) Name() string                                                { return "nil" }

// errRes always returns an error.
type errRes struct{}

func (r *errRes) Resolve(_ context.Context, ref string) (*action.Action, error) {
	return nil, fmt.Errorf("errRes: no action for %q", ref)
}
func (r *errRes) Name() string { return "err" }
