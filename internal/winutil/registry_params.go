package winutil

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// registryHiveAliases maps every accepted hive prefix (short PSDrive names,
// with or without the trailing colon, and the long HKEY_* names) to the
// provider-qualified hive that PowerShell's *-Item cmdlets accept uniformly.
var registryHiveAliases = map[string]string{
	"HKLM":                "HKEY_LOCAL_MACHINE",
	"HKEY_LOCAL_MACHINE":  "HKEY_LOCAL_MACHINE",
	"HKCU":                "HKEY_CURRENT_USER",
	"HKEY_CURRENT_USER":   "HKEY_CURRENT_USER",
	"HKCR":                "HKEY_CLASSES_ROOT",
	"HKEY_CLASSES_ROOT":   "HKEY_CLASSES_ROOT",
	"HKU":                 "HKEY_USERS",
	"HKEY_USERS":          "HKEY_USERS",
	"HKCC":                "HKEY_CURRENT_CONFIG",
	"HKEY_CURRENT_CONFIG": "HKEY_CURRENT_CONFIG",
}

// normalizeRegistryProviderPath rewrites a registry path with any recognised
// hive prefix into the provider-qualified form ("Registry::HKEY_*\..."). That
// form is the only one PowerShell's *-Item cmdlets accept reliably with
// -LiteralPath: a bare "HKLM\Foo" (no colon) is treated as a *relative
// filesystem* path and silently creates junk instead of a registry key, which
// is how an apply could report success while the value was never written.
//
// Paths already provider-qualified, or carrying an unrecognised prefix, are
// returned unchanged.
func normalizeRegistryProviderPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return path
	}
	if strings.HasPrefix(strings.ToUpper(trimmed), "REGISTRY::") {
		return trimmed
	}

	hive := trimmed
	rest := ""
	if idx := strings.IndexByte(trimmed, '\\'); idx >= 0 {
		hive = trimmed[:idx]
		rest = trimmed[idx:]
	}
	hive = strings.TrimSuffix(hive, ":")

	full, ok := registryHiveAliases[strings.ToUpper(hive)]
	if !ok {
		return path
	}
	return "Registry::" + full + rest
}

// NormalizeRegistryParams canonicalizes registry value specs into a list form
// that is easy for PowerShell scripts to consume.
func NormalizeRegistryParams(params map[string]any) (map[string]any, error) {
	cloned := CloneParams(params)
	if rawPath, ok := cloned["path"]; ok && rawPath != nil {
		path, ok := rawPath.(string)
		if !ok {
			return nil, fmt.Errorf("registry path must be a string, got %T", rawPath)
		}
		cloned["path"] = normalizeRegistryProviderPath(path)
	}
	if rawUser, ok := cloned["user"]; ok && rawUser != nil {
		user, ok := rawUser.(string)
		if !ok {
			return nil, fmt.Errorf("registry user must be a string, got %T", rawUser)
		}
		cloned["user"] = strings.TrimSpace(user)
	}
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

			patch, hasPatch, err := normalizeRegistryPatch(spec["patch"])
			if err != nil {
				return nil, fmt.Errorf("registry values[%d].patch: %w", i, err)
			}
			if hasPatch {
				if valueType == "" {
					valueType = "binary"
				}
				if valueType != "binary" {
					return nil, fmt.Errorf("registry values[%d].patch is only supported for binary values", i)
				}
				entry["patch"] = patch
			}

			value, hasData := spec["data"]
			if !hasData {
				if !hasPatch {
					return nil, fmt.Errorf("registry values[%d].data is required when ensure=present", i)
				}
				entry["type"] = valueType
				values = append(values, entry)
				continue
			}
			if hasPatch {
				return nil, fmt.Errorf("registry values[%d].data cannot be combined with patch", i)
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

func normalizeRegistryPatch(raw any) ([]map[string]any, bool, error) {
	if raw == nil {
		return nil, false, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, true, fmt.Errorf("must be a list, got %T", raw)
	}
	if len(items) == 0 {
		return nil, true, fmt.Errorf("must not be empty")
	}
	patches := make([]map[string]any, 0, len(items))
	for i, item := range items {
		spec, ok := item.(map[string]any)
		if !ok {
			return nil, true, fmt.Errorf("[%d] must be an object, got %T", i, item)
		}
		rawOffset, ok := spec["offset"]
		if !ok {
			return nil, true, fmt.Errorf("[%d].offset is required", i)
		}
		offset, err := normalizeIntegralValue(rawOffset, 32)
		if err != nil {
			return nil, true, fmt.Errorf("[%d].offset: %w", i, err)
		}
		if offset < 0 {
			return nil, true, fmt.Errorf("[%d].offset must be non-negative", i)
		}
		rawData, ok := spec["data"]
		if !ok {
			return nil, true, fmt.Errorf("[%d].data is required", i)
		}
		data, err := normalizeIntegralValue(rawData, 8)
		if err != nil {
			return nil, true, fmt.Errorf("[%d].data: %w", i, err)
		}
		patches = append(patches, map[string]any{
			"offset": offset,
			"data":   data,
		})
	}
	return patches, true, nil
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
