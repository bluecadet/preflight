package output

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// EventType identifies the kind of output event.
type EventType string

const (
	EventPlayStart      EventType = "play_start"
	EventTaskStart      EventType = "task_start"
	EventTaskOutput     EventType = "task_output"
	EventTaskResult     EventType = "task_result"
	EventPlayEnd        EventType = "play_end"
	EventWarning        EventType = "warning"
	EventError          EventType = "error"
	EventFacts          EventType = "facts"
	EventPlan           EventType = "plan"
	EventState          EventType = "state"
	EventValidate       EventType = "validate"
	EventActionList     EventType = "action_list"
	EventActionInfo     EventType = "action_info"
	EventActionFetch    EventType = "action_fetch"
	EventActivityStart  EventType = "activity_start"
	EventActivityResult EventType = "activity_result"
)

// Event is the sealed interface implemented by all renderer event types.
type Event interface{ isEvent() }

type PlayStartEvent struct{ PlayName string }
type TaskStartEvent struct{ TaskName, TaskID, ActionPath, Target string }
type TaskOutputEvent struct {
	TaskName, TaskID, Target string
	Lines                    []string
}
type TaskResultEvent struct {
	TaskName, TaskID, ActionPath, Target, Status, Message string
	Output                                                []string
}
type PlayEndEvent struct {
	Target                                           string
	OKCount, ChangedCount, FailedCount, SkippedCount int
}
type WarningEvent struct{ Message string }
type ErrorEvent struct{ Message string }
type ActivityStartEvent struct{ Target, Message string }
type ActivityResultEvent struct{ Target, Message, Status string }

// FactsEvent carries gathered facts for a single target.
type FactsEvent struct {
	Target string
	Facts  map[string]any
}

// PlanTaskEntry describes a single planned task for PlanEvent.
type PlanTaskEntry struct {
	Number int
	Module string
	Name   string
	When   string
	Tags   []string
}

// PlanEvent carries the resolved execution plan for a single target.
type PlanEvent struct {
	Target       string
	PlaybookName string
	Tasks        []PlanTaskEntry
}

// StateEvent carries the state comparison data for a single target.
type StateEvent struct {
	Target       string
	PlaybookName string
	StatePath    string
	LastApplied  string
	Comparisons  []StateComparison
}

// StateComparison is a single row in the state diff table.
type StateComparison struct {
	Status         string
	TaskName       string
	Module         string
	RecordedStatus string
}

// ValidationEvent carries the results of a validate command run.
type ValidationEvent struct {
	PlaybookPath    string
	PlaybookName    string
	TaskCount       int
	VisitedRefCount int
	ResolvedRefs    []string
	ErrorCount      int
}

// ActionCatalogEvent carries the available action refs grouped by source.
type ActionCatalogEvent struct {
	EmbeddedNamespace string
	EmbeddedRefs      []string
	LocalDir          string
	LocalRefs         []string
}

// ActionInputEntry describes one input row for ActionInfoEvent.
type ActionInputEntry struct {
	Name        string
	Type        string
	Description string
	Required    bool
	Default     string
}

// ActionInfoEvent carries metadata about a single action ref.
type ActionInfoEvent struct {
	Ref         string
	Name        string
	Version     string
	Description string
	Author      string
	Inputs      []ActionInputEntry
	TaskNames   []string
}

// ActionFetchEntry describes one fetched remote action ref.
type ActionFetchEntry struct {
	Ref string
	SHA string
}

// ActionFetchEvent carries the refs fetched by action fetch.
type ActionFetchEvent struct {
	Entries []ActionFetchEntry
}

func (PlayStartEvent) isEvent()      {}
func (TaskStartEvent) isEvent()      {}
func (TaskOutputEvent) isEvent()     {}
func (TaskResultEvent) isEvent()     {}
func (PlayEndEvent) isEvent()        {}
func (WarningEvent) isEvent()        {}
func (ErrorEvent) isEvent()          {}
func (ActivityStartEvent) isEvent()  {}
func (ActivityResultEvent) isEvent() {}
func (FactsEvent) isEvent()          {}
func (PlanEvent) isEvent()           {}
func (StateEvent) isEvent()          {}
func (ValidationEvent) isEvent()     {}
func (ActionCatalogEvent) isEvent()  {}
func (ActionInfoEvent) isEvent()     {}
func (ActionFetchEvent) isEvent()    {}

// Renderer is the interface that all output renderers implement.
type Renderer interface {
	Emit(event Event)
	Close()
}

// ANSI color codes.
const (
	ansiReset  = "\033[0m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiGrey   = "\033[90m"
	ansiBold   = "\033[1m"
	ansiCyan   = "\033[36m"
)

const lineWidth = 80

// isTTY returns true if w is os.Stdout or os.Stderr and the fd is a terminal.
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	// ModeCharDevice is set for terminal devices.
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// TextRenderer writes Ansible-style human-readable output.
type TextRenderer struct {
	w          io.Writer
	color      bool
	verbose    bool
	taskOutput map[string][]string
}

// NewTextRenderer creates a TextRenderer. Colors are enabled only when w is a TTY.
func NewTextRenderer(w io.Writer) *TextRenderer {
	return NewTextRendererWithOptions(w, Options{})
}

// NewTextRendererWithOptions creates a TextRenderer with the provided options.
func NewTextRendererWithOptions(w io.Writer, opts Options) *TextRenderer {
	return &TextRenderer{
		w:          w,
		color:      isTTY(w),
		verbose:    opts.Verbose,
		taskOutput: make(map[string][]string),
	}
}

func (r *TextRenderer) colorize(code, text string) string {
	if !r.color {
		return text
	}
	return code + text + ansiReset
}

func fillLine(prefix, fill string, width int) string {
	remaining := width - len(prefix)
	if remaining <= 0 {
		return prefix
	}
	return prefix + " " + strings.Repeat(fill, remaining-1)
}

func (r *TextRenderer) writeOutputLines(lines []string) {
	for _, line := range lines {
		_, _ = fmt.Fprintf(r.w, "  │ %s\n", line)
	}
}

func taskBufferKey(taskID, taskName, target string) string {
	var base string
	if taskID != "" {
		base = taskID
	} else {
		base = taskName
	}
	if target == "" {
		return base
	}
	return target + "\x00" + base
}

func (r *TextRenderer) bufferTaskOutput(e TaskOutputEvent) bool {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	if key == "" {
		return false
	}
	if r.taskOutput == nil {
		r.taskOutput = make(map[string][]string)
	}
	r.taskOutput[key] = append(r.taskOutput[key], e.Lines...)
	return true
}

func (r *TextRenderer) takeBufferedOutput(e TaskResultEvent) []string {
	key := taskBufferKey(e.TaskID, e.TaskName, e.Target)
	if key == "" || r.taskOutput == nil {
		return nil
	}
	lines := r.taskOutput[key]
	delete(r.taskOutput, key)
	return lines
}

// Emit writes a formatted line (or block) for the given event.
func (r *TextRenderer) Emit(event Event) {
	switch e := event.(type) {
	case PlayStartEvent:
		title := fmt.Sprintf("PLAY [%s]", e.PlayName)
		line := fillLine(title, "*", lineWidth)
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiBold, line))
		_, _ = fmt.Fprintln(r.w)

	case TaskStartEvent:
		_, _ = fmt.Fprintf(r.w, "TASK [%s]\n", e.TaskName)

	case TaskOutputEvent:
		if !r.bufferTaskOutput(e) {
			r.writeOutputLines(e.Lines)
		}

	case TaskResultEvent:
		label := fmt.Sprintf("TASK [%s]", e.TaskName)
		statusStr := r.statusColored(e.Status, e.Message)
		dotsNeeded := lineWidth - len(label) - len(e.Status) - 3
		dotsNeeded = max(dotsNeeded, 1)
		dots := strings.Repeat(".", dotsNeeded)
		_, _ = fmt.Fprintf(r.w, "%s %s %s\n", label, dots, statusStr)
		buffered := r.takeBufferedOutput(e)
		lines := buffered
		if len(e.Output) > 0 {
			lines = e.Output
		}
		if len(lines) > 0 && (r.verbose || e.Status == "failed") {
			r.writeOutputLines(lines)
		}

	case PlayEndEvent:
		title := "PLAY RECAP"
		line := fillLine(title, "*", lineWidth)
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiBold, line))
		target := e.Target
		if target == "" {
			target = "localhost"
		}
		recap := fmt.Sprintf("%-14s : ok=%-4d changed=%-4d failed=%-4d skipped=%-4d",
			target,
			e.OKCount,
			e.ChangedCount,
			e.FailedCount,
			e.SkippedCount,
		)
		_, _ = fmt.Fprintln(r.w, recap)
		_, _ = fmt.Fprintln(r.w)

	case ErrorEvent:
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiRed, "ERROR: "+e.Message))

	case WarningEvent:
		_, _ = fmt.Fprintln(r.w, r.colorize(ansiYellow, "WARNING: "+e.Message))

	case ActivityStartEvent:
		_, _ = fmt.Fprintln(r.w, formatActivityLine(e.Target, e.Message))

	case ActivityResultEvent:
		if e.Status == "failed" {
			_, _ = fmt.Fprintln(r.w, r.colorize(ansiRed, formatActivityLine(e.Target, e.Message)))
		}

	case FactsEvent:
		target := e.Target
		if target == "" {
			target = "localhost"
		}
		_, _ = fmt.Fprintf(r.w, "Facts for %s:\n", target)
		for _, line := range renderFactLines(e.Facts, 2) {
			_, _ = fmt.Fprintln(r.w, line)
		}

	case PlanEvent:
		target := e.Target
		if target == "" {
			target = "localhost"
		}
		_, _ = fmt.Fprintf(r.w, "Target: %s\n", target)
		_, _ = fmt.Fprintf(r.w, "Playbook: %s\n", e.PlaybookName)
		_, _ = fmt.Fprintf(r.w, "Tasks (%d):\n", len(e.Tasks))
		for _, t := range e.Tasks {
			_, _ = fmt.Fprintf(r.w, "  %d. [%s] %s", t.Number, t.Module, t.Name)
			if t.When != "" {
				_, _ = fmt.Fprintf(r.w, " (when: %s)", t.When)
			}
			if len(t.Tags) > 0 {
				_, _ = fmt.Fprintf(r.w, " [tags: %v]", t.Tags)
			}
			_, _ = fmt.Fprintln(r.w)
		}

	case StateEvent:
		if e.PlaybookName != "" {
			_, _ = fmt.Fprintf(r.w, "State diff for playbook: %s\n", e.PlaybookName)
		}
		if e.Target != "" {
			_, _ = fmt.Fprintf(r.w, "Target: %s\n", e.Target)
		}
		_, _ = fmt.Fprintf(r.w, "State file: %s\n", e.StatePath)
		_, _ = fmt.Fprintf(r.w, "Last applied: %s\n\n", e.LastApplied)
		if len(e.Comparisons) > 0 {
			_, _ = fmt.Fprintf(r.w, "%-12s %-28s %-16s %s\n", "STATUS", "TASK", "MODULE", "RECORDED STATUS")
			_, _ = fmt.Fprintf(r.w, "%-12s %-28s %-16s %s\n", "------------", "----------------------------", "----------------", "---------------")
			for _, c := range e.Comparisons {
				_, _ = fmt.Fprintf(r.w, "%-12s %-28s %-16s %s\n", c.Status, c.TaskName, c.Module, c.RecordedStatus)
			}
		}

	case ValidationEvent:
		name := e.PlaybookName
		if name == "" {
			name = e.PlaybookPath
		}
		_, _ = fmt.Fprintf(r.w, "Validated: %s (%d tasks, %d action refs resolved)\n", name, e.TaskCount, len(e.ResolvedRefs))
		if len(e.ResolvedRefs) > 0 {
			_, _ = fmt.Fprintln(r.w, "Resolved refs:")
			for _, ref := range e.ResolvedRefs {
				_, _ = fmt.Fprintf(r.w, "  - %s\n", ref)
			}
		}
		if e.ErrorCount > 0 {
			_, _ = fmt.Fprintf(r.w, "Errors: %d\n", e.ErrorCount)
		}

	case ActionCatalogEvent:
		namespace := e.EmbeddedNamespace
		if namespace == "" {
			namespace = "preflight/"
		}
		_, _ = fmt.Fprintf(r.w, "Embedded actions (%s):\n", namespace)
		for _, ref := range e.EmbeddedRefs {
			_, _ = fmt.Fprintf(r.w, "  %s\n", ref)
		}
		_, _ = fmt.Fprintf(r.w, "\nLocal actions (%s):\n", e.LocalDir)
		if len(e.LocalRefs) == 0 {
			_, _ = fmt.Fprintln(r.w, "  (none)")
			break
		}
		for _, ref := range e.LocalRefs {
			_, _ = fmt.Fprintf(r.w, "  %s\n", ref)
		}

	case ActionInfoEvent:
		_, _ = fmt.Fprintf(r.w, "Name:        %s\n", e.Name)
		_, _ = fmt.Fprintf(r.w, "Version:     %s\n", e.Version)
		_, _ = fmt.Fprintf(r.w, "Description: %s\n", e.Description)
		if e.Author != "" {
			_, _ = fmt.Fprintf(r.w, "Author:      %s\n", e.Author)
		}
		if len(e.Inputs) > 0 {
			_, _ = fmt.Fprintln(r.w, "\nInputs:")
			for _, input := range e.Inputs {
				required := ""
				if input.Required {
					required = " (required)"
				}
				defaultValue := ""
				if input.Default != "" {
					defaultValue = " [default: " + input.Default + "]"
				}
				_, _ = fmt.Fprintf(r.w, "  %-20s %s%s%s\n", input.Name+":", input.Description, required, defaultValue)
			}
		}
		_, _ = fmt.Fprintf(r.w, "\nTasks (%d):\n", len(e.TaskNames))
		for i, taskName := range e.TaskNames {
			_, _ = fmt.Fprintf(r.w, "  %d. %s\n", i+1, taskName)
		}

	case ActionFetchEvent:
		for _, entry := range e.Entries {
			_, _ = fmt.Fprintf(r.w, "Fetched %s -> %s\n", entry.Ref, entry.SHA)
		}
	}
}

func (r *TextRenderer) statusColored(status, message string) string {
	label := status
	if message != "" {
		label = fmt.Sprintf("%s (%s)", status, message)
	}
	switch status {
	case "ok":
		return r.colorize(ansiGreen, label)
	case "changed":
		return r.colorize(ansiYellow, label)
	case "failed":
		return r.colorize(ansiRed, label)
	case "skipped":
		return r.colorize(ansiGrey, label)
	default:
		return label
	}
}

func formatActivityLine(target, message string) string {
	target = fallbackTarget(target)
	message = strings.TrimSpace(message)
	if message == "" {
		message = "connecting"
	}
	return uppercaseFirst(message) + " to " + target + "..."
}

func fallbackTarget(target string) string {
	if target == "" {
		return "localhost"
	}
	return target
}

func renderFactLines(values map[string]any, indent int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var lines []string
	for _, key := range keys {
		lines = append(lines, renderFactValueLines(key, values[key], indent)...)
	}
	return lines
}

func renderFactValueLines(label string, value any, indent int) []string {
	prefix := strings.Repeat(" ", indent)
	switch v := normalizeFactValue(value).(type) {
	case map[string]any:
		if len(v) == 0 {
			return []string{prefix + label + ": {}"}
		}
		lines := []string{prefix + label + ":"}
		for _, key := range sortedFactKeys(v) {
			lines = append(lines, renderFactValueLines(key, v[key], indent+2)...)
		}
		return lines
	case []any:
		if len(v) == 0 {
			return []string{prefix + label + ": []"}
		}
		lines := []string{prefix + label + ":"}
		for _, item := range v {
			lines = append(lines, renderFactListItemLines(item, indent+2)...)
		}
		return lines
	default:
		return []string{prefix + label + ": " + formatFactScalar(v)}
	}
}

func renderFactListItemLines(value any, indent int) []string {
	prefix := strings.Repeat(" ", indent)
	switch v := normalizeFactValue(value).(type) {
	case map[string]any:
		if len(v) == 0 {
			return []string{prefix + "- {}"}
		}
		keys := sortedFactKeys(v)
		first := keys[0]
		lines := []string{prefix + "- " + first + ": " + formatFactInlineValue(v[first])}
		for _, key := range keys[1:] {
			lines = append(lines, renderFactValueLines(key, v[key], indent+2)...)
		}
		return lines
	case []any:
		if len(v) == 0 {
			return []string{prefix + "- []"}
		}
		lines := []string{prefix + "-"}
		for _, item := range v {
			lines = append(lines, renderFactListItemLines(item, indent+2)...)
		}
		return lines
	default:
		return []string{prefix + "- " + formatFactScalar(v)}
	}
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
	preferredOrder := map[string]int{
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
	sort.Slice(keys, func(i, j int) bool {
		leftRank, leftPreferred := preferredOrder[keys[i]]
		rightRank, rightPreferred := preferredOrder[keys[j]]
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
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		return s
	case float32:
		s := fmt.Sprintf("%.2f", v)
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		return s
	default:
		return fmt.Sprintf("%v", v)
	}
}

func uppercaseFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Close is a no-op for TextRenderer.
func (r *TextRenderer) Close() {}
