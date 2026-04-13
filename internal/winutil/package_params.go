package winutil

import (
	"fmt"
	"maps"
	"strings"
)

// NormalizeWingetParams accepts either a "packages" list or the legacy
// single-package form ("id" at the top level) and returns a params map with a
// canonical "packages" key.
func NormalizeWingetParams(params map[string]any) (map[string]any, error) {
	_, hasPkgs := params["packages"]
	_, hasID := params["id"]
	if hasPkgs && hasID {
		return nil, fmt.Errorf("winget_package: specify either 'packages' or 'id', not both")
	}
	if hasPkgs {
		list, ok := params["packages"].([]any)
		if !ok {
			return nil, fmt.Errorf("winget_package: 'packages' must be a list")
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
	if hasID {
		id, ok := params["id"].(string)
		if !ok || id == "" {
			return nil, fmt.Errorf("winget_package: 'id' must be a non-empty string")
		}
		spec := map[string]any{"id": id}
		if v, _ := params["version"].(string); v != "" {
			spec["version"] = v
		}
		if v, _ := params["source"].(string); v != "" {
			spec["source"] = v
		}
		if v, _ := params["scope"].(string); v != "" {
			spec["scope"] = v
		}
		if v, _ := params["ensure"].(string); v != "" {
			spec["ensure"] = v
		}
		return map[string]any{"packages": []any{spec}}, nil
	}
	return nil, fmt.Errorf("winget_package: 'packages' or 'id' is required")
}

// NormalizeRemoveAppxParams accepts either a "packages" list or the legacy
// single-package form ("name" at the top level) and returns a params map with a
// canonical "packages" key.
func NormalizeRemoveAppxParams(params map[string]any) (map[string]any, error) {
	_, hasPkgs := params["packages"]
	_, hasName := params["name"]
	if hasPkgs && hasName {
		return nil, fmt.Errorf("remove_appx_packages: specify either 'packages' or 'name', not both")
	}
	if hasPkgs {
		list, ok := params["packages"].([]any)
		if !ok {
			return nil, fmt.Errorf("remove_appx_packages: 'packages' must be a list")
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
	if hasName {
		name, ok := params["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("remove_appx_packages: 'name' must be a non-empty string")
		}
		spec := map[string]any{"name": name}
		if v, _ := params["scope"].(string); v != "" {
			spec["scope"] = v
		}
		return map[string]any{"packages": []any{spec}}, nil
	}
	return nil, fmt.Errorf("remove_appx_packages: 'packages' or 'name' is required")
}

// NormalizePackageParams accepts either a "packages" list or the legacy
// single-package form ("product_id" at the top level) and returns a params map
// with a canonical "packages" key.
func NormalizePackageParams(params map[string]any) (map[string]any, error) {
	_, hasPkgs := params["packages"]
	_, hasPID := params["product_id"]
	if hasPkgs && hasPID {
		return nil, fmt.Errorf("package: specify either 'packages' or 'product_id', not both")
	}
	if hasPkgs {
		list, ok := params["packages"].([]any)
		if !ok {
			return nil, fmt.Errorf("package: 'packages' must be a list")
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
	if hasPID {
		pid, ok := params["product_id"].(string)
		if !ok || pid == "" {
			return nil, fmt.Errorf("package: 'product_id' must be a non-empty string")
		}
		ensure, _ := params["ensure"].(string)
		if ensure == "" {
			ensure = "present"
		}
		if ensure == "present" {
			if src, _ := params["source"].(string); src == "" {
				return nil, fmt.Errorf("package: 'source' is required when ensure=present")
			}
		}
		spec := map[string]any{"product_id": pid, "ensure": ensure}
		if v, _ := params["source"].(string); v != "" {
			spec["source"] = v
		}
		if v, ok := params["args"]; ok && v != nil {
			spec["args"] = v
		}
		return map[string]any{"packages": []any{spec}}, nil
	}
	return nil, fmt.Errorf("package: 'packages' or 'product_id' is required")
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

func cloneParams(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	cloned := make(map[string]any, len(params))
	maps.Copy(cloned, params)
	return cloned
}
