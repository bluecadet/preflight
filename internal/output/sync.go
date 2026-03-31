package output

import "sync"

type synchronizedRenderer struct {
	mu sync.Mutex
	r  Renderer
}

// Synchronized wraps a renderer with a mutex so concurrent host workers can
// emit output safely without interleaving writes inside renderer internals.
func Synchronized(r Renderer) Renderer {
	if r == nil {
		return nil
	}
	return &synchronizedRenderer{r: r}
}

func (r *synchronizedRenderer) Emit(event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.r.Emit(event)
}

func (r *synchronizedRenderer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.r.Close()
}
