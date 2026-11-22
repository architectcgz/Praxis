package loop

import (
	"context"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/modelcall"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"time"
)

// ToolExecutor 完成授权、执行与输出边界控制，返回实际结果及后置 Hook 所需的调用信息。
// Loop 提交真实回执后调用后置 Hook，历史写入由条目提交入口负责。
type ToolExecutor interface {
	Execute(context.Context, sessionentry.ToolCall, sessionentry.AppendEntry) (toolexecution.CallResult, error)
	BoundResult(tool.ToolResult, int) (tool.ToolResult, error)
}

// RunParams 保存一次执行的身份、系统提示词、工具目录与冻结模型配置，供各轮模型请求共用。
type RunParams struct {
	ID           identity.RunID
	SourceTaskID identity.TaskID
	SessionID    identity.SessionID
	AgentID      identity.AgentID
	SystemPrompt string
	Model        modelapi.Config
	Tools        tool.ToolCatalog
}

// RunDependencies 绑定本次 Loop 的历史读取、事实提交和计时能力，不持有执行或会话对象。
type RunDependencies struct {
	AcceptSubagentReports func(context.Context) error
	AcceptInstructions    func(context.Context) (bool, error)
	FinishInstructions    func(context.Context) (bool, error)
	Budget                *appcontext.Budget
	ReadEntries           func(context.Context) ([]*sessionentry.Entry, error)
	AppendEntry           sessionentry.AppendEntry
	ExecuteTools          func(appendEntry sessionentry.AppendEntry, execute func(submitEntry sessionentry.AppendEntry))
}

// Dependencies 保存 Loop 的共享能力；Provider 在执行开始时绑定会话配置。
type Dependencies struct {
	Provider       providerapi.Provider
	ModelCaller    *modelcall.Caller
	HookDispatcher *hook.Dispatcher
	Emitter        *coreevent.Emitter
	Logf           func(string, ...any)
}

// Runner 负责 ReAct 多轮迭代；模型上下文只用于本轮请求，不持有执行或会话对象。
type Runner struct {
	Dependencies
}

func NewRunner(deps Dependencies) Runner {
	if deps.ModelCaller == nil {
		deps.ModelCaller = modelcall.NewCaller(modelcall.Dependencies{
			HookDispatcher: deps.HookDispatcher,
			Emitter:        deps.Emitter,
			Logf:           deps.Logf,
		})
	}
	return Runner{Dependencies: deps}
}

// Run 每轮读取 Session 已接纳的历史，逐条提交模型输出和工具回执，直到最终回答或失败。
// 返回最后一次 Turn 身份，供执行服务关联失败展示；Run 结算由调用方负责。
func (r *Runner) Run(ctx context.Context, params RunParams, deps RunDependencies, input *sessionentry.Entry, executor ToolExecutor) (turnID identity.TurnID, err error) {
	for {
		if _, err := deps.AcceptInstructions(ctx); err != nil {
			return turnID, err
		}
		if err := deps.AcceptSubagentReports(ctx); err != nil {
			return turnID, err
		}
		entries, err := deps.ReadEntries(ctx)
		if err != nil {
			return turnID, err
		}
		modelContext := r.BuildContext(ctx, params, entries)
		modelContext, err = r.Compress(ctx, params, deps, input, modelContext)
		if err != nil {
			return turnID, modelcall.WrapModelError(err)
		}
		turnID = identity.NewTurnID()
		_, _, err = deps.AppendEntry(ctx, sessionentry.Entry{
			ID:        identity.NewEntryID(),
			RunID:     params.ID,
			TurnID:    turnID,
			CreatedAt: time.Now().UTC(),
			Payload:   sessionentry.TurnStarted{},
		})
		if err != nil {
			return turnID, err
		}
		request := providerapi.Request{
			RunID:           params.ID,
			PromptCacheKey:  params.SessionID.String(),
			Model:           params.Model,
			Context:         modelContext,
			Tools:           params.Tools.List(),
			MaxOutputTokens: params.Model.MaxOutputTokens,
			TurnID:          turnID,
		}
		r.emit(params, turnID, coreevent.TurnStarted{})
		finished, err := r.runTurn(ctx, params, deps, executor, request)
		endedAt := time.Now().UTC()
		var ending sessionentry.Payload = sessionentry.TurnCompleted{}
		if err != nil {
			ending = sessionentry.TurnFailed{}
			if ctx.Err() != nil {
				ending = sessionentry.TurnInterrupted{}
			}
		}
		_, _, saveErr := deps.AppendEntry(context.WithoutCancel(ctx), sessionentry.Entry{
			ID:        identity.NewEntryID(),
			RunID:     params.ID,
			TurnID:    turnID,
			CreatedAt: endedAt,
			Payload:   ending,
		})
		if saveErr != nil {
			return turnID, errorcode.ErrStorage.Wrap(saveErr)
		}
		switch ending.(type) {
		case sessionentry.TurnFailed:
			r.emit(params, turnID, coreevent.TurnFailed{})
		case sessionentry.TurnInterrupted:
			r.emit(params, turnID, coreevent.TurnInterrupted{})
		default:
			r.emit(params, turnID, coreevent.TurnCompleted{})
		}
		if finished && err == nil {
			delivered, acceptErr := deps.FinishInstructions(ctx)
			if acceptErr != nil {
				return turnID, acceptErr
			}
			finished = !delivered
		}
		if err != nil || finished {
			return turnID, err
		}
	}
}

// BuildContext 只选择本轮所需的 Entry 引用；Hook 只读观察，容量评估在构建完成后执行。
func (r *Runner) BuildContext(ctx context.Context, params RunParams, entries []*sessionentry.Entry) appcontext.ModelContext {
	built := appcontext.ModelContextFromEntries(params.SystemPrompt, entries)
	hookIdentity := runIdentity(params, "")
	hookIdentity.OperationID = identity.NewOperationID()
	if hookErr := r.HookDispatcher.AfterContextBuild(context.WithoutCancel(ctx), hook.ContextBuilt{
		Identity: hookIdentity,
		Context:  built,
	}); hookErr != nil {
		r.Logf("after context build hook failed run_id=%s: %v", params.ID, hookErr)
	}
	return built
}

// Request 通过统一底层模型请求组件接收一次完整模型响应。
func (r *Runner) Request(ctx context.Context, params RunParams, deps RunDependencies, request providerapi.Request, kind sessionentry.ModelRequestKind) (modelcall.Response, error) {
	call := modelcall.Call{
		Provider:     r.Provider,
		Request:      request,
		Kind:         kind,
		SessionID:    params.SessionID,
		AgentID:      params.AgentID,
		SourceTaskID: params.SourceTaskID,
		AllowTools:   kind == sessionentry.ModelRequestIteration,
		AppendEntry:  deps.AppendEntry,
	}
	if kind == sessionentry.ModelRequestIteration {
		call.Budget = deps.Budget
		call.Prepare = func(req providerapi.Request) (providerapi.Request, error) {
			return remindContextBudget(req, deps.Budget)
		}
		call.OnEvent = func(event providerapi.Event) {
			r.emitModelEvent(params, request.TurnID, event)
		}
	}
	return r.ModelCaller.Do(ctx, call)
}

func runIdentity(params RunParams, turnID identity.TurnID) hook.Identity {
	return hook.Identity{
		SourceTaskID: params.SourceTaskID,
		SessionID:    params.SessionID,
		AgentID:      params.AgentID,
		RunID:        params.ID,
		TurnID:       turnID,
	}
}

func (r *Runner) emit(params RunParams, turnID identity.TurnID, payload coreevent.Payload) {
	r.Emitter.Emit(coreevent.Event{
		Source: coreevent.EventSource{
			SessionID: params.SessionID,
			AgentID:   params.AgentID,
			RunID:     params.ID,
			TurnID:    turnID,
		},
		Payload: payload,
	})
}

func (r *Runner) emitModelEvent(params RunParams, turnID identity.TurnID, event providerapi.Event) {
	switch event.Kind {
	case providerapi.StreamTextDelta:
		r.emit(params, turnID, coreevent.TextDelta{Text: event.Text})
	case providerapi.StreamThinkingDelta:
		r.emit(params, turnID, coreevent.ThinkingDelta{Text: event.Text})
	}
}
