package winutil

import (
	"fmt"
	"strings"
)

// ParseBool accepts common bool-like values used in Windows module params.
func ParseBool(v any) (bool, error) {
	switch typed := v.(type) {
	case bool:
		return typed, nil
	case string:
		return parseBoolString(typed)
	case []byte:
		return parseBoolString(string(typed))
	default:
		return false, fmt.Errorf("expected bool, got %T", v)
	}
}

func parseBoolString(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("expected bool, got %T", v)
	}
}
