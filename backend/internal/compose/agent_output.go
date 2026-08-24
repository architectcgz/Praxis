package compose

import (
	"sync"

	"praxis/internal/agentruntime"
)

// agentOutputPublisher fans out transient provider output without giving it
// durable ownership. Callers must query the transcript after settlement.
type agentOutputPublisher struct {
	mu        sync.RWMutex
	nextID    uint64
	observers map[uint64]agentruntime.AgentOutputObserver
}

func newAgentOutputPublisher() *agentOutputPublisher {
	return &agentOutputPublisher{observers: make(map[uint64]agentruntime.AgentOutputObserver)}
}

func (p *agentOutputPublisher) Subscribe(observer agentruntime.AgentOutputObserver) func() {
	if p == nil || observer == nil {
		return func() {}
	}
	p.mu.Lock()
	p.nextID++
	id := p.nextID
	p.observers[id] = observer
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		delete(p.observers, id)
		p.mu.Unlock()
	}
}

func (p *agentOutputPublisher) Publish(event agentruntime.AgentOutputEvent) {
	if p == nil || event.AgentID == "" || event.ExecutionID == "" {
		return
	}
	p.mu.RLock()
	observers := make([]agentruntime.AgentOutputObserver, 0, len(p.observers))
	for _, observer := range p.observers {
		observers = append(observers, observer)
	}
	p.mu.RUnlock()
	for _, observer := range observers {
		observer(event)
	}
}
