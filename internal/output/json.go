package output

import (
	"encoding/json"
	"io"
	"time"
)

// jsonEvent is the serializable form of an Event.
type jsonEvent struct {
	Type         EventType            `json:"type"`
	PlayName     string               `json:"play,omitempty"`
	Name         string               `json:"name,omitempty"`
	Namespace    string               `json:"namespace,omitempty"`
	Ref          string               `json:"ref,omitempty"`
	TaskID       string               `json:"task_id,omitempty"`
	Task         string               `json:"task,omitempty"`
	Target       string               `json:"target,omitempty"`
	Status       string               `json:"status,omitempty"`
	Message      string               `json:"message,omitempty"`
	Error        string               `json:"error,omitempty"`
	TaskCount    int                  `json:"task_count,omitempty"`
	OKCount      *int                 `json:"ok_count,omitempty"`
	ChangedCount *int                 `json:"changed_count,omitempty"`
	FailedCount  *int                 `json:"failed_count,omitempty"`
	SkippedCount *int                 `json:"skipped_count,omitempty"`
	Lines        []string             `json:"lines,omitempty"`
	Output       []string             `json:"output,omitempty"`
	Facts        map[string]any       `json:"facts,omitempty"`
	Tasks        []PlanTaskEntry      `json:"tasks,omitempty"`
	StatePath    string               `json:"state_path,omitempty"`
	LastApplied  string               `json:"last_applied,omitempty"`
	Comparisons  []StateComparison    `json:"comparisons,omitempty"`
	PlaybookPath string               `json:"playbook_path,omitempty"`
	VisitedRefs  int                  `json:"visited_refs,omitempty"`
	ResolvedRefs []string             `json:"resolved_refs,omitempty"`
	ErrorCount   int                  `json:"error_count,omitempty"`
	EmbeddedRefs []string             `json:"embedded_refs,omitempty"`
	LocalDir     string               `json:"local_dir,omitempty"`
	LocalRefs    []string             `json:"local_refs,omitempty"`
	Version      string               `json:"version,omitempty"`
	Description  string               `json:"description,omitempty"`
	Author       string               `json:"author,omitempty"`
	Inputs       []ActionInputEntry   `json:"inputs,omitempty"`
	TaskNames    []string             `json:"task_names,omitempty"`
	Entries      []ActionFetchEntry   `json:"entries,omitempty"`
	Plugins      []PluginListEntry    `json:"plugins,omitempty"`
	Hosts        []InventoryHostEntry `json:"hosts,omitempty"`
	Secrets      []SecretListEntry    `json:"secrets,omitempty"`
	TS           string               `json:"ts"`
}

// JSONRenderer writes newline-delimited JSON events to an io.Writer.
type JSONRenderer struct {
	w   io.Writer
	enc *json.Encoder
}

// NewJSONRenderer creates a JSONRenderer.
func NewJSONRenderer(w io.Writer) *JSONRenderer {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &JSONRenderer{w: w, enc: enc}
}

// Emit serialises the event as a single JSON line.
func (r *JSONRenderer) Emit(event Event) {
	ts := time.Now().UTC().Format(time.RFC3339)
	var je jsonEvent
	je.TS = ts

	switch e := event.(type) {
	case PlayStartEvent:
		je.Type = EventPlayStart
		je.PlayName = e.PlayName

	case TaskStartEvent:
		je.Type = EventTaskStart
		je.Task = e.TaskName
		je.TaskID = e.TaskID
		je.Target = e.Target

	case TaskOutputEvent:
		je.Type = EventTaskOutput
		je.Task = e.TaskName
		je.TaskID = e.TaskID
		je.Target = e.Target
		if len(e.Lines) > 0 {
			je.Lines = e.Lines
		}

	case TaskResultEvent:
		je.Type = EventTaskResult
		je.Task = e.TaskName
		je.TaskID = e.TaskID
		je.Target = e.Target
		je.Status = e.Status
		je.Message = e.Message
		if len(e.Output) > 0 {
			je.Output = e.Output
		}

	case PlayEndEvent:
		je.Type = EventPlayEnd
		je.Target = e.Target
		ok := e.OKCount
		ch := e.ChangedCount
		fa := e.FailedCount
		sk := e.SkippedCount
		je.OKCount = &ok
		je.ChangedCount = &ch
		je.FailedCount = &fa
		je.SkippedCount = &sk

	case WarningEvent:
		je.Type = EventWarning
		je.Message = e.Message

	case ErrorEvent:
		je.Type = EventError
		je.Error = e.Message

	case ActivityStartEvent:
		je.Type = EventActivityStart
		je.Target = e.Target
		je.Message = e.Message

	case ActivityResultEvent:
		je.Type = EventActivityResult
		je.Target = e.Target
		je.Message = e.Message
		je.Status = e.Status

	case FactsEvent:
		je.Type = EventFacts
		je.Target = e.Target
		je.Facts = e.Facts

	case PlanEvent:
		je.Type = EventPlan
		je.Target = e.Target
		je.PlayName = e.PlaybookName
		je.Tasks = e.Tasks

	case StateEvent:
		je.Type = EventState
		je.Target = e.Target
		je.PlayName = e.PlaybookName
		je.StatePath = e.StatePath
		je.LastApplied = e.LastApplied
		je.Comparisons = e.Comparisons

	case ValidationEvent:
		je.Type = EventValidate
		je.PlayName = e.PlaybookName
		je.PlaybookPath = e.PlaybookPath
		je.TaskCount = e.TaskCount
		je.VisitedRefs = e.VisitedRefCount
		je.ResolvedRefs = e.ResolvedRefs
		je.ErrorCount = e.ErrorCount

	case ActionCatalogEvent:
		je.Type = EventActionList
		je.Namespace = e.EmbeddedNamespace
		je.EmbeddedRefs = e.EmbeddedRefs
		je.LocalDir = e.LocalDir
		je.LocalRefs = e.LocalRefs

	case ActionInfoEvent:
		je.Type = EventActionInfo
		je.Ref = e.Ref
		je.Name = e.Name
		je.Version = e.Version
		je.Description = e.Description
		je.Author = e.Author
		je.Inputs = e.Inputs
		je.TaskNames = e.TaskNames

	case ActionFetchEvent:
		je.Type = EventActionFetch
		je.Entries = e.Entries

	case PluginListEvent:
		je.Type = EventPluginList
		je.Plugins = e.Entries

	case InventoryListEvent:
		je.Type = EventInventoryList
		je.Hosts = e.Hosts

	case SecretListEvent:
		je.Type = EventSecretList
		je.Secrets = e.Entries
	}

	// Ignore encode errors — nothing useful to do with them at render time.
	_ = r.enc.Encode(je)
}

// Close is a no-op for JSONRenderer.
func (r *JSONRenderer) Close() {}
