package hook

import (
	"context"
	"errors"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/identity"
)

// CommittedEvent 只通知已经可靠提交的事实，供记忆或审计订阅。
// 由业务调用方在事务成功返回后发布；通知为 best-effort，不保证跨重启必达或并发全局顺序。
type CommittedEvent struct {
	Identity
	Message   *messagemodel.Message
	Execution *Execution
}
type CommittedHook interface {
	OnCommitted(context.Context, CommittedEvent) error
}

// NewDispatcherWithEvents 冻结操作 Hook 和提交后 Hook 的注册列表。
func NewDispatcherWithEvents(operations []OperationHook, events ...CommittedHook) *Dispatcher {
	dispatcher := NewDispatcher(operations...)
	dispatcher.events = append([]CommittedHook(nil), events...)
	return dispatcher
}

// OnCommitted 收集扩展错误，调用方只能记录错误，不能撤销已提交事务。
func (d *Dispatcher) OnCommitted(ctx context.Context, event CommittedEvent) error {
	if d == nil {
		return nil
	}
	var errs []error
	for _, hook := range d.events {
		if err := hook.OnCommitted(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) HasCommittedHooks() bool { return d != nil && len(d.events) > 0 }

// Execution 是提交后观察所需的最小执行事实，不暴露可变业务实体。
type Execution struct {
	ID          identity.AgentExecutionID
	Outcome     string
	FailureCode string
}
