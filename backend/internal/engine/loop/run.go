package loop

import (
	"context"
	appcontext "praxis/internal/context"
	"praxis/internal/engine/hook"
	"praxis/internal/engine/modelcall"
	"praxis/internal/engine/toolexecution"
	"praxis/internal/entry"
	"praxis/internal/errorcode"
	coreevent "praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/model"
	"praxis/internal/provider"
	"praxis/internal/tool"
	"time"
)

// ToolExecutor 完成授权、执行与输出边界控制，返回实际结果及后置 Hook 所需的调用信息。
// Loop 提交真实回执后调用后置 Hook，历史写入由条目提交入口负责。
type ToolExecutor interface {
	Execute(context.Context, entry.ToolCall, entry.AppendEntry) (toolexecution.CallResult, error)
	SpillResult(tool.ToolResult) (tool.ToolResult, error)
}

// RunParams 保存一次执行的身份、系统提示词、工具目录与冻结模型配置，供各轮模型请求共用。
type RunParams struct {
	ID           identity.RunID
	SourceTaskID identity.TaskID
	SessionID    identity.SessionID
	AgentID      identity.AgentID
	SystemPrompt string
	Model        model.Config
	Tools        tool.ToolCatalog
}

// RunDependencies 绑定本次 Loop 的历史读取、事实提交和计时能力，不持有执行或会话对象。
type RunDependencies struct {
	AcceptSubagentReports func(context.Context) error
	AcceptInstructions    func(context.Context) (bool, error)
	FinishInstructions    func(context.Context) (bool, error)
	Budget                *appcontext.Budget
	ReadEntries           func(context.Context) ([]*entry.Entry, error)
	AppendEntry           entry.AppendEntry
	ExecuteTools          func(appendEntry entry.AppendEntry, execute func(submitEntry entry.AppendEntry))
}

// Dependencies 保存 Loop 的共享能力；Provider 在执行开始时绑定会话配置。
type Dependencies struct {
	CommitTimeout        time.Duration
	CompressionPolicy    appcontext.CompressionPolicy
	SummaryTimeout       time.Duration
	ToolResultOverhead   int
	KeepToolResultTokens int
	ReadOnlyParallelism  int
	Provider             provider.Provider
	ModelCaller          *modelcall.Caller
	HookDispatcher       *hook.Dispatcher
	Emitter              *coreevent.Emitter
	Logf                 func(string, ...any)
}

// Runner 负责 ReAct 多轮迭代；模型上下文只用于本轮请求，不持有执行或会话对象。
type Runner struct {
	Dependencies
}

func NewRunner(deps Dependencies) Runner {
	return Runner{Dependencies: deps}
}

func (r *Runner) commitContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), r.CommitTimeout)
}

// Run 每轮读取 Session 已接纳的历史，逐条提交模型输出和工具回执，直到最终回答或失败。
// 返回最后一次 Turn 身份，供执行服务关联失败展示；Run 结算由调用方负责。
func (r *Runner) Run(ctx context.Context, params RunParams, deps RunDependencies, input *entry.Entry, executor ToolExecutor) (turnID identity.TurnID, err error) {
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
		_, _, err = deps.AppendEntry(ctx, entry.Entry{
			ID:        identity.NewEntryID(),
			RunID:     params.ID,
			TurnID:    turnID,
			CreatedAt: time.Now().UTC(),
			Payload:   entry.TurnStarted{},
		})
		if err != nil {
			return turnID, err
		}
		request := provider.Request{
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
		var ending entry.Payload = entry.TurnCompleted{}
		if err != nil {
			ending = entry.TurnFailed{}
			if ctx.Err() != nil {
				ending = entry.TurnInterrupted{}
			}
		}
		commitCtx, cancelCommit := r.commitContext(ctx)
		_, _, saveErr := deps.AppendEntry(commitCtx, entry.Entry{
			ID:        identity.NewEntryID(),
			RunID:     params.ID,
			TurnID:    turnID,
			CreatedAt: endedAt,
			Payload:   ending,
		})
		cancelCommit()
		if saveErr != nil {
			return turnID, errorcode.ErrStorage.Wrap(saveErr)
		}
		switch ending.(type) {
		case entry.TurnFailed:
			r.emit(params, turnID, coreevent.TurnFailed{})
		case entry.TurnInterrupted:
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
func (r *Runner) BuildContext(ctx context.Context, params RunParams, entries []*entry.Entry) appcontext.ModelContext {
	built := appcontext.ModelContextFromEntries(params.SystemPrompt, entries)
	hookIdentity := runIdentity(params, "")
	hookIdentity.OperationID = identity.NewOperationID()
	commitCtx, cancel := r.commitContext(ctx)
	defer cancel()
	if hookErr := r.HookDispatcher.AfterContextBuild(commitCtx, hook.ContextBuilt{
		Identity: hookIdentity,
		Context:  built,
	}); hookErr != nil {
		r.Logf("after context build hook failed run_id=%s: %v", params.ID, hookErr)
	}
	return built
}

// Request 通过统一底层模型请求组件接收一次完整模型响应。
func (r *Runner) Request(ctx context.Context, params RunParams, deps RunDependencies, request provider.Request, kind entry.ModelRequestKind) (modelcall.Response, error) {
	call := modelcall.Call{
		Provider:     r.Provider,
		Request:      request,
		Kind:         kind,
		SessionID:    params.SessionID,
		AgentID:      params.AgentID,
		SourceTaskID: params.SourceTaskID,
		AllowTools:   kind == entry.ModelRequestIteration,
		AppendEntry:  deps.AppendEntry,
	}
	if kind == entry.ModelRequestIteration {
		call.Budget = deps.Budget
		call.Prepare = func(req provider.Request) (provider.Request, error) {
			return r.remindContextBudget(req, deps.Budget)
		}
		call.OnProviderEvent = func(event provider.Event) {
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

func (r *Runner) emitModelEvent(params RunParams, turnID identity.TurnID, event provider.Event) {
	switch event.Kind {
	case provider.StreamTextDelta:
		r.emit(params, turnID, coreevent.TextDelta{Text: event.Text})
	case provider.StreamThinkingDelta:
		r.emit(params, turnID, coreevent.ThinkingDelta{Text: event.Text})
	}
}
