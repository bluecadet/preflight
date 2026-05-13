//go:build !windows

package module_test

import (
	"context"
	"os"
	"testing"

	"github.com/bluecadet/preflight/internal/module"
)

func TestEnvironmentModule_Check_NotSet(t *testing.T) {
	reg := module.Registry()
	m := reg["environment"]

	const varName = "PREFLIGHT_TEST_ENV_UNSET_VAR"
	if err := os.Unsetenv(varName); err != nil {
		t.Fatalf("Unsetenv(%q): %v", varName, err)
	}

	res, err := m.Check(context.Background(), map[string]any{
		"name":  varName,
		"value": "hello",
	}, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !res.NeedsChange {
		t.Error("expected NeedsChange=true for unset variable")
	}
}

func TestEnvironmentModule_Check_AlreadySet(t *testing.T) {
	reg := module.Registry()
	m := reg["environment"]

	const varName = "PREFLIGHT_TEST_ENV_ALREADY_SET"
	t.Setenv(varName, "desired")

	res, err := m.Check(context.Background(), map[string]any{
		"name":  varName,
		"value": "desired",
	}, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.NeedsChange {
		t.Error("expected NeedsChange=false when variable already at desired value")
	}
}

func TestEnvironmentModule_ApplyThenCheck(t *testing.T) {
	reg := module.Registry()
	m := reg["environment"]

	const varName = "PREFLIGHT_TEST_ENV_APPLY"
	if err := os.Unsetenv(varName); err != nil {
		t.Fatalf("Unsetenv(%q): %v", varName, err)
	}
	t.Cleanup(func() {
		if err := os.Unsetenv(varName); err != nil {
			t.Fatalf("Unsetenv(%q): %v", varName, err)
		}
	})

	if _, err := m.Apply(context.Background(), map[string]any{
		"name":  varName,
		"value": "applied",
	}, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	res, err := m.Check(context.Background(), map[string]any{
		"name":  varName,
		"value": "applied",
	}, nil)
	if err != nil {
		t.Fatalf("Check after Apply: %v", err)
	}
	if res.NeedsChange {
		t.Error("expected NeedsChange=false after Apply set the variable")
	}
}

func TestEnvironmentModule_EnsureAbsent(t *testing.T) {
	reg := module.Registry()
	m := reg["environment"]

	const varName = "PREFLIGHT_TEST_ENV_ABSENT"
	t.Setenv(varName, "present")

	res, err := m.Check(context.Background(), map[string]any{
		"name":   varName,
		"ensure": "absent",
	}, nil)
	if err != nil {
		t.Fatalf("Check absent: %v", err)
	}
	if !res.NeedsChange {
		t.Error("expected NeedsChange=true when var is present and ensure=absent")
	}

	if _, err := m.Apply(context.Background(), map[string]any{
		"name":   varName,
		"ensure": "absent",
	}, nil); err != nil {
		t.Fatalf("Apply absent: %v", err)
	}

	res, err = m.Check(context.Background(), map[string]any{
		"name":   varName,
		"ensure": "absent",
	}, nil)
	if err != nil {
		t.Fatalf("Check after absent Apply: %v", err)
	}
	if res.NeedsChange {
		t.Error("expected NeedsChange=false after Apply removed the variable")
	}
}
