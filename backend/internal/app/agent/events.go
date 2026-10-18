package agent

import (
	"sync"

	agentcontracts "praxis/internal/app/agent/contracts"
)

// SubscribeAgentEvents 订阅瞬时事件；终态后调用方仍应查询持久化事实。
func (a *App) SubscribeAgentEvents(observer func(agentcontracts.AgentEvent)) func() {
	if a == nil || a.events == nil {
		return func() {}
	}
	return a.events.Subscribe(observer)
}

// EventPublisher 扇出应用公开事件，不拥有持久化状态。
type EventPublisher struct {
	mu        sync.RWMutex
	nextID    uint64
	observers map[uint64]func(agentcontracts.AgentEvent)
}

// NewEventPublisher 创建空事件出口，不启动后台线程。
func NewEventPublisher() *EventPublisher {
	return &EventPublisher{observers: make(map[uint64]func(agentcontracts.AgentEvent))}
}

// Subscribe 注册观察者并返回注销函数；空观察者不注册。
func (p *EventPublisher) Subscribe(observer func(agentcontracts.AgentEvent)) func() {
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

// Publish 在锁外通知当前观察者；缺少 Agent 或 Task 身份的事件不发布。
func (p *EventPublisher) Publish(event agentcontracts.AgentEvent) {
	if p == nil || event.AgentID == "" || event.TaskID == "" {
		return
	}
	p.mu.RLock()
	observers := make([]func(agentcontracts.AgentEvent), 0, len(p.observers))
	for _, observer := range p.observers {
		observers = append(observers, observer)
	}
	p.mu.RUnlock()
	for _, observer := range observers {
		observer(event)
	}
}
