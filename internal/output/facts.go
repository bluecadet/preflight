package output

import (
	"fmt"
	"sort"
	"strings"
)

var preferredFactKeyOrder = map[string]int{
	"path":     0,
	"name":     1,
	"hostname": 2,
	"version":  3,
	"build":    4,
	"arch":     5,
	"total_gb": 6,
	"free_gb":  7,
	"used_gb":  8,
}

func fallbackTarget(target string) string {
	if target == "" {
		return "localhost"
	}
	return target
}

func uppercaseFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func normalizeFactValue(value any) any {
	switch v := value.(type) {
	case map[string]string:
		m := make(map[string]any, len(v))
		for key, item := range v {
			m[key] = item
		}
		return m
	case []map[string]any:
		items := make([]any, len(v))
		for i, item := range v {
			items[i] = item
		}
		return items
	case []string:
		items := make([]any, len(v))
		for i, item := range v {
			items[i] = item
		}
		return items
	default:
		return value
	}
}

func sortedFactKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {
		leftRank, leftPreferred := preferredFactKeyOrder[keys[i]]
		rightRank, rightPreferred := preferredFactKeyOrder[keys[j]]
		switch {
		case leftPreferred && rightPreferred:
			if leftRank != rightRank {
				return leftRank < rightRank
			}
		case leftPreferred:
			return true
		case rightPreferred:
			return false
		}
		return keys[i] < keys[j]
	})

	return keys
}

func formatFactInlineValue(value any) string {
	switch v := normalizeFactValue(value).(type) {
	case map[string]any:
		return "{...}"
	case []any:
		return "[...]"
	default:
		return formatFactScalar(v)
	}
}

func formatFactScalar(value any) string {
	switch v := value.(type) {
	case float64:
		s := fmt.Sprintf("%.2f", v)
		return strings.TrimRight(strings.TrimRight(s, "0"), ".")
	case float32:
		s := fmt.Sprintf("%.2f", v)
		return strings.TrimRight(strings.TrimRight(s, "0"), ".")
	default:
		return fmt.Sprintf("%v", v)
	}
}
