//go:build !windows

package module

import (
	"context"
	"fmt"
	"os"

	"github.com/bluecadet/preflight/internal/target"
)

type EnvironmentModule struct{}

func (m *EnvironmentModule) Check(_ context.Context, params map[string]any, _ target.OutputFunc) (target.CheckResult, error) {
	var p EnvironmentParams
	if err := Decode(params, &p); err != nil {
		return target.CheckResult{}, err
	}

	current := os.Getenv(p.Name)

	needed, err := EnsureCheck("environment", p.Ensure,
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
	return target.CheckResult{NeedsChange: needed}, err
}

func (m *EnvironmentModule) Apply(_ context.Context, params map[string]any, _ target.OutputFunc) (target.ApplyResult, error) {
	var p EnvironmentParams
	if err := Decode(params, &p); err != nil {
		return target.ApplyResult{}, err
	}

	return target.ApplyResult{}, EnsureApply("environment", p.Ensure,
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
