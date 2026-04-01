package module

import (
	"fmt"

	"github.com/bluecadet/preflight/internal/target"
)

// Registry returns a map of all built-in module names to their implementations.
func Registry() target.ModuleRegistry {
	reg := target.ModuleRegistry{
		"file":        &FileModule{},
		"directory":   &DirectoryModule{},
		"powershell":  &PowershellModule{},
		"shell":       &ShellModule{},
		"environment": &EnvironmentModule{},
		"wait":        &WaitModule{},
		"reboot":      &RebootModule{},
	}
	// Windows-only modules (stubs on non-Windows).
	addWindowsModules(reg)
	return reg
}

// paramString extracts a string parameter from params.
// Returns defaultVal if the key is absent or empty string.
// Returns an error if the key exists but is not a string.
func paramString(params map[string]any, key, defaultVal string) (string, error) {
	v, ok := params[key]
	if !ok || v == nil {
		return defaultVal, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("module: param %q must be a string, got %T", key, v)
	}
	return s, nil
}

// paramStringRequired extracts a required string parameter.
func paramStringRequired(params map[string]any, key string) (string, error) {
	v, ok := params[key]
	if !ok || v == nil {
		return "", fmt.Errorf("module: required param %q is missing", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("module: param %q must be a string, got %T", key, v)
	}
	if s == "" {
		return "", fmt.Errorf("module: required param %q must not be empty", key)
	}
	return s, nil
}

// paramInt extracts an integer parameter from params.
func paramInt(params map[string]any, key string, defaultVal int) (int, error) {
	v, ok := params[key]
	if !ok || v == nil {
		return defaultVal, nil
	}
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	}
	return 0, fmt.Errorf("module: param %q must be an integer, got %T", key, v)
}

// paramStringSlice extracts a []string parameter from params.
func paramStringSlice(params map[string]any, key string) ([]string, error) {
	v, ok := params[key]
	if !ok || v == nil {
		return nil, nil
	}
	switch t := v.(type) {
	case []string:
		return t, nil
	case []any:
		out := make([]string, 0, len(t))
		for i, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("module: param %q[%d] must be a string, got %T", key, i, item)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("module: param %q must be a list of strings, got %T", key, v)
}
