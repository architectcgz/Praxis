package loop

import (
	"context"
	appcontext "praxis/internal/core/context"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// Dependencies 保存 Loop 的共享能力；Provider 和工具目录在执行开始时绑定会话配置。
type Dependencies struct {
	Provider       providerapi.Provider
	Tools          tool.ToolCatalog
	HookDispatcher *hook.Dispatcher
	Emitter        *coreevent.Emitter
	Logf           func(string, ...any)
}

// Runner 负责 ReAct 多轮迭代；本次上下文仅在 Run 调用期间存在，不持有执行或会话对象。
type Runner struct {
	Dependencies
}

func NewRunner(deps Dependencies) Runner {
	return Runner{Dependencies: deps}
}

// Run 从初始历史构建本地上下文，逐条提交模型输出和工具回执，直到最终回答或失败。
// 返回最后一次 Turn 身份，供执行服务关联失败展示；Run 结算由调用方负责。
func (r *Runner) Run(ctx context.Context, params RunParams, deps RunDependencies, input Input, executor ToolExecutor) (turnID identity.TurnID, err error) {
	current := appcontext.BuildContext(input.Entries)
	appendEntry := deps.AppendEntry
	deps.AppendEntry = func(ctx context.Context, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
		accepted, inserted, err := appendEntry(ctx, value)
		if inserted {
			current.Append(accepted)
		}
		return accepted, inserted, err
	}
	for {
		content := appcontext.ContentFromEntries(input.SystemPrompt, input.InputEntryID, current.Entries())
		modelContext, err := r.BuildContext(ctx, params, appcontext.ModelContext{
			SystemPrompt: content.SystemPrompt,
			Entries:      content.Entries,
		})
		if err != nil {
			return turnID, NewModelFailure(err)
		}
		sequence, err := deps.Progress.NextRequestSequence(ctx)
		if err != nil {
			return turnID, err
		}
		turnID = identity.NewTurnID(params.ID, sequence)
		request := providerapi.Request{
			RunID:           params.ID,
			PromptCacheKey:  params.SessionID.String(),
			Model:           params.Model.Clone(),
			Context:         modelContext,
			Tools:           r.Tools.List(),
			MaxOutputTokens: params.Model.MaxOutputTokens,
			TurnID:          turnID,
		}
		finished, err := r.runTurn(ctx, params, deps, executor, request, sequence)
		if err != nil || finished {
			return turnID, err
		}
	}
}

// Request 接收一次模型响应；断流时同时返回已收到内容，由所属调用流程决定如何记录。
func (r *Runner) Request(ctx context.Context, params RunParams, deps RunDependencies, request providerapi.Request, kind hook.ModelRequestKind, sequence uint64) (Response, error) {
	return r.callModel(ctx, params, deps, request, kind, sequence, func(event providerapi.Event) {
		if kind == hook.ModelRequestIteration {
			r.emitModelEvent(params, request.TurnID, event)
		}
	})
}
