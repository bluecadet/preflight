package winutil

import (
	"fmt"
	"strings"
)

// NormalizeWingetParams validates the canonical "packages" list form and
// returns a params map with a clean "packages" key.
func NormalizeWingetParams(params map[string]any) (map[string]any, error) {
	list, ok := params["packages"].([]any)
	if !ok {
		return nil, fmt.Errorf("winget_package: 'packages' is required and must be a list")
	}
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("winget_package: packages[%d] must be an object", i)
		}
		if id, _ := m["id"].(string); id == "" {
			return nil, fmt.Errorf("winget_package: packages[%d].id is required", i)
		}
	}
	return map[string]any{"packages": list}, nil
}

// NormalizeRemoveAppxParams validates the canonical "packages" list form and
// returns a params map with a clean "packages" key.
func NormalizeRemoveAppxParams(params map[string]any) (map[string]any, error) {
	list, ok := params["packages"].([]any)
	if !ok {
		return nil, fmt.Errorf("remove_appx_packages: 'packages' is required and must be a list")
	}
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("remove_appx_packages: packages[%d] must be an object", i)
		}
		if name, _ := m["name"].(string); name == "" {
			return nil, fmt.Errorf("remove_appx_packages: packages[%d].name is required", i)
		}
	}
	return map[string]any{"packages": list}, nil
}

// NormalizePackageParams validates the canonical "packages" list form and
// returns a params map with a clean "packages" key.
func NormalizePackageParams(params map[string]any) (map[string]any, error) {
	list, ok := params["packages"].([]any)
	if !ok {
		return nil, fmt.Errorf("package: 'packages' is required and must be a list")
	}
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("package: packages[%d] must be an object", i)
		}
		pid, _ := m["product_id"].(string)
		if pid == "" {
			return nil, fmt.Errorf("package: packages[%d].product_id is required", i)
		}
		ensure, _ := m["ensure"].(string)
		if ensure == "" {
			ensure = "present"
		}
		if ensure == "present" {
			if src, _ := m["source"].(string); src == "" {
				return nil, fmt.Errorf("package: packages[%d].source is required when ensure=present", i)
			}
		}
	}
	return map[string]any{"packages": list}, nil
}

// NormalizeFirewallPorts canonicalizes firewall port values into the string
// format expected by the Windows firewall PowerShell cmdlets.
func NormalizeFirewallPorts(value any) (string, error) {
	if value == nil {
		return "", nil
	}

	switch typed := value.(type) {
	case int:
		return fmt.Sprintf("%d", typed), nil
	case int64:
		return fmt.Sprintf("%d", typed), nil
	case float64:
		return fmt.Sprintf("%g", typed), nil
	case string:
		return typed, nil
	case []any:
		parts := make([]string, 0, len(typed))
		for i, item := range typed {
			if item == nil {
				return "", fmt.Errorf("ports[%d] must not be null", i)
			}
			text, err := NormalizeFirewallPorts(item)
			if err != nil {
				return "", fmt.Errorf("ports[%d]: %w", i, err)
			}
			parts = append(parts, text)
		}
		return strings.Join(parts, ","), nil
	default:
		return "", fmt.Errorf("ports must be a string, number, or list, got %T", value)
	}
}
