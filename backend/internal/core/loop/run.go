package loop

import (
	"context"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/tool"
)

// Dependencies 保存模型请求能力；Provider 和工具目录在执行开始时绑定会话配置。
type Dependencies struct {
	Provider       providerapi.Provider
	Tools          tool.ToolCatalog
	HookDispatcher *hook.Dispatcher
	Emitter        *coreevent.Emitter
	Logf           func(string, ...any)
}

// Runner 提供上下文构建、模型请求和摘要生成能力，不持有 Run 或 Session。
type Runner struct {
	Dependencies
}

func NewRunner(deps Dependencies) Runner {
	return Runner{Dependencies: deps}
}

// Request 接收一次模型响应；断流时同时返回已收到的内容，交由执行服务决定如何记录。
func (r *Runner) Request(ctx context.Context, execution ExecutionParams, deps ExecutionDependencies, request providerapi.Request, kind hook.ModelRequestKind, sequence uint64) (Response, error) {
	return r.callModel(ctx, execution, deps, request, kind, sequence, func(event providerapi.Event) {
		if kind == hook.ModelRequestIteration {
			r.emitModelEvent(execution, request.TurnID, event)
		}
	})
}
