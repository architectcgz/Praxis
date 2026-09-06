package orchestration

import "sync"

// ReadinessGate controls durable command admission while recovery converges.
type ReadinessGate struct {
	mu    sync.RWMutex
	ready bool
}

// NewReadinessGate creates a concurrency-safe startup admission gate.
func NewReadinessGate(initiallyReady bool) *ReadinessGate {
	return &ReadinessGate{ready: initiallyReady}
}

// SetReady opens or closes command admission without changing durable state.
func (g *ReadinessGate) SetReady(ready bool) {
	g.mu.Lock()
	g.ready = ready
	g.mu.Unlock()
}

// Ready reports whether new durable commands may be admitted.
func (g *ReadinessGate) Ready() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.ready
}
