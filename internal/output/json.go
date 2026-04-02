package output

import (
	"encoding/json"
	"io"
	"time"
)

// jsonEvent is the serializable form of an Event.
type jsonEvent struct {
	Type         EventType `json:"type"`
	PlayName     string    `json:"play,omitempty"`
	Task         string    `json:"task,omitempty"`
	Module       string    `json:"module,omitempty"`
	Target       string    `json:"target,omitempty"`
	Status       string    `json:"status,omitempty"`
	Message      string    `json:"message,omitempty"`
	TaskIndex    int       `json:"task_index,omitempty"`
	TaskTotal    int       `json:"task_total,omitempty"`
	Error        string    `json:"error,omitempty"`
	OKCount      *int      `json:"ok_count,omitempty"`
	ChangedCount *int      `json:"changed_count,omitempty"`
	FailedCount  *int      `json:"failed_count,omitempty"`
	SkippedCount *int      `json:"skipped_count,omitempty"`
	TS           string    `json:"ts"`
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
	je := jsonEvent{
		Type:      event.Type,
		PlayName:  event.PlayName,
		Task:      event.TaskName,
		Module:    event.Module,
		Target:    event.Target,
		Status:    event.Status,
		Message:   event.Message,
		TaskIndex: event.TaskIndex,
		TaskTotal: event.TaskTotal,
		TS:        time.Now().UTC().Format(time.RFC3339),
	}
	if event.Error != nil {
		je.Error = event.Error.Error()
	}
	if event.Type == EventPlayEnd {
		ok := event.OKCount
		ch := event.ChangedCount
		fa := event.FailedCount
		sk := event.SkippedCount
		je.OKCount = &ok
		je.ChangedCount = &ch
		je.FailedCount = &fa
		je.SkippedCount = &sk
	}
	// Ignore encode errors — nothing useful to do with them at render time.
	_ = r.enc.Encode(je)
}

// Close is a no-op for JSONRenderer.
func (r *JSONRenderer) Close() {}
