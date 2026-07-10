package output

import (
	"fmt"
	"strings"
	"time"
)

type outcomeTotals struct {
	ok      int
	changed int
	failed  int
	skipped int
}

func normalizeRunMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "check":
		return "check"
	case "stage":
		return "stage"
	default:
		return "apply"
	}
}

func titleRunMode(mode string) string {
	switch normalizeRunMode(mode) {
	case "check":
		return "Check"
	case "stage":
		return "Stage"
	default:
		return "Apply"
	}
}

func statusGlyph(status string, checkMode bool) string {
	switch status {
	case "ok":
		return "✓"
	case "changed":
		if checkMode {
			return "!"
		}
		return "~"
	case "failed":
		return "x"
	case "skipped":
		return "-"
	default:
		return " "
	}
}

func changedDetail(message string, checkMode bool) string {
	message = strings.TrimSpace(message)
	if checkMode {
		if message == "" || message == "would apply change (dry-run)" {
			return "would change"
		}
		return "would change: " + message
	}
	if message == "" || message == "change applied" {
		return ""
	}
	return "changed: " + message
}

func okDetail(message string) string {
	message = strings.TrimSpace(message)
	if message == "" || message == "already in desired state" {
		return ""
	}
	return "ok: " + message
}

func renderDisplayPath(actionPath string) string {
	segments := strings.Split(strings.Trim(actionPath, "/"), "/")
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment != "" {
			parts = append(parts, segment)
		}
	}
	return strings.Join(parts, " > ")
}

func renderTaskFailurePath(actionPath, taskName string) string {
	path := renderDisplayPath(actionPath)
	if path == "" {
		return taskName
	}
	if taskName == "" {
		return path
	}
	return path + " > " + taskName
}

// buildTargetRosterLines renders the aligned multi-target roster rows
// (name padded to the longest, followed by " (transport" and optional
// " • address" and ")"), each prefixed with a two-space indent. Used by both
// the TUI and text renderers to fold the roster into the run-start header.
// colorName, when non-nil, renders a target's raw name in its assigned host
// color; the padding and parenthetical transport/address detail stay
// uncolored. Padding is applied with plain spaces so ANSI escapes in the
// colored name do not throw off the column width.
func buildTargetRosterLines(targets []TargetInfo, colorName func(string) string) []string {
	if len(targets) == 0 {
		return nil
	}
	maxName := 0
	for _, ti := range targets {
		maxName = max(maxName, len(ti.Name))
	}
	lines := make([]string, 0, len(targets))
	for _, ti := range targets {
		name := ti.Name
		if colorName != nil {
			name = colorName(name)
		}
		pad := strings.Repeat(" ", maxName-len(ti.Name))
		s := "  " + name + pad + " (" + ti.Transport
		if ti.Address != "" {
			s += " • " + ti.Address
		}
		s += ")"
		lines = append(lines, s)
	}
	return lines
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%02dm", hours, minutes)
}

// padLine delegates to the shared PadLine helper.
func padLine(left, right string, width int) string {
	return PadLine(left, right, width)
}

func indentWrapped(indent int, message string) []string {
	prefix := strings.Repeat(" ", indent)
	width := lineWidth - indent
	wrapped := wrapFactValue(message, width)
	lines := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		lines = append(lines, prefix+line)
	}
	return lines
}

func outputBlockLines(lines []string, indent int) []string {
	var out []string
	for _, line := range lines {
		for _, wrapped := range wrapFactValue(line, lineWidth-indent) {
			out = append(out, strings.Repeat(" ", indent)+wrapped)
		}
	}
	return out
}

func limitFailureOutput(limit int, lines []string) []string {
	if len(lines) <= limit {
		return lines
	}
	return lines[len(lines)-limit:]
}

func recapTotals(recaps []struct{ ok, changed, failed, skipped int }) outcomeTotals {
	var totals outcomeTotals
	for _, recap := range recaps {
		totals.ok += recap.ok
		totals.changed += recap.changed
		totals.failed += recap.failed
		totals.skipped += recap.skipped
	}
	return totals
}

func renderTaskTotals(t outcomeTotals, checkMode bool, warnings int) string {
	changedLabel := "changed"
	if checkMode {
		changedLabel = "would change"
	}
	parts := []string{
		fmt.Sprintf("%d ok", t.ok),
		fmt.Sprintf("%d %s", t.changed, changedLabel),
		fmt.Sprintf("%d skipped", t.skipped),
		fmt.Sprintf("%d failed", t.failed),
	}
	if warnings > 0 {
		parts = append(parts, fmt.Sprintf("%d warnings", warnings))
	}
	return strings.Join(parts, ", ")
}
