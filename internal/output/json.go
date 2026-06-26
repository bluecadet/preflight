package output

import (
	"encoding/json"
	"io"
	"time"
)

// jsonEvent is the serializable form of an Event.
type jsonEvent struct {
	Type          EventType            `json:"type"`
	SchemaVersion string               `json:"schema_version,omitempty"`
	Mode          string               `json:"mode,omitempty"`
	PlayName      string               `json:"play,omitempty"`
	PlaybookPath  string               `json:"playbook_path,omitempty"`
	Name          string               `json:"name,omitempty"`
	Namespace     string               `json:"namespace,omitempty"`
	Ref           string               `json:"ref,omitempty"`
	TaskID        string               `json:"task_id,omitempty"`
	Task          string               `json:"task,omitempty"`
	Target        string               `json:"target,omitempty"`
	Status        string               `json:"status,omitempty"`
	Message       string               `json:"message,omitempty"`
	Error         string               `json:"error,omitempty"`
	TaskCount     int                  `json:"task_count,omitempty"`
	OKCount       *int                 `json:"ok_count,omitempty"`
	ChangedCount  *int                 `json:"changed_count,omitempty"`
	FailedCount   *int                 `json:"failed_count,omitempty"`
	SkippedCount  *int                 `json:"skipped_count,omitempty"`
	Lines         []string             `json:"lines,omitempty"`
	Output        []string             `json:"output,omitempty"`
	Facts         map[string]any       `json:"facts,omitempty"`
	Tasks         []PlanTaskEntry      `json:"tasks,omitempty"`
	StatePath     string               `json:"state_path,omitempty"`
	LastApplied   string               `json:"last_applied,omitempty"`
	Comparisons   []StateComparison    `json:"comparisons,omitempty"`
	Targets       []string             `json:"targets,omitempty"`
	VisitedRefs   int                  `json:"visited_refs,omitempty"`
	ResolvedRefs  []string             `json:"resolved_refs,omitempty"`
	ErrorCount    int                  `json:"error_count,omitempty"`
	EmbeddedRefs  []string             `json:"embedded_refs,omitempty"`
	LocalDir      string               `json:"local_dir,omitempty"`
	LocalRefs     []string             `json:"local_refs,omitempty"`
	Version       string               `json:"version,omitempty"`
	Description   string               `json:"description,omitempty"`
	Author        string               `json:"author,omitempty"`
	Inputs        []ActionInputEntry   `json:"inputs,omitempty"`
	TaskNames     []string             `json:"task_names,omitempty"`
	Entries       []ActionFetchEntry   `json:"entries,omitempty"`
	Plugins       []PluginListEntry    `json:"plugins,omitempty"`
	Hosts         []InventoryHostEntry `json:"hosts,omitempty"`
	Secrets       []SecretListEntry    `json:"secrets,omitempty"`
	TS            string               `json:"ts"`
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
	je := jsonEvent{TS: time.Now().UTC().Format(time.RFC3339)}
	switch e := event.(type) {
	case VersionEvent:
		je.Type = EventVersion
		je.SchemaVersion = e.SchemaVersion
		je.Version = e.PreflightVersion
		je.PlayName = e.PlaybookName
	case RunStartEvent:
		je.Type = EventRunStart
		je.Mode, je.PlayName, je.PlaybookPath = e.Mode, e.PlaybookName, e.PlaybookPath
		je.Targets = e.Targets
	case TargetStartEvent:
		je.Type = EventTargetStart
		je.Target = e.Target
	case TargetCompleteEvent:
		je.Type = EventTargetComplete
		je.Target = e.Target
		okCount, changedCount, failedCount, skippedCount := e.OKCount, e.ChangedCount, e.FailedCount, e.SkippedCount
		je.OKCount, je.ChangedCount, je.FailedCount, je.SkippedCount = &okCount, &changedCount, &failedCount, &skippedCount
	case TaskStartedEvent:
		je.Type = EventTaskStarted
		je.TaskID, je.Task, je.Target = e.TaskID, e.TaskName, e.Target
	case TaskOKEvent:
		je.Type = EventTaskOK
		je.TaskID, je.Task, je.Target = e.TaskID, e.TaskName, e.Target
	case TaskChangedEvent:
		je.Type = EventTaskChanged
		je.TaskID, je.Task, je.Target = e.TaskID, e.TaskName, e.Target
	case TaskSkippedEvent:
		je.Type = EventTaskSkipped
		je.TaskID, je.Task, je.Target = e.TaskID, e.TaskName, e.Target
	case TaskFailedEvent:
		je.Type = EventTaskFailed
		je.TaskID, je.Task, je.Target = e.TaskID, e.TaskName, e.Target
	case RunSummaryEvent:
		je.Type = EventRunSummary
	case TaskOutputEvent:
		je.Type = EventTaskOutput
		je.TaskID, je.Task, je.Target, je.Lines = e.TaskID, e.TaskName, e.Target, e.Lines
	case WarningEvent:
		je.Type = EventWarning
		je.Message = e.Message
	case ActivityStartEvent:
		je.Type = EventActivityStart
		je.Target, je.Message = e.Target, e.Message
	case ActivityResultEvent:
		je.Type = EventActivityResult
		je.Target, je.Message, je.Status = e.Target, e.Message, e.Status
	case FactsEvent:
		je.Type = EventFacts
		je.Target, je.Facts = e.Target, e.Facts
	case PlanEvent:
		je.Type = EventPlan
		je.Target, je.PlayName, je.Tasks = e.Target, e.PlaybookName, e.Tasks
	case StateEvent:
		je.Type = EventState
		je.Target, je.PlayName, je.StatePath, je.LastApplied, je.Comparisons = e.Target, e.PlaybookName, e.StatePath, e.LastApplied, e.Comparisons
	case ValidationEvent:
		je.Type = EventValidate
		je.PlayName, je.PlaybookPath, je.TaskCount, je.VisitedRefs, je.ResolvedRefs, je.ErrorCount = e.PlaybookName, e.PlaybookPath, e.TaskCount, e.VisitedRefCount, e.ResolvedRefs, e.ErrorCount
	case ActionCatalogEvent:
		je.Type = EventActionList
		je.Namespace, je.EmbeddedRefs, je.LocalDir, je.LocalRefs = e.EmbeddedNamespace, e.EmbeddedRefs, e.LocalDir, e.LocalRefs
	case ActionInfoEvent:
		je.Type = EventActionInfo
		je.Ref, je.Name, je.Version, je.Description, je.Author, je.Inputs, je.TaskNames = e.Ref, e.Name, e.Version, e.Description, e.Author, e.Inputs, e.TaskNames
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
	default:
		_ = r.enc.Encode(je)
		return
	}
	// Ignore encode errors — nothing useful to do with them at render time.
	_ = r.enc.Encode(je)
}

// Close is a no-op for JSONRenderer.
func (r *JSONRenderer) Close() {}
