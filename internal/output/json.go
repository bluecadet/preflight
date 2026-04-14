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

	p, ok := projectEvent(event)
	if !ok {
		_ = r.enc.Encode(je)
		return
	}

	je.Type = p.kind
	je.PlayName = p.playName
	je.Name = p.name
	je.Namespace = p.namespace
	je.Ref = p.ref
	je.TaskID = p.taskID
	je.Task = p.task
	je.Target = p.target
	je.Status = p.status
	je.Message = p.message
	je.Error = p.errorMessage
	je.TaskCount = p.taskCount
	if p.kind == EventPlayEnd {
		okCount := p.okCount
		changedCount := p.changedCount
		failedCount := p.failedCount
		skippedCount := p.skippedCount
		je.OKCount = &okCount
		je.ChangedCount = &changedCount
		je.FailedCount = &failedCount
		je.SkippedCount = &skippedCount
	}
	je.Lines = p.lines
	je.Output = p.output
	je.Facts = p.facts
	je.Tasks = p.tasks
	je.StatePath = p.statePath
	je.LastApplied = p.lastApplied
	je.Comparisons = p.comparisons
	je.PlaybookPath = p.playbookPath
	je.VisitedRefs = p.visitedRefs
	je.ResolvedRefs = p.resolvedRefs
	je.ErrorCount = p.errorCount
	je.EmbeddedRefs = p.embeddedRefs
	je.LocalDir = p.localDir
	je.LocalRefs = p.localRefs
	je.Version = p.version
	je.Description = p.description
	je.Author = p.author
	je.Inputs = p.inputs
	je.TaskNames = p.taskNames
	je.Entries = p.entries
	je.Plugins = p.plugins
	je.Hosts = p.hosts
	je.Secrets = p.secrets

	// Ignore encode errors — nothing useful to do with them at render time.
	_ = r.enc.Encode(je)
}

// Close is a no-op for JSONRenderer.
func (r *JSONRenderer) Close() {}
