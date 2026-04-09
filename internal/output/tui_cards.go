package output

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderFactsCard(e FactsEvent, width int) string {
	card := newTUICard("◌ Facts")
	target := fallbackTarget(e.Target)
	lines := tsRenderFactValueLines("Target", target, 0, factsContentWidth(width), true)
	lines = append(lines, tsRenderFactsMap(e.Facts, 0, factsContentWidth(width), true)...)
	card.add(strings.Join(lines, "\n"))
	return card.render()
}

func renderPlanCard(e PlanEvent) string {
	card := newTUICard("☰ Execution Plan")

	rows := [][2]string{{"Target", fallbackTarget(e.Target)}}
	if e.PlaybookName != "" {
		rows = append(rows, [2]string{"Playbook", e.PlaybookName})
	}
	card.add(tsRenderPairs(rows))

	if len(e.Tasks) == 0 {
		card.add(tsMuted.Render("No tasks resolved."))
		return card.render()
	}

	taskRows := make([][]string, 0, len(e.Tasks))
	for _, task := range e.Tasks {
		var details []string
		if task.When != "" {
			details = append(details, "when: "+task.When)
		}
		if len(task.Tags) > 0 {
			details = append(details, "tags: "+strings.Join(task.Tags, ", "))
		}
		taskRows = append(taskRows, []string{
			strconv.Itoa(task.Number),
			task.Module,
			task.Name,
			strings.Join(details, "  ·  "),
		})
	}
	card.add(tsRenderSimpleTable([]string{"#", "MODULE", "TASK", "DETAILS"}, taskRows))
	return card.render()
}

func renderStateCard(e StateEvent) string {
	title := "◫ State Snapshot"
	if e.PlaybookName != "" {
		title = "◫ State Diff"
	}

	card := newTUICard(title)
	rows := [][2]string{
		{"State file", e.StatePath},
		{"Last applied", e.LastApplied},
	}
	if e.Target != "" {
		rows = append([][2]string{{"Target", e.Target}}, rows...)
	}
	if e.PlaybookName != "" {
		rows = append([][2]string{{"Playbook", e.PlaybookName}}, rows...)
	}
	card.add(tsRenderPairs(rows))

	if len(e.Comparisons) > 0 {
		tableRows := make([][]string, 0, len(e.Comparisons))
		for _, comparison := range e.Comparisons {
			tableRows = append(tableRows, []string{
				tsDecorateStateStatus(comparison.Status),
				comparison.TaskName,
				comparison.Module,
				comparison.RecordedStatus,
			})
		}
		card.add(tsRenderSimpleTable([]string{"STATUS", "TASK", "MODULE", "RECORDED"}, tableRows))
	}

	return card.render()
}

func renderValidationCard(e ValidationEvent) string {
	card := newTUICard("◇ Validate")
	name := e.PlaybookName
	if name == "" {
		name = e.PlaybookPath
	}

	rows := [][2]string{
		{"Playbook", name},
		{"Tasks", fmt.Sprintf("%d", e.TaskCount)},
		{"Resolved refs", fmt.Sprintf("%d", len(e.ResolvedRefs))},
	}
	if e.PlaybookPath != "" {
		rows = append(rows, [2]string{"Path", e.PlaybookPath})
	}
	card.add(tsRenderPairs(rows))

	if e.ErrorCount > 0 {
		card.add(tsFailed.Render(fmt.Sprintf("%d %s", e.ErrorCount, tsPluralize(e.ErrorCount, "error", "errors"))))
	}
	if len(e.ResolvedRefs) > 0 {
		card.addLabeled("Resolved refs", tsRenderBulletList(e.ResolvedRefs, false))
	}

	return card.render()
}

func renderActionCatalogCard(e ActionCatalogEvent) string {
	card := newTUICard("▣ Action Catalog")
	namespace := e.EmbeddedNamespace
	if namespace == "" {
		namespace = "preflight/"
	}

	card.add(tsRenderPairs([][2]string{
		{"Namespace", namespace},
		{"Local dir", e.LocalDir},
		{"Embedded", fmt.Sprintf("%d", len(e.EmbeddedRefs))},
		{"Local", fmt.Sprintf("%d", len(e.LocalRefs))},
	}))
	card.addLabeled("Embedded actions", tsRenderBulletList(e.EmbeddedRefs, false))
	card.addLabeled("Local actions", tsRenderOptionalBulletList(e.LocalRefs))
	return card.render()
}

func renderActionInfoCard(e ActionInfoEvent) string {
	card := newTUICard("◫ Action Info")

	rows := [][2]string{
		{"Ref", e.Ref},
		{"Name", e.Name},
		{"Description", e.Description},
	}
	if e.Version != "" {
		rows = append(rows, [2]string{"Version", e.Version})
	}
	if e.Author != "" {
		rows = append(rows, [2]string{"Author", e.Author})
	}
	card.add(tsRenderPairs(rows))

	if len(e.Inputs) > 0 {
		inputRows := make([][]string, 0, len(e.Inputs))
		for _, input := range e.Inputs {
			required := "optional"
			if input.Required {
				required = "required"
			}
			defaultValue := input.Default
			if defaultValue == "" {
				defaultValue = "—"
			}
			inputRows = append(inputRows, []string{
				input.Name,
				input.Type,
				required,
				defaultValue,
				input.Description,
			})
		}
		card.addLabeled("Inputs", tsRenderSimpleTable(
			[]string{"NAME", "TYPE", "REQUIRED", "DEFAULT", "DESCRIPTION"},
			inputRows,
		))
	}

	card.addLabeled("Tasks", tsRenderBulletList(e.TaskNames, true))
	return card.render()
}

func renderActionFetchCard(e ActionFetchEvent) string {
	card := newTUICard("↳ Fetched Actions")
	rows := make([][]string, 0, len(e.Entries))
	for _, entry := range e.Entries {
		rows = append(rows, []string{entry.Ref, entry.SHA})
	}
	card.add(tsRenderSimpleTable([]string{"REF", "SHA"}, rows))
	return card.render()
}

func tsRenderFactsMap(values map[string]any, indent, width int, topLevel bool) []string {
	keys := sortedFactKeys(values)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, tsRenderFactValueLines(key, values[key], indent, width, topLevel)...)
	}
	return lines
}

func tsRenderFactValueLines(label string, value any, indent, width int, topLevel bool) []string {
	prefix := strings.Repeat(" ", indent)
	labelStyle := tsKey
	if topLevel {
		labelStyle = tsLabel
	}

	switch v := normalizeFactValue(value).(type) {
	case map[string]any:
		if len(v) == 0 {
			return []string{prefix + labelStyle.Render(label) + tsMuted.Render(": {}")}
		}
		lines := []string{prefix + labelStyle.Render(label) + tsMuted.Render(":")}
		lines = append(lines, tsRenderFactsMap(v, indent+2, width, false)...)
		return lines
	case []any:
		if len(v) == 0 {
			return []string{prefix + labelStyle.Render(label) + tsMuted.Render(": []")}
		}
		lines := []string{prefix + labelStyle.Render(label) + tsMuted.Render(":")}
		for _, item := range v {
			lines = append(lines, tsRenderFactListItemLines(item, indent+2, width)...)
		}
		return lines
	default:
		return tsRenderFactScalarLines(prefix, labelStyle, label, formatFactScalar(v), width)
	}
}

func tsRenderFactListItemLines(value any, indent, width int) []string {
	prefix := strings.Repeat(" ", indent)
	switch v := normalizeFactValue(value).(type) {
	case map[string]any:
		if len(v) == 0 {
			return []string{prefix + tsMuted.Render("-") + " " + tsMuted.Render("{}")}
		}
		keys := sortedFactKeys(v)
		lines := append([]string{}, tsRenderFactScalarLines(
			prefix+tsMuted.Render("-")+" ",
			tsKey,
			keys[0],
			formatFactInlineScalar(v[keys[0]]),
			width,
		)...)
		for _, key := range keys[1:] {
			lines = append(lines, tsRenderFactValueLines(key, v[key], indent+2, width, false)...)
		}
		return lines
	case []any:
		lines := []string{prefix + tsMuted.Render("-")}
		for _, item := range v {
			lines = append(lines, tsRenderFactListItemLines(item, indent+2, width)...)
		}
		return lines
	default:
		return []string{prefix + tsMuted.Render("-") + " " + tsValue.Render(formatFactScalar(v))}
	}
}

func tsRenderFactScalarLines(prefix string, labelStyle lipgloss.Style, label, value string, width int) []string {
	labelText := labelStyle.Render(label) + tsMuted.Render(":")
	firstPrefix := prefix + labelText + " "
	available := max(width-lipgloss.Width(firstPrefix), 16)
	parts := wrapFactValue(value, available)
	if len(parts) == 1 {
		return []string{firstPrefix + tsValue.Render(parts[0])}
	}

	lines := []string{prefix + labelText}
	continuationPrefix := prefix + "  "
	for _, part := range parts {
		lines = append(lines, continuationPrefix+tsValue.Render(part))
	}
	return lines
}

func formatFactInlineScalar(value any) string {
	switch v := normalizeFactValue(value).(type) {
	case map[string]any:
		return "{...}"
	case []any:
		return "[...]"
	default:
		return formatFactScalar(v)
	}
}
