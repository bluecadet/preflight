package output

import (
	"sort"
	"strings"
)

func renderTextFactLines(values map[string]any, indent int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, renderTextFactValueLines(key, values[key], indent)...)
	}
	return lines
}

func renderTextFactValueLines(label string, value any, indent int) []string {
	prefix := strings.Repeat(" ", indent)
	switch v := normalizeFactValue(value).(type) {
	case map[string]any:
		if len(v) == 0 {
			return []string{prefix + label + ": {}"}
		}
		lines := []string{prefix + label + ":"}
		for _, key := range sortedFactKeys(v) {
			lines = append(lines, renderTextFactValueLines(key, v[key], indent+2)...)
		}
		return lines
	case []any:
		if len(v) == 0 {
			return []string{prefix + label + ": []"}
		}
		lines := []string{prefix + label + ":"}
		for _, item := range v {
			lines = append(lines, renderTextFactListItemLines(item, indent+2)...)
		}
		return lines
	default:
		return []string{prefix + label + ": " + formatFactScalar(v)}
	}
}

func renderTextFactListItemLines(value any, indent int) []string {
	prefix := strings.Repeat(" ", indent)
	switch v := normalizeFactValue(value).(type) {
	case map[string]any:
		if len(v) == 0 {
			return []string{prefix + "- {}"}
		}
		keys := sortedFactKeys(v)
		lines := []string{prefix + "- " + keys[0] + ": " + formatFactInlineValue(v[keys[0]])}
		for _, key := range keys[1:] {
			lines = append(lines, renderTextFactValueLines(key, v[key], indent+2)...)
		}
		return lines
	case []any:
		if len(v) == 0 {
			return []string{prefix + "- []"}
		}
		lines := []string{prefix + "-"}
		for _, item := range v {
			lines = append(lines, renderTextFactListItemLines(item, indent+2)...)
		}
		return lines
	default:
		return []string{prefix + "- " + formatFactScalar(v)}
	}
}
