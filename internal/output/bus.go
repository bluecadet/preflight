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
// from all text and map fields.
func (b *Bus) scrubEvent(event Event, secrets []string) Event {
	if len(secrets) == 0 {
		return event
	}
	return event.Redact(secrets)
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

// deepScrubMap recursively walks a map[string]any and scrubs all string values,
// string elements within slices and nested maps, and string keys.
func deepScrubMap(m map[string]any, secrets []string) map[string]any {
	if len(secrets) == 0 || len(m) == 0 {
		return m
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		scrubbedKey := scrubString(k, secrets)
		switch val := v.(type) {
		case string:
			result[scrubbedKey] = scrubString(val, secrets)
		case map[string]any:
			result[scrubbedKey] = deepScrubMap(val, secrets)
		case []any:
			result[scrubbedKey] = deepScrubSlice(val, secrets)
		default:
			result[scrubbedKey] = v
		}
	}
	return result
}

// deepScrubSlice recursively scrubs string values in a []any.
func deepScrubSlice(s []any, secrets []string) []any {
	result := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case string:
			result[i] = scrubString(val, secrets)
		case map[string]any:
			result[i] = deepScrubMap(val, secrets)
		case []any:
			result[i] = deepScrubSlice(val, secrets)
		default:
			result[i] = v
		}
	}
	return result
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
