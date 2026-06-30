package output

var textFactFormat = &factFormat{
	label: func(s string, _ bool) string { return s },
	muted: func(s string) string { return s },
	value: func(s string) string { return s },
}

func renderTextFactLines(values map[string]any, indent int) []string {
	lines := make([]string, 0, len(values))
	for _, key := range sortedFactKeys(values) {
		lines = append(lines, renderFactValueLines(key, values[key], indent, true, textFactFormat)...)
	}
	return lines
}
