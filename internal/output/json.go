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
	Phase        string    `json:"phase,omitempty"`
	TaskID       string    `json:"task_id,omitempty"`
	Task         string    `json:"task,omitempty"`
	Target       string    `json:"target,omitempty"`
	Module       string    `json:"module,omitempty"`
	Stream       string    `json:"stream,omitempty"`
	Line         string    `json:"line,omitempty"`
	Status       string    `json:"status,omitempty"`
	Message      string    `json:"message,omitempty"`
	Error        string    `json:"error,omitempty"`
	TaskTotal    *int      `json:"task_total,omitempty"`
	OKCount      *int      `json:"ok_count,omitempty"`
	ChangedCount *int      `json:"changed_count,omitempty"`
	FailedCount  *int      `json:"failed_count,omitempty"`
	SkippedCount *int      `json:"skipped_count,omitempty"`
	TS           string    `json:"ts"`
}

// JSONRenderer writes newline-delimited JSON events to an io.Writer.
type JSONRenderer struct {
	w       io.Writer
	enc     *json.Encoder
	verbose bool
}

// NewJSONRenderer creates a JSONRenderer.
func NewJSONRenderer(w io.Writer) *JSONRenderer {
	return NewJSONRendererWithOptions(w, Options{})
}

// NewJSONRendererWithOptions creates a JSONRenderer with explicit options.
func NewJSONRendererWithOptions(w io.Writer, options Options) *JSONRenderer {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &JSONRenderer{w: w, enc: enc, verbose: options.Verbose}
}

// Emit serialises the event as a single JSON line.
func (r *JSONRenderer) Emit(event Event) {
	if event.Type == EventTaskLog && !r.verbose {
		return
	}

	je := jsonEvent{
		Type:     event.Type,
		PlayName: event.PlayName,
		Phase:    event.Phase,
		TaskID:   event.TaskID,
		Task:     event.TaskName,
		Target:   event.Target,
		Module:   event.Module,
		Stream:   event.Stream,
		Line:     event.Line,
		Status:   event.Status,
		Message:  event.Message,
		TS:       time.Now().UTC().Format(time.RFC3339),
	}
	if event.Error != nil {
		je.Error = event.Error.Error()
	}
	if event.TaskTotal > 0 {
		total := event.TaskTotal
		je.TaskTotal = &total
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
