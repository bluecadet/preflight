package output

import (
	"strings"
	"sync"
)

// Bus is a fan-out emitter that broadcasts events to multiple sinks.
// It implements the Renderer interface and manages run-level metadata.
// Secrets set via Scrub are redacted from text fields before fan-out.
type Bus struct {
	sinks   []Renderer
	mu      sync.Mutex
	secrets []string
}

// NewBus creates a Bus that fans out events to the given sinks in order.
func NewBus(sinks ...Renderer) *Bus {
	return &Bus{sinks: sinks}
}

// AddSink appends a sink to the fan-out list.
func (b *Bus) AddSink(sink Renderer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sinks = append(b.sinks, sink)
}

// Scrub stores secret values that will be redacted from event text fields.
func (b *Bus) Scrub(secrets []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.secrets = append(b.secrets, secrets...)
}

// Emit broadcasts the event to all sinks sequentially.
// Secrets set via Scrub are redacted from text fields before fan-out.
func (b *Bus) Emit(event Event) {
	b.mu.Lock()
	sinks := append([]Renderer(nil), b.sinks...)
	secrets := append([]string(nil), b.secrets...)
	b.mu.Unlock()

	if len(secrets) > 0 {
		event = b.scrubEvent(event, secrets)
	}

	for _, sink := range sinks {
		sink.Emit(event)
	}
}

// scrubEvent returns a copy of the event with secret values redacted
// from all text fields (msg, Lines, Output, Message, FailMessage).
func (b *Bus) scrubEvent(event Event, secrets []string) Event {
	if len(secrets) == 0 {
		return event
	}

	switch e := event.(type) {
	case TaskOutputEvent:
		e.Lines = scrubStrings(e.Lines, secrets)
		return e
	case TaskResultEvent:
		e.Message = scrubString(e.Message, secrets)
		e.Output = scrubStrings(e.Output, secrets)
		return e
	case TaskFailedEvent:
		e.FailMessage = scrubString(e.FailMessage, secrets)
		e.Output = scrubStrings(e.Output, secrets)
		return e
	case DiagnosticEvent:
		e.Summary = scrubString(e.Summary, secrets)
		e.Detail = scrubString(e.Detail, secrets)
		return e
	case WarningEvent:
		e.Message = scrubString(e.Message, secrets)
		return e
	case ErrorEvent:
		e.Message = scrubString(e.Message, secrets)
		return e
	default:
		return event
	}
}

func scrubStrings(lines []string, secrets []string) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = scrubString(line, secrets)
	}
	return result
}

func scrubString(s string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" && len(secret) >= 4 {
			s = strings.ReplaceAll(s, secret, "***")
		}
	}
	return s
}

// Close closes all sinks.
func (b *Bus) Close() {
	b.mu.Lock()
	sinks := append([]Renderer(nil), b.sinks...)
	b.mu.Unlock()
	for _, sink := range sinks {
		sink.Close()
	}
}
