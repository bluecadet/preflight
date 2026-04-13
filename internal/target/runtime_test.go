package target

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestBuildRemoteModuleRegistryFillsUnsupportedModules(t *testing.T) {
	registry := buildRemoteModuleRegistry(RuntimeKindPOSIXShell, remoteModuleRegistry{
		"shell": remoteModuleFuncs{
			check: func(context.Context, map[string]any) (bool, string, error) {
				return false, "", nil
			},
			apply: func(context.Context, map[string]any) (string, error) {
				return "", nil
			},
		},
	}, func(module string) error {
		return errors.New("unsupported: " + module)
	})

	if _, ok := registry["shell"]; !ok {
		t.Fatal("expected supported module to remain in registry")
	}

	result, err := executeRemoteModule(context.Background(), "task-1", "service", nil, false, nil, registry, errors.New)
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

	buildRemoteModuleRegistry(RuntimeKindPOSIXShell, remoteModuleRegistry{
		"not-a-real-module": remoteModuleFuncs{
			check: func(context.Context, map[string]any) (bool, string, error) {
				return false, "", nil
			},
			apply: func(context.Context, map[string]any) (string, error) {
				return "", nil
			},
		},
	}, errors.New)
}

func TestExecuteRemoteModuleKeepsDefaultMessageForMultilineOutput(t *testing.T) {
	registry := remoteModuleRegistry{
		"demo": remoteModuleFuncs{
			check: func(context.Context, map[string]any) (bool, string, error) {
				return true, "", nil
			},
			apply: func(context.Context, map[string]any) (string, error) {
				return "step one\nstep two\n", nil
			},
		},
	}

	var gotOutput []string
	result, err := executeRemoteModule(context.Background(), "task-1", "demo", nil, false, func(line string) {
		gotOutput = append(gotOutput, line)
	}, registry, errors.New)
	if err != nil {
		t.Fatalf("executeRemoteModule returned error: %v", err)
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

func TestExecuteRemoteModuleUsesSingleLineOutputAsMessage(t *testing.T) {
	registry := remoteModuleRegistry{
		"demo": remoteModuleFuncs{
			check: func(context.Context, map[string]any) (bool, string, error) {
				return true, "", nil
			},
			apply: func(context.Context, map[string]any) (string, error) {
				return "applied", nil
			},
		},
	}

	result, err := executeRemoteModule(context.Background(), "task-1", "demo", nil, false, nil, registry, errors.New)
	if err != nil {
		t.Fatalf("executeRemoteModule returned error: %v", err)
	}
	if result.Message != "applied" {
		t.Fatalf("result.Message = %q, want %q", result.Message, "applied")
	}
}

func TestExecuteRemoteModuleCapturesCheckOutputDuringDryRun(t *testing.T) {
	registry := remoteModuleRegistry{
		"demo": remoteModuleFuncs{
			checkWithOutput: func(_ context.Context, _ map[string]any, onOutput OutputFunc) (bool, string, error) {
				onOutput("checking package A")
				onOutput("checking package B")
				return true, "", nil
			},
			apply: func(context.Context, map[string]any) (string, error) {
				return "", nil
			},
		},
	}

	var gotOutput []string
	result, err := executeRemoteModule(context.Background(), "task-1", "demo", nil, true, func(line string) {
		gotOutput = append(gotOutput, line)
	}, registry, errors.New)
	if err != nil {
		t.Fatalf("executeRemoteModule returned error: %v", err)
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
