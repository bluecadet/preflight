package winutil

import (
	"fmt"
	"maps"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// NormalizeRegistryParams canonicalizes registry value specs into a list form
// that is easy for PowerShell scripts to consume.
func NormalizeRegistryParams(params map[string]any) (map[string]any, error) {
	cloned := cloneParams(params)
	values, err := normalizeRegistryValues(cloned["values"])
	if err != nil {
		return nil, err
	}
	if values != nil {
		cloned["values"] = values
	}
	return cloned, nil
}

func normalizeRegistryValues(raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	switch typed := raw.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		values := make([]map[string]any, 0, len(keys))
		for _, key := range keys {
			value, valueType, err := inferRegistryValue(typed[key])
			if err != nil {
				return nil, fmt.Errorf("registry value %q: %w", key, err)
			}
			values = append(values, map[string]any{
				"name":   key,
				"type":   valueType,
				"data":   value,
				"ensure": "present",
			})
		}
		return values, nil
	case []any:
		values := make([]map[string]any, 0, len(typed))
		for i, item := range typed {
			spec, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("registry values[%d] must be an object, got %T", i, item)
			}

			name, ok := spec["name"].(string)
			if !ok || strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("registry values[%d].name is required", i)
			}

			ensure := "present"
			if rawEnsure, ok := spec["ensure"]; ok && rawEnsure != nil {
				text, ok := rawEnsure.(string)
				if !ok {
					return nil, fmt.Errorf("registry values[%d].ensure must be a string, got %T", i, rawEnsure)
				}
				ensure = strings.ToLower(strings.TrimSpace(text))
			}
			if ensure != "present" && ensure != "absent" {
				return nil, fmt.Errorf("registry values[%d].ensure must be present or absent", i)
			}

			entry := map[string]any{
				"name":   name,
				"ensure": ensure,
			}
			if ensure == "absent" {
				values = append(values, entry)
				continue
			}

			valueType := ""
			if rawType, ok := spec["type"]; ok && rawType != nil {
				text, ok := rawType.(string)
				if !ok {
					return nil, fmt.Errorf("registry values[%d].type must be a string, got %T", i, rawType)
				}
				valueType = normalizeRegistryType(text)
			}

			value, ok := spec["data"]
			if !ok {
				return nil, fmt.Errorf("registry values[%d].data is required when ensure=present", i)
			}
			normalizedValue, normalizedType, err := normalizeRegistryValue(value, valueType)
			if err != nil {
				return nil, fmt.Errorf("registry values[%d]: %w", i, err)
			}
			entry["type"] = normalizedType
			entry["data"] = normalizedValue
			values = append(values, entry)
		}
		return values, nil
	default:
		return nil, fmt.Errorf("registry values must be an object or list, got %T", raw)
	}
}

func inferRegistryValue(value any) (any, string, error) {
	return normalizeRegistryValue(value, "")
}

func normalizeRegistryValue(value any, valueType string) (any, string, error) {
	if valueType == "" {
		inferred, err := inferRegistryType(value)
		if err != nil {
			return nil, "", err
		}
		valueType = inferred
	}

	switch valueType {
	case "string", "expand_string":
		text, ok := value.(string)
		if !ok {
			return nil, "", fmt.Errorf("expected string data for %s, got %T", valueType, value)
		}
		return text, valueType, nil
	case "dword":
		number, err := normalizeIntegralValue(value, 32)
		if err != nil {
			return nil, "", err
		}
		return number, valueType, nil
	case "qword":
		number, err := normalizeIntegralValue(value, 64)
		if err != nil {
			return nil, "", err
		}
		return number, valueType, nil
	case "multi_string":
		switch typed := value.(type) {
		case []string:
			items := make([]any, len(typed))
			for i, item := range typed {
				items[i] = item
			}
			return items, valueType, nil
		case []any:
			items := make([]any, 0, len(typed))
			for i, item := range typed {
				text, ok := item.(string)
				if !ok {
					return nil, "", fmt.Errorf("multi_string data[%d] must be a string, got %T", i, item)
				}
				items = append(items, text)
			}
			return items, valueType, nil
		default:
			return nil, "", fmt.Errorf("expected string list for multi_string, got %T", value)
		}
	case "binary":
		switch typed := value.(type) {
		case []byte:
			items := make([]any, len(typed))
			for i, item := range typed {
				items[i] = int(item)
			}
			return items, valueType, nil
		case []any:
			items := make([]any, 0, len(typed))
			for i, item := range typed {
				number, err := normalizeIntegralValue(item, 8)
				if err != nil {
					return nil, "", fmt.Errorf("binary data[%d]: %w", i, err)
				}
				items = append(items, number)
			}
			return items, valueType, nil
		default:
			return nil, "", fmt.Errorf("expected byte list for binary, got %T", value)
		}
	default:
		return nil, "", fmt.Errorf("unsupported registry value type %q", valueType)
	}
}

func inferRegistryType(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return "string", nil
	case bool, int, int64:
		return "dword", nil
	case float64:
		if math.Trunc(typed) != typed {
			return "", fmt.Errorf("cannot infer integer registry type from non-integral number %v", typed)
		}
		if typed >= math.MinInt32 && typed <= math.MaxInt32 {
			return "dword", nil
		}
		return "qword", nil
	case []string:
		return "multi_string", nil
	case []any:
		if len(typed) == 0 {
			return "multi_string", nil
		}
		allStrings := true
		for _, item := range typed {
			if _, ok := item.(string); !ok {
				allStrings = false
				break
			}
		}
		if allStrings {
			return "multi_string", nil
		}
		return "binary", nil
	default:
		return "", fmt.Errorf("cannot infer registry value type from %T", value)
	}
}

func normalizeRegistryType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "string", "sz", "reg_sz":
		return "string"
	case "expandstring", "expand_string", "expand-string", "reg_expand_sz":
		return "expand_string"
	case "dword", "reg_dword":
		return "dword"
	case "qword", "reg_qword":
		return "qword"
	case "binary", "reg_binary":
		return "binary"
	case "multistring", "multi_string", "multi-string", "reg_multi_sz":
		return "multi_string"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

// NormalizeScheduledTaskParams applies aliases and defaults so both the local
// Windows module and the WinRM implementation use the same semantics.
func NormalizeScheduledTaskParams(params map[string]any) (map[string]any, error) {
	cloned := cloneParams(params)

	if execute, ok := cloned["command"]; ok {
		if _, exists := cloned["execute"]; !exists {
			cloned["execute"] = execute
		}
	}
	if runAs, ok := cloned["user"]; ok {
		if _, exists := cloned["run_as"]; !exists {
			cloned["run_as"] = runAs
		}
	}

	path, err := normalizeTaskPath(cloned["path"])
	if err != nil {
		return nil, err
	}
	cloned["path"] = path

	trigger := "startup"
	if rawTrigger, ok := cloned["trigger"]; ok && rawTrigger != nil {
		text, ok := rawTrigger.(string)
		if !ok {
			return nil, fmt.Errorf("scheduled_task trigger must be a string, got %T", rawTrigger)
		}
		trigger = strings.ToLower(strings.TrimSpace(text))
	}
	cloned["trigger"] = trigger

	runLevel := "least"
	if rawRunLevel, ok := cloned["run_level"]; ok && rawRunLevel != nil {
		text, ok := rawRunLevel.(string)
		if !ok {
			return nil, fmt.Errorf("scheduled_task run_level must be a string, got %T", rawRunLevel)
		}
		runLevel = strings.ToLower(strings.TrimSpace(text))
	}
	if runLevel != "least" && runLevel != "highest" {
		return nil, fmt.Errorf("scheduled_task run_level must be least or highest")
	}
	cloned["run_level"] = runLevel

	enabled := true
	if rawEnabled, ok := cloned["enabled"]; ok && rawEnabled != nil {
		value, err := parseBool(rawEnabled)
		if err != nil {
			return nil, fmt.Errorf("scheduled_task enabled: %w", err)
		}
		enabled = value
	}
	cloned["enabled"] = enabled

	if rawDelay, ok := cloned["delay"]; ok && rawDelay != nil {
		delay, err := normalizeDelay(rawDelay)
		if err != nil {
			return nil, fmt.Errorf("scheduled_task delay: %w", err)
		}
		cloned["delay"] = delay
	}

	if startAt, ok := cloned["start_at"]; ok && startAt != nil {
		text, ok := startAt.(string)
		if !ok {
			return nil, fmt.Errorf("scheduled_task start_at must be a string, got %T", startAt)
		}
		cloned["start_at"] = strings.TrimSpace(text)
	} else if trigger == "daily" {
		cloned["start_at"] = "03:00"
	}

	return cloned, nil
}

// ValidateScheduledTaskParams checks the normalized scheduled task parameter
// contract shared by the local Windows module and the WinRM implementation.
func ValidateScheduledTaskParams(params map[string]any) error {
	ensure := "present"
	if rawEnsure, ok := params["ensure"]; ok && rawEnsure != nil {
		text, ok := rawEnsure.(string)
		if !ok {
			return fmt.Errorf("scheduled_task ensure must be a string, got %T", rawEnsure)
		}
		ensure = strings.ToLower(strings.TrimSpace(text))
	}
	if ensure != "present" && ensure != "absent" {
		return fmt.Errorf("scheduled_task ensure must be present or absent")
	}
	if ensure == "absent" {
		return nil
	}

	execute, ok := params["execute"].(string)
	if !ok || strings.TrimSpace(execute) == "" {
		return fmt.Errorf("scheduled_task execute is required")
	}

	trigger, _ := params["trigger"].(string)
	switch trigger {
	case "startup", "onlogon", "daily", "once":
	default:
		return fmt.Errorf("scheduled_task trigger %q is not supported", trigger)
	}

	if _, ok := params["delay"]; ok && params["delay"] != nil {
		if trigger != "startup" && trigger != "onlogon" {
			return fmt.Errorf("scheduled_task delay is only supported for startup and onlogon triggers")
		}
	}

	if trigger == "once" {
		startAt, _ := params["start_at"].(string)
		if strings.TrimSpace(startAt) == "" {
			return fmt.Errorf("scheduled_task start_at is required for once trigger")
		}
	}

	return nil
}

func normalizeTaskPath(raw any) (string, error) {
	if raw == nil {
		return "\\", nil
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("scheduled_task path must be a string, got %T", raw)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "\\", nil
	}
	if !strings.HasPrefix(text, "\\") {
		text = "\\" + text
	}
	if !strings.HasSuffix(text, "\\") {
		text += "\\"
	}
	return text, nil
}

func normalizeDelay(raw any) (string, error) {
	switch typed := raw.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return "", nil
		}
		if strings.HasPrefix(strings.ToUpper(text), "P") {
			return strings.ToUpper(text), nil
		}
		duration, err := time.ParseDuration(text)
		if err != nil {
			return "", err
		}
		return durationToISODuration(duration)
	case int:
		return durationToISODuration(time.Duration(typed) * time.Second)
	case int64:
		return durationToISODuration(time.Duration(typed) * time.Second)
	case float64:
		if math.Trunc(typed) != typed {
			return "", fmt.Errorf("delay seconds must be an integer, got %v", typed)
		}
		return durationToISODuration(time.Duration(int64(typed)) * time.Second)
	default:
		return "", fmt.Errorf("unsupported delay type %T", raw)
	}
}

func durationToISODuration(duration time.Duration) (string, error) {
	if duration <= 0 {
		return "", fmt.Errorf("duration must be greater than zero")
	}
	totalSeconds := int64(duration / time.Second)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	var b strings.Builder
	b.WriteString("PT")
	if hours > 0 {
		b.WriteString(strconv.FormatInt(hours, 10))
		b.WriteByte('H')
	}
	if minutes > 0 {
		b.WriteString(strconv.FormatInt(minutes, 10))
		b.WriteByte('M')
	}
	if seconds > 0 || (hours == 0 && minutes == 0) {
		b.WriteString(strconv.FormatInt(seconds, 10))
		b.WriteByte('S')
	}
	return b.String(), nil
}

func normalizeIntegralValue(value any, bits int) (int64, error) {
	switch typed := value.(type) {
	case bool:
		if typed {
			return 1, nil
		}
		return 0, nil
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		if math.Trunc(typed) != typed {
			return 0, fmt.Errorf("expected integer, got %v", typed)
		}
		return int64(typed), nil
	case string:
		s := strings.TrimSpace(typed)
		switch strings.ToLower(s) {
		case "true":
			return 1, nil
		case "false":
			return 0, nil
		}
		parsed, err := strconv.ParseInt(s, 10, bits)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("expected integer-like value, got %T", value)
	}
}

func parseBool(value any) (bool, error) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes":
			return true, nil
		case "false", "0", "no":
			return false, nil
		}
	}
	return false, fmt.Errorf("expected bool, got %T", value)
}

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
