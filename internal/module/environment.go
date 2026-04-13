//go:build !windows

package module

import (
	"context"
	"fmt"
	"os"
)

type EnvironmentModule struct{}

func (m *EnvironmentModule) Check(_ context.Context, params map[string]any) (bool, error) {
	var p EnvironmentParams
	if err := Decode(params, &p); err != nil {
		return false, err
	}

	current := os.Getenv(p.Name)

	return EnsureCheck("environment", p.Ensure,
		func() (bool, error) {
			if p.Value == "" {
				return false, fmt.Errorf("module: required param %q is missing", "value")
			}
			return current != p.Value, nil
		},
		func() (bool, error) {
			if current == "" {
				return false, nil
			}
			return true, nil
		},
	)
}

func (m *EnvironmentModule) Apply(_ context.Context, params map[string]any) error {
	var p EnvironmentParams
	if err := Decode(params, &p); err != nil {
		return err
	}

	return EnsureApply("environment", p.Ensure,
		func() error {
			if p.Value == "" {
				return fmt.Errorf("module: required param %q is missing", "value")
			}
			if err := os.Setenv(p.Name, p.Value); err != nil {
				return fmt.Errorf("environment: set %q: %w", p.Name, err)
			}
			return nil
		},
		func() error {
			if err := os.Unsetenv(p.Name); err != nil {
				return fmt.Errorf("environment: unset %q: %w", p.Name, err)
			}
			return nil
		},
	)
}
