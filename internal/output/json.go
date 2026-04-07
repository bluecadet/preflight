package output

import (
	"encoding/json"
	"io"
	"time"
)

// jsonEvent is the serializable form of an Event.
type jsonEvent struct {
	Type         EventType         `json:"type"`
	PlayName     string            `json:"play,omitempty"`
	TaskID       string            `json:"task_id,omitempty"`
	Task         string            `json:"task,omitempty"`
	Target       string            `json:"target,omitempty"`
	Status       string            `json:"status,omitempty"`
	Message      string            `json:"message,omitempty"`
	Error        string            `json:"error,omitempty"`
	OKCount      *int              `json:"ok_count,omitempty"`
	ChangedCount *int              `json:"changed_count,omitempty"`
	FailedCount  *int              `json:"failed_count,omitempty"`
	SkippedCount *int              `json:"skipped_count,omitempty"`
	Lines        []string          `json:"lines,omitempty"`
	Output       []string          `json:"output,omitempty"`
	Facts        map[string]any    `json:"facts,omitempty"`
	Tasks        []PlanTaskEntry   `json:"tasks,omitempty"`
	StatePath    string            `json:"state_path,omitempty"`
	LastApplied  string            `json:"last_applied,omitempty"`
	Comparisons  []StateComparison `json:"comparisons,omitempty"`
	TS           string            `json:"ts"`
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
	}

	// Ignore encode errors — nothing useful to do with them at render time.
	_ = r.enc.Encode(je)
}

// Close is a no-op for JSONRenderer.
func (r *JSONRenderer) Close() {}
