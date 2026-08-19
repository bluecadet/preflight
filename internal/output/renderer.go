package output

// Renderer is the interface that all output renderers implement.
//
// Close must return any error encountered while flushing or finalizing
// output. For renderers backed by a file (RunLogSink), a swallowed Close
// error can mean the primary audit artifact (run.json) silently fails to
// write on a disk-full or permission error, so callers must check it.
type Renderer interface {
	Emit(event Event)
	Close() error
}
