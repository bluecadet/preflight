package output

import (
	"fmt"
	"sort"
	"strings"
)

type textBlockBuilder struct {
	lines []string
}

func (b *textBlockBuilder) linef(format string, args ...any) {
	b.lines = append(b.lines, fmt.Sprintf(format, args...))
}

func (b *textBlockBuilder) line(line string) {
	b.lines = append(b.lines, line)
}

func (b *textBlockBuilder) blank() {
	if len(b.lines) == 0 || b.lines[len(b.lines)-1] == "" {
		return
	}
	b.lines = append(b.lines, "")
}

func (b *textBlockBuilder) bulletList(title string, items []string) {
	if title != "" {
		b.line(title)
	}
	for _, item := range items {
		b.line("  - " + item)
	}
}

func (b *textBlockBuilder) numberedList(title string, items []string) {
	if title != "" {
		b.line(title)
	}
	for i, item := range items {
		b.linef("  %d. %s", i+1, item)
	}
}

func (b *textBlockBuilder) values() []string {
	return append([]string(nil), b.lines...)
}

func renderTextFacts(e FactsEvent) []string {
	var b textBlockBuilder
	b.linef("Facts for %s:", fallbackTarget(e.Target))
	for _, line := range renderTextFactLines(e.Facts, 2) {
		b.line(line)
	}
	return b.values()
}

func renderTextPlan(e PlanEvent) []string {
	var b textBlockBuilder
	b.linef("Target: %s", fallbackTarget(e.Target))
	b.linef("Playbook: %s", e.PlaybookName)
	b.linef("Tasks (%d):", len(e.Tasks))
	for _, task := range e.Tasks {
		line := fmt.Sprintf("  %d. [%s] %s", task.Number, task.Module, task.Name)
		if task.When != "" {
			line += " (when: " + task.When + ")"
		}
		if len(task.Tags) > 0 {
			line += " [tags: " + fmt.Sprintf("%v", task.Tags) + "]"
		}
		b.line(line)
	}
	return b.values()
}

func renderTextState(e StateEvent) []string {
	var b textBlockBuilder
	if e.PlaybookName != "" {
		b.linef("State diff for playbook: %s", e.PlaybookName)
	}
	if e.Target != "" {
		b.linef("Target: %s", e.Target)
	}
	b.linef("State file: %s", e.StatePath)
	b.linef("Last applied: %s", e.LastApplied)
	if len(e.Comparisons) == 0 {
		return b.values()
	}

	b.blank()
	b.line(fmt.Sprintf("%-12s %-28s %-16s %s", "STATUS", "TASK", "MODULE", "RECORDED STATUS"))
	b.line(fmt.Sprintf("%-12s %-28s %-16s %s", "------------", "----------------------------", "----------------", "---------------"))
	for _, comparison := range e.Comparisons {
		b.linef("%-12s %-28s %-16s %s",
			comparison.Status,
			comparison.TaskName,
			comparison.Module,
			comparison.RecordedStatus,
		)
	}
	return b.values()
}

func renderTextValidation(e ValidationEvent) []string {
	var b textBlockBuilder
	name := e.PlaybookName
	if name == "" {
		name = e.PlaybookPath
	}
	b.linef("Validated: %s (%d tasks, %d action refs resolved)", name, e.TaskCount, len(e.ResolvedRefs))
	if len(e.ResolvedRefs) > 0 {
		b.bulletList("Resolved refs:", e.ResolvedRefs)
	}
	if e.ErrorCount > 0 {
		b.linef("Errors: %d", e.ErrorCount)
	}
	return b.values()
}

func renderTextActionCatalog(e ActionCatalogEvent) []string {
	var b textBlockBuilder
	namespace := e.EmbeddedNamespace
	if namespace == "" {
		namespace = "preflight/"
	}
	b.linef("Embedded actions (%s):", namespace)
	for _, ref := range e.EmbeddedRefs {
		b.line("  " + ref)
	}

	b.blank()
	b.linef("Local actions (%s):", e.LocalDir)
	if len(e.LocalRefs) == 0 {
		b.line("  (none)")
		return b.values()
	}
	for _, ref := range e.LocalRefs {
		b.line("  " + ref)
	}
	return b.values()
}

func renderTextActionInfo(e ActionInfoEvent) []string {
	var b textBlockBuilder
	b.linef("Name:        %s", e.Name)
	b.linef("Version:     %s", e.Version)
	b.linef("Description: %s", e.Description)
	if e.Author != "" {
		b.linef("Author:      %s", e.Author)
	}
	if len(e.Inputs) > 0 {
		b.blank()
		b.line("Inputs:")
		for _, input := range e.Inputs {
			required := ""
			if input.Required {
				required = " (required)"
			}
			defaultValue := ""
			if input.Default != "" {
				defaultValue = " [default: " + input.Default + "]"
			}
			b.linef("  %-20s %s%s%s",
				input.Name+":",
				input.Description,
				required,
				defaultValue,
			)
		}
	}

	b.blank()
	b.numberedList(fmt.Sprintf("Tasks (%d):", len(e.TaskNames)), e.TaskNames)
	return b.values()
}

func renderTextActionFetch(e ActionFetchEvent) []string {
	lines := make([]string, 0, len(e.Entries))
	for _, entry := range e.Entries {
		lines = append(lines, fmt.Sprintf("Fetched %s -> %s", entry.Ref, entry.SHA))
	}
	return lines
}

func renderTextPluginList(e PluginListEvent) []string {
	if len(e.Entries) == 0 {
		return []string{"No plugins found."}
	}

	lines := []string{
		fmt.Sprintf("%-24s %-12s %-8s %s", "NAME", "VERSION", "STATUS", "PATH"),
		fmt.Sprintf("%-24s %-12s %-8s %s", "----", "-------", "------", "----"),
	}
	for _, entry := range e.Entries {
		lines = append(lines, fmt.Sprintf("%-24s %-12s %-8s %s", entry.Name, entry.Version, entry.Status, entry.Path))
	}
	return lines
}

func renderTextInventoryList(e InventoryListEvent) []string {
	if len(e.Hosts) == 0 {
		return []string{"No hosts found in inventory."}
	}

	nameW, addrW := len("NAME"), len("ADDRESS")
	for _, host := range e.Hosts {
		if len(host.Name) > nameW {
			nameW = len(host.Name)
		}
		if len(host.Address) > addrW {
			addrW = len(host.Address)
		}
	}
	nameW += 2
	addrW += 2

	row := fmt.Sprintf("%%-%ds %%-%ds %%-10s %%-6s %%s", nameW, addrW)
	lines := []string{
		fmt.Sprintf(row, "NAME", "ADDRESS", "TRANSPORT", "PORT", "GROUPS"),
		fmt.Sprintf(row,
			strings.Repeat("-", nameW-2),
			strings.Repeat("-", addrW-2),
			strings.Repeat("-", 10),
			strings.Repeat("-", 6),
			strings.Repeat("-", 20),
		),
	}

	rowData := fmt.Sprintf("%%-%ds %%-%ds %%-10s %%-6d %%s", nameW, addrW)
	for _, host := range e.Hosts {
		groups := append([]string(nil), host.Groups...)
		sort.Strings(groups)
		lines = append(lines, fmt.Sprintf(rowData, host.Name, host.Address, host.Transport, host.Port, strings.Join(groups, ", ")))
	}
	return lines
}

func renderTextSecretList(e SecretListEvent) []string {
	if len(e.Entries) == 0 {
		return []string{"No secrets configured."}
	}

	lines := make([]string, 0, len(e.Entries))
	for _, entry := range e.Entries {
		lines = append(lines, fmt.Sprintf("%-24s %s", entry.Name, entry.File))
	}
	return lines
}

func formatActivityLine(target, message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "connecting"
	}
	return uppercaseFirst(message) + " to " + fallbackTarget(target) + "..."
}
