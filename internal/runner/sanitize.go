package runner

import (
	"strings"
	"unicode"
)

func sanitizeSlug(s string, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}

	var b strings.Builder
	b.Grow(len(s))
	lastDash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return fallback
	}
	return out
}
