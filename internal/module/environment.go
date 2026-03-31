package module

import (
	"context"
	"fmt"
	"os"
)

// EnvironmentModule manages environment variables.
// Params:
//   - name (required): variable name
//   - value (required for ensure=present): variable value
//   - scope: "user" (default) or "machine"
//   - ensure: "present" (default) or "absent"
//
// Note: os.Setenv affects the current process. Machine-scope persistence
// on Windows requires registry writes (handled in environment_windows.go).
type EnvironmentModule struct{}

func (m *EnvironmentModule) Check(_ context.Context, params map[string]any) (bool, error) {
	name, err := paramStringRequired(params, "name")
	if err != nil {
		return false, err
	}
	ensure, err := paramString(params, "ensure", "present")
	if err != nil {
		return false, err
	}

	current := os.Getenv(name)

	switch ensure {
	case "absent":
		if current == "" {
			return false, nil // already absent
		}
		return true, nil // needs removal

	case "present":
		value, err := paramStringRequired(params, "value")
		if err != nil {
			return false, err
		}
		return current != value, nil

	default:
		return false, fmt.Errorf("environment: unknown ensure value %q (want present|absent)", ensure)
	}
}

func (m *EnvironmentModule) Apply(_ context.Context, params map[string]any) error {
	name, err := paramStringRequired(params, "name")
	if err != nil {
		return err
	}
	ensure, err := paramString(params, "ensure", "present")
	if err != nil {
		return err
	}

	switch ensure {
	case "absent":
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("environment: unset %q: %w", name, err)
		}
		return nil

	case "present":
		value, err := paramStringRequired(params, "value")
		if err != nil {
			return err
		}
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("environment: set %q: %w", name, err)
		}
		return nil

	default:
		return fmt.Errorf("environment: unknown ensure value %q (want present|absent)", ensure)
	}
}
