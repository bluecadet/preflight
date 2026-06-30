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

// factFormat controls how fact values are rendered for a specific output sink.
type factFormat struct {
	label  func(string, bool) string             // label rendering (bool = topLevel)
	muted  func(string) string                   // structural text rendering
	value  func(string) string                   // scalar value rendering
	scalar func(string, string, string) []string // optional: word-wrap hook for "label: value" lines
}

// renderFactValueLines traverses a fact value and returns formatted lines.
func renderFactValueLines(label string, value any, indent int, topLevel bool, ff *factFormat) []string {
	prefix := strings.Repeat(" ", indent)
	labelText := ff.label(label, topLevel)
	switch v := value.(type) {
	case map[string]any:
		if len(v) == 0 {
			return []string{prefix + labelText + ff.muted(": {}")}
		}
		lines := []string{prefix + labelText + ff.muted(":")}
		for _, key := range sortedFactKeys(v) {
			lines = append(lines, renderFactValueLines(key, v[key], indent+2, false, ff)...)
		}
		return lines
	case []any:
		if len(v) == 0 {
			return []string{prefix + labelText + ff.muted(": []")}
		}
		lines := []string{prefix + labelText + ff.muted(":")}
		for _, item := range v {
			lines = append(lines, renderFactListItemLines(item, indent+2, ff)...)
		}
		return lines
	default:
		if ff.scalar != nil {
			return ff.scalar(prefix, labelText, formatFactScalar(v))
		}
		return []string{prefix + labelText + ff.muted(": ") + ff.value(formatFactScalar(v))}
	}
}

// renderFactListItemLines formats a list item (scalar, map, or sub-array).
func renderFactListItemLines(value any, indent int, ff *factFormat) []string {
	prefix := strings.Repeat(" ", indent)
	switch v := value.(type) {
	case map[string]any:
		if len(v) == 0 {
			return []string{prefix + ff.muted("-") + " " + ff.muted("{}")}
		}
		keys := sortedFactKeys(v)
		inline := prefix + ff.muted("- ") + ff.label(keys[0], false) + ff.muted(": ") + ff.value(formatFactInlineValue(v[keys[0]]))
		lines := []string{inline}
		for _, key := range keys[1:] {
			lines = append(lines, renderFactValueLines(key, v[key], indent+2, false, ff)...)
		}
		return lines
	case []any:
		if len(v) == 0 {
			return []string{prefix + ff.muted("-") + " " + ff.muted("[]")}
		}
		lines := []string{prefix + ff.muted("-")}
		for _, item := range v {
			lines = append(lines, renderFactListItemLines(item, indent+2, ff)...)
		}
		return lines
	default:
		return []string{prefix + ff.muted("- ") + ff.value(formatFactScalar(v))}
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
	switch v := value.(type) {
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
