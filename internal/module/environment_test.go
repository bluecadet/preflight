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

	needsChange, err := m.Check(context.Background(), map[string]any{
		"name":  varName,
		"value": "hello",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !needsChange {
		t.Error("expected needsChange=true for unset variable")
	}
}

func TestEnvironmentModule_Check_AlreadySet(t *testing.T) {
	reg := module.Registry()
	m := reg["environment"]

	const varName = "PREFLIGHT_TEST_ENV_ALREADY_SET"
	t.Setenv(varName, "desired")

	needsChange, err := m.Check(context.Background(), map[string]any{
		"name":  varName,
		"value": "desired",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if needsChange {
		t.Error("expected needsChange=false when variable already at desired value")
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

	if err := m.Apply(context.Background(), map[string]any{
		"name":  varName,
		"value": "applied",
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	needsChange, err := m.Check(context.Background(), map[string]any{
		"name":  varName,
		"value": "applied",
	})
	if err != nil {
		t.Fatalf("Check after Apply: %v", err)
	}
	if needsChange {
		t.Error("expected needsChange=false after Apply set the variable")
	}
}

func TestEnvironmentModule_EnsureAbsent(t *testing.T) {
	reg := module.Registry()
	m := reg["environment"]

	const varName = "PREFLIGHT_TEST_ENV_ABSENT"
	t.Setenv(varName, "present")

	needsChange, err := m.Check(context.Background(), map[string]any{
		"name":   varName,
		"ensure": "absent",
	})
	if err != nil {
		t.Fatalf("Check absent: %v", err)
	}
	if !needsChange {
		t.Error("expected needsChange=true when var is present and ensure=absent")
	}

	if err := m.Apply(context.Background(), map[string]any{
		"name":   varName,
		"ensure": "absent",
	}); err != nil {
		t.Fatalf("Apply absent: %v", err)
	}

	needsChange, err = m.Check(context.Background(), map[string]any{
		"name":   varName,
		"ensure": "absent",
	})
	if err != nil {
		t.Fatalf("Check after absent Apply: %v", err)
	}
	if needsChange {
		t.Error("expected needsChange=false after Apply removed the variable")
	}
}
