package winutil

import (
	"fmt"
	"maps"
	"strings"
)

// NormalizeManagerPackages validates the "packages" list for winget_package
// and choco_package and returns a params map with a clean "packages" key.
// Package IDs are case-insensitive and may appear once.
func NormalizeManagerPackages(module string, params map[string]any) (map[string]any, error) {
	entries, err := managerPackageEntries(module, params)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]int, len(entries))
	for i, m := range entries {
		id := m["id"].(string)
		key := strings.ToLower(id)
		if first, dup := seen[key]; dup {
			return nil, fmt.Errorf("%s: packages[%d].id %q duplicates packages[%d]", module, i, id, first)
		}
		seen[key] = i
	}
	return map[string]any{"packages": params["packages"]}, nil
}

// NormalizeScoopPackages validates scoop_package entries. Scoop keeps several
// versions of an app side by side, so an app may repeat with distinct
// versions when exactly one entry sets active: true. A versioned entry for an
// app listed once is active unless it says otherwise. Active entries move to
// the end so they run after the installs that would change the current
// version. The app is the id without its bucket prefix.
func NormalizeScoopPackages(params map[string]any) (map[string]any, error) {
	const module = "scoop_package"
	entries, err := managerPackageEntries(module, params)
	if err != nil {
		return nil, err
	}
	byApp := make(map[string][]int, len(entries))
	var order []string
	for i, m := range entries {
		id := m["id"].(string)
		if v, set := m["version"]; set && v != nil {
			if _, ok := v.(string); !ok {
				return nil, fmt.Errorf("%s: packages[%d].version must be a string, got %T", module, i, v)
			}
		}
		if a, set := m["active"]; set && a != nil {
			if _, ok := a.(bool); !ok {
				return nil, fmt.Errorf("%s: packages[%d].active must be a boolean, got %T", module, i, a)
			}
		}
		version, _ := m["version"].(string)
		if ensure, _ := m["ensure"].(string); ensure == "absent" && version != "" {
			return nil, fmt.Errorf("%s: packages[%d] sets version with ensure: absent; Scoop can only remove every version of %q", module, i, id)
		}
		app := strings.ToLower(id[strings.LastIndex(id, "/")+1:])
		if _, ok := byApp[app]; !ok {
			order = append(order, app)
		}
		byApp[app] = append(byApp[app], i)
	}
	out := make([]map[string]any, 0, len(entries))
	var active []map[string]any
	for _, app := range order {
		idx := byApp[app]
		if len(idx) == 1 {
			m := maps.Clone(entries[idx[0]])
			if _, set := m["active"]; !set && m["version"] != nil {
				m["active"] = true
			}
			if m["active"] == true {
				active = append(active, m)
			} else {
				out = append(out, m)
			}
			continue
		}
		versions := make(map[string]int, len(idx))
		actives := 0
		for _, i := range idx {
			m := entries[i]
			version, _ := m["version"].(string)
			if version == "" {
				return nil, fmt.Errorf("%s: packages[%d].id %q repeats packages[%d]; repeated apps need a version on every entry", module, i, m["id"], idx[0])
			}
			if first, dup := versions[strings.ToLower(version)]; dup {
				return nil, fmt.Errorf("%s: packages[%d] duplicates packages[%d] (%s %s)", module, i, first, app, version)
			}
			versions[strings.ToLower(version)] = i
			if m["active"] == true {
				actives++
				active = append(active, maps.Clone(m))
			} else {
				out = append(out, maps.Clone(m))
			}
		}
		if actives != 1 {
			return nil, fmt.Errorf("%s: %q is listed %d times; exactly one entry needs active: true", module, app, len(idx))
		}
	}
	list := make([]any, 0, len(entries))
	for _, m := range append(out, active...) {
		list = append(list, m)
	}
	return map[string]any{"packages": list}, nil
}

// managerPackageEntries checks the list shape and ids shared by every
// package-manager module.
func managerPackageEntries(module string, params map[string]any) ([]map[string]any, error) {
	list, ok := params["packages"].([]any)
	if !ok {
		return nil, fmt.Errorf("%s: 'packages' is required and must be a list", module)
	}
	entries := make([]map[string]any, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: packages[%d] must be an object", module, i)
		}
		if id, _ := m["id"].(string); id == "" {
			return nil, fmt.Errorf("%s: packages[%d].id is required", module, i)
		}
		entries[i] = m
	}
	return entries, nil
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
// list expected by the Windows firewall PowerShell cmdlets.
func NormalizeFirewallPorts(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	if list, ok := value.([]any); ok {
		ports := make([]string, 0, len(list))
		for i, item := range list {
			if item == nil {
				return nil, fmt.Errorf("ports[%d] must not be null", i)
			}
			port, err := firewallPort(item)
			if err != nil {
				return nil, fmt.Errorf("ports[%d]: %w", i, err)
			}
			ports = append(ports, port)
		}
		return ports, nil
	}
	port, err := firewallPort(value)
	if err != nil {
		return nil, err
	}
	return []string{port}, nil
}

func firewallPort(value any) (string, error) {
	switch typed := value.(type) {
	case int:
		return fmt.Sprintf("%d", typed), nil
	case int64:
		return fmt.Sprintf("%d", typed), nil
	case float64:
		return fmt.Sprintf("%g", typed), nil
	case string:
		return typed, nil
	default:
		return "", fmt.Errorf("ports must be a string, number, or list, got %T", value)
	}
}
