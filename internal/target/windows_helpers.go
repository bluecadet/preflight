package target

import (
	"fmt"
	"strings"

	"github.com/bluecadet/preflight/internal/winutil"
)

func powershellJSONVar(name string, value any) (string, error) {
	return winutil.JSONVarScript(name, value)
}

func parseWindowsBool(out string) (bool, error) {
	value, _, err := parseWindowsBoolOutput(out)
	return value, err
}

func parseWindowsBoolOutput(out string) (bool, []string, error) {
	lines := splitOutputLines(out)
	if len(lines) == 0 {
		return false, nil, fmt.Errorf("unexpected boolean output %q", strings.TrimSpace(out))
	}
	switch strings.ToLower(strings.TrimSpace(lines[len(lines)-1])) {
	case "true":
		return true, lines[:len(lines)-1], nil
	case "false":
		return false, lines[:len(lines)-1], nil
	default:
		return false, nil, fmt.Errorf("unexpected boolean output %q", strings.TrimSpace(out))
	}
}

func normalizeWindowsArch(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "x64", "amd64", "64-bit":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	case "x86", "386", "32-bit":
		return "386"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func paramStringSlice(params map[string]any, key string) ([]string, error) {
	value, ok := params[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []string:
		return typed, nil
	case []any:
		result := make([]string, 0, len(typed))
		for i, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d] must be a string, got %T", key, i, item)
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("%s must be a string list, got %T", key, value)
	}
}

func paramString(params map[string]any, key, defaultVal string) (string, error) {
	value, ok := params[key]
	if !ok || value == nil {
		return defaultVal, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", key, value)
	}
	if text == "" {
		return defaultVal, nil
	}
	return text, nil
}

func paramStringRequired(params map[string]any, key string) (string, error) {
	value, ok := params[key]
	if !ok || value == nil {
		return "", fmt.Errorf("required param %q is missing", key)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", key, value)
	}
	if text == "" {
		return "", fmt.Errorf("required param %q must not be empty", key)
	}
	return text, nil
}
