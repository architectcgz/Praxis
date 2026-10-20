package hook

import (
	"context"
	"slices"
)

// Dispatcher 持有初始化时注册的 Hook，所有回调均同步执行，不创建 goroutine。
type Dispatcher struct {
	operations []OperationHook
	events     []CommittedHook
	models     []ModelHook
}

// NewDispatcher 复制注册列表；不传 Hook 时返回空分发器，Hook 实例应支持并发执行。
func NewDispatcher(operations ...OperationHook) *Dispatcher {
	return &Dispatcher{operations: slices.Clone(operations)}
}

// Start 按注册顺序启动 Hook 并逐级传播 Context；nil Dispatcher 返回空 Scope。
// ctx 必须非 nil，具体 Hook 必须遵守 OperationHook 的 Context 与生命周期约定。
func (d *Dispatcher) Start(ctx context.Context, operation Operation) (context.Context, Scope) {
	if d == nil {
		return ctx, compositeScope{}
	}
	scopes := make([]Scope, 0, len(d.operations))
	for _, hook := range d.operations {
		nextCtx, scope := hook.Start(ctx, operation)
		ctx = nextCtx
		if scope != nil {
			scopes = append(scopes, scope)
		}
	}
	return ctx, compositeScope{scopes: scopes}
}

type compositeScope struct {
	scopes []Scope
}

func (s compositeScope) OnSignal(signal Signal) {
	for _, scope := range s.scopes {
		scope.OnSignal(signal)
	}
}

func (s compositeScope) End(result Result) {
	for i := len(s.scopes) - 1; i >= 0; i-- {
		s.scopes[i].End(result)
	}
}
