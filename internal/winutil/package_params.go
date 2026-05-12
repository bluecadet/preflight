package winutil

import (
	"fmt"
	"strings"
)

type packageNormConfig struct {
	moduleName   string
	singleKey    string
	validateItem func(i int, item map[string]any) error
	buildSingle  func(params map[string]any) (map[string]any, error)
}

func normalizePackageList(params map[string]any, cfg packageNormConfig) (map[string]any, error) {
	_, hasPackages := params["packages"]
	_, hasSingle := params[cfg.singleKey]

	if hasPackages && hasSingle {
		return nil, fmt.Errorf("%s: specify either 'packages' or '%s', not both", cfg.moduleName, cfg.singleKey)
	}

	if hasPackages {
		list, ok := params["packages"].([]any)
		if !ok {
			return nil, fmt.Errorf("%s: 'packages' must be a list", cfg.moduleName)
		}
		for i, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s: packages[%d] must be an object", cfg.moduleName, i)
			}
			if err := cfg.validateItem(i, m); err != nil {
				return nil, err
			}
		}
		return map[string]any{"packages": list}, nil
	}

	if hasSingle {
		spec, err := cfg.buildSingle(params)
		if err != nil {
			return nil, err
		}
		return map[string]any{"packages": []any{spec}}, nil
	}

	return nil, fmt.Errorf("%s: 'packages' or '%s' is required", cfg.moduleName, cfg.singleKey)
}

func copyOptionalString(dst, src map[string]any, key string) {
	if value, _ := src[key].(string); value != "" {
		dst[key] = value
	}
}

// NormalizeWingetParams accepts either a "packages" list or the legacy
// single-package form ("id" at the top level) and returns a params map with a
// canonical "packages" key.
func NormalizeWingetParams(params map[string]any) (map[string]any, error) {
	return normalizePackageList(params, packageNormConfig{
		moduleName: "winget_package",
		singleKey:  "id",
		validateItem: func(i int, item map[string]any) error {
			if id, _ := item["id"].(string); id == "" {
				return fmt.Errorf("winget_package: packages[%d].id is required", i)
			}
			return nil
		},
		buildSingle: func(params map[string]any) (map[string]any, error) {
			id, ok := params["id"].(string)
			if !ok || id == "" {
				return nil, fmt.Errorf("winget_package: 'id' must be a non-empty string")
			}

			spec := map[string]any{"id": id}
			for _, key := range []string{"version", "source", "scope", "ensure"} {
				copyOptionalString(spec, params, key)
			}
			if args, ok := params["args"]; ok && args != nil {
				spec["args"] = args
			}
			return spec, nil
		},
	})
}

// NormalizeRemoveAppxParams accepts either a "packages" list or the legacy
// single-package form ("name" at the top level) and returns a params map with a
// canonical "packages" key.
func NormalizeRemoveAppxParams(params map[string]any) (map[string]any, error) {
	return normalizePackageList(params, packageNormConfig{
		moduleName: "remove_appx_packages",
		singleKey:  "name",
		validateItem: func(i int, item map[string]any) error {
			if name, _ := item["name"].(string); name == "" {
				return fmt.Errorf("remove_appx_packages: packages[%d].name is required", i)
			}
			return nil
		},
		buildSingle: func(params map[string]any) (map[string]any, error) {
			name, ok := params["name"].(string)
			if !ok || name == "" {
				return nil, fmt.Errorf("remove_appx_packages: 'name' must be a non-empty string")
			}

			spec := map[string]any{"name": name}
			copyOptionalString(spec, params, "scope")
			return spec, nil
		},
	})
}

// NormalizePackageParams accepts either a "packages" list or the legacy
// single-package form ("product_id" at the top level) and returns a params map
// with a canonical "packages" key.
func NormalizePackageParams(params map[string]any) (map[string]any, error) {
	return normalizePackageList(params, packageNormConfig{
		moduleName: "package",
		singleKey:  "product_id",
		validateItem: func(i int, item map[string]any) error {
			pid, _ := item["product_id"].(string)
			if pid == "" {
				return fmt.Errorf("package: packages[%d].product_id is required", i)
			}
			ensure, _ := item["ensure"].(string)
			if ensure == "" {
				ensure = "present"
			}
			if ensure == "present" {
				if src, _ := item["source"].(string); src == "" {
					return fmt.Errorf("package: packages[%d].source is required when ensure=present", i)
				}
			}
			return nil
		},
		buildSingle: func(params map[string]any) (map[string]any, error) {
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
			copyOptionalString(spec, params, "source")
			if args, ok := params["args"]; ok && args != nil {
				spec["args"] = args
			}
			return spec, nil
		},
	})
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
