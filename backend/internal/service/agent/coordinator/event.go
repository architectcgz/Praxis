package coordinator

import (
	"encoding/json"
	"praxis/internal/core/model"
	timingmodel "praxis/internal/core/timing"
	"praxis/internal/identity"
	"sync"
)

const EventToolApprovalRequired = "tool_approval_required"

// Event 是进程内执行通知，持久化事实仍由业务查询读取。
type Event struct {
	Kind           string
	SessionID      identity.SessionID
	AgentID        identity.AgentID
	ExecutionID    identity.AgentExecutionID
	Outcome        string
	FailureCode    string
	FailureMessage string
	TurnID         identity.TurnID
	Text           string
	CallID         string
	Name           string
	Input          json.RawMessage
	Result         string
	IsError        bool
	Error          string
	Timing         *timingmodel.Record
	Usage          *model.ModelUsage
}

// EventPublisher 只保存进程级订阅，不缓存任务事件或持久化消息。
type EventPublisher struct {
	mu        sync.RWMutex
	nextID    uint64
	observers map[uint64]func(Event)
}

func NewEventPublisher() *EventPublisher {
	return &EventPublisher{observers: make(map[uint64]func(Event))}
}

func (p *EventPublisher) SubscribeAgentEvents(observer func(Event)) func() {
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

// Publish 在锁外同步通知当前订阅者，观察者不得修改事件中的切片与指针。
func (p *EventPublisher) Publish(event Event) {
	if p == nil || event.AgentID == "" || event.ExecutionID == "" {
		return
	}
	p.mu.RLock()
	observers := make([]func(Event), 0, len(p.observers))
	for _, observer := range p.observers {
		observers = append(observers, observer)
	}
	p.mu.RUnlock()
	for _, observer := range observers {
		observer(event)
	}
}
