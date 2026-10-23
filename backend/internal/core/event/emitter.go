package event

import (
	"slices"
	"sync"
)

type subscription struct {
	id      uint64
	observe func(Event)
}

// Emitter 管理进程内订阅，不缓存事件或持久化业务事实，零值即可使用。
type Emitter struct {
	mu            sync.RWMutex
	nextID        uint64
	subscriptions []subscription
}

func NewEmitter() *Emitter { return &Emitter{} }

// Subscribe 按注册顺序订阅事件，返回可重复调用的取消订阅函数。
// 取消订阅不撤回已经取得订阅快照的通知。
func (e *Emitter) Subscribe(observe func(Event)) func() {
	if observe == nil {
		return func() {}
	}
	e.mu.Lock()
	e.nextID++
	id := e.nextID
	e.subscriptions = append(e.subscriptions, subscription{id: id, observe: observe})
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		e.subscriptions = slices.DeleteFunc(e.subscriptions, func(value subscription) bool { return value.id == id })
		e.mu.Unlock()
	}
}

// Emit 在锁外按注册顺序同步通知订阅者；订阅者不得修改事件中的切片与指针。
// 并发 Emit 不保证全局顺序，耗时操作会阻塞本次发布。
func (e *Emitter) Emit(value Event) {
	e.mu.RLock()
	subscriptions := slices.Clone(e.subscriptions)
	e.mu.RUnlock()
	for _, subscriber := range subscriptions {
		subscriber.observe(value)
	}
}
