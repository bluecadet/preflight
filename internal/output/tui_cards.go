package output

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderFactsCard(e FactsEvent, width int) string {
	card := newTUICard("◌ Facts")
	cw := factsContentWidth(width)
	ff := &factFormat{
		label: func(s string, topLevel bool) string {
			if topLevel {
				return S.Label.Render(s)
			}
			return S.Key.Render(s)
		},
		muted: func(s string) string { return S.Muted.Render(s) },
		value: func(s string) string { return S.Value.Render(s) },
		scalar: func(prefix, labelText, value string) []string {
			labelWithColon := labelText + S.Muted.Render(":")
			firstPrefix := prefix + labelWithColon + " "
			available := max(cw-lipgloss.Width(firstPrefix), 16)
			parts := wrapFactValue(value, available)
			if len(parts) == 1 {
				return []string{firstPrefix + S.Value.Render(parts[0])}
			}
			lines := []string{prefix + labelWithColon}
			continuationPrefix := prefix + "  "
			for _, part := range parts {
				lines = append(lines, continuationPrefix+S.Value.Render(part))
			}
			return lines
		},
	}
	target := fallbackTarget(e.Target)
	lines := renderFactValueLines("Target", target, 0, true, ff)
	for _, key := range sortedFactKeys(e.Facts) {
		lines = append(lines, renderFactValueLines(key, e.Facts[key], 0, true, ff)...)
	}
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
		card.add(S.Muted.Render("No tasks resolved."))
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
		card.add(S.Failed.Render(fmt.Sprintf("%d %s", e.ErrorCount, tsPluralize(e.ErrorCount, "error", "errors"))))
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
