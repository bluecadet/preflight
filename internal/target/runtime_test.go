package target

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestBuildRemoteModuleRegistryFillsUnsupportedModules(t *testing.T) {
	registry := buildRemoteModuleRegistry(RuntimeKindPOSIXShell, ModuleRegistry{
		"shell": moduleFuncs{
			check: func(context.Context, map[string]any, OutputFunc) (CheckResult, error) {
				return CheckResult{}, nil
			},
			apply: func(context.Context, map[string]any, OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, nil
			},
		},
	}, func(module string) error {
		return errors.New("unsupported: " + module)
	})

	if _, ok := registry["shell"]; !ok {
		t.Fatal("expected supported module to remain in registry")
	}

	result, err := executeModule(context.Background(), "task-1", "service", nil, false, nil, registry, errors.New)
	if err == nil || err.Error() != "unsupported: service" {
		t.Fatalf("expected unsupported service error, got result=%+v err=%v", result, err)
	}
}

func TestBuildRemoteModuleRegistryPanicsOnUnknownModule(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for unknown module registration")
		}
	}()

	buildRemoteModuleRegistry(RuntimeKindPOSIXShell, ModuleRegistry{
		"not-a-real-module": moduleFuncs{
			check: func(context.Context, map[string]any, OutputFunc) (CheckResult, error) {
				return CheckResult{}, nil
			},
			apply: func(context.Context, map[string]any, OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, nil
			},
		},
	}, errors.New)
}

func TestExecuteModuleKeepsDefaultMessageForMultilineOutput(t *testing.T) {
	registry := ModuleRegistry{
		"demo": moduleFuncs{
			check: func(context.Context, map[string]any, OutputFunc) (CheckResult, error) {
				return CheckResult{NeedsChange: true}, nil
			},
			apply: apply(func(context.Context, map[string]any) (string, error) {
				return "step one\nstep two\n", nil
			}),
		},
	}

	var gotOutput []string
	result, err := executeModule(context.Background(), "task-1", "demo", nil, false, func(line string) {
		gotOutput = append(gotOutput, line)
	}, registry, errors.New)
	if err != nil {
		t.Fatalf("executeModule returned error: %v", err)
	}
	if result.Message != "change applied" {
		t.Fatalf("result.Message = %q, want %q", result.Message, "change applied")
	}
	want := []string{"step one", "step two"}
	if !reflect.DeepEqual(result.Output, want) {
		t.Fatalf("result.Output = %v, want %v", result.Output, want)
	}
	if !reflect.DeepEqual(gotOutput, want) {
		t.Fatalf("gotOutput = %v, want %v", gotOutput, want)
	}
}

func TestExecuteModuleUsesSingleLineOutputAsMessage(t *testing.T) {
	registry := ModuleRegistry{
		"demo": moduleFuncs{
			check: func(context.Context, map[string]any, OutputFunc) (CheckResult, error) {
				return CheckResult{NeedsChange: true}, nil
			},
			apply: apply(func(context.Context, map[string]any) (string, error) {
				return "applied", nil
			}),
		},
	}

	result, err := executeModule(context.Background(), "task-1", "demo", nil, false, nil, registry, errors.New)
	if err != nil {
		t.Fatalf("executeModule returned error: %v", err)
	}
	if result.Message != "applied" {
		t.Fatalf("result.Message = %q, want %q", result.Message, "applied")
	}
}

func TestExecuteModuleFailedApplyDoesNotInheritChangeAppliedMessage(t *testing.T) {
	registry := ModuleRegistry{
		"demo": moduleFuncs{
			check: func(context.Context, map[string]any, OutputFunc) (CheckResult, error) {
				return CheckResult{NeedsChange: true}, nil
			},
			apply: func(context.Context, map[string]any, OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, errors.New("boom")
			},
		},
	}

	result, err := executeModule(context.Background(), "task-1", "demo", nil, false, nil, registry, errors.New)
	if err == nil {
		t.Fatal("expected apply error, got nil")
	}
	if result.Status != StatusFailed {
		t.Fatalf("result.Status = %q, want %q", result.Status, StatusFailed)
	}
	if result.Message != "" {
		t.Fatalf("result.Message = %q, want empty (failed apply must not show 'change applied')", result.Message)
	}
}

func TestExecuteModuleCapturesCheckOutputDuringDryRun(t *testing.T) {
	registry := ModuleRegistry{
		"demo": moduleFuncs{
			check: func(_ context.Context, _ map[string]any, out OutputFunc) (CheckResult, error) {
				out("checking package A")
				out("checking package B")
				return CheckResult{NeedsChange: true}, nil
			},
			apply: func(context.Context, map[string]any, OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, nil
			},
		},
	}

	var gotOutput []string
	result, err := executeModule(context.Background(), "task-1", "demo", nil, true, func(line string) {
		gotOutput = append(gotOutput, line)
	}, registry, errors.New)
	if err != nil {
		t.Fatalf("executeModule returned error: %v", err)
	}
	want := []string{"checking package A", "checking package B"}
	if !reflect.DeepEqual(gotOutput, want) {
		t.Fatalf("gotOutput = %v, want %v", gotOutput, want)
	}
	if !reflect.DeepEqual(result.Output, want) {
		t.Fatalf("result.Output = %v, want %v", result.Output, want)
	}
	if result.Status != StatusChanged {
		t.Fatalf("result.Status = %q, want %q", result.Status, StatusChanged)
	}
}
