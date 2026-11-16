package loop

import (
	"context"
	"errors"
	"fmt"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"time"
)

// Dependencies 保存 Loop 的共享能力；Provider 和工具目录在执行开始时绑定会话配置。
type Dependencies struct {
	Provider       providerapi.Provider
	Tools          tool.ToolCatalog
	HookDispatcher *hook.Dispatcher
	Emitter        *coreevent.Emitter
	Logf           func(string, ...any)
}

// Runner 负责 ReAct 多轮迭代；模型上下文只用于本轮请求，不持有执行或会话对象。
type Runner struct {
	Dependencies
}

func NewRunner(deps Dependencies) Runner {
	return Runner{Dependencies: deps}
}

// Run 每轮读取 Session 已接纳的历史，逐条提交模型输出和工具回执，直到最终回答或失败。
// 返回最后一次 Turn 身份，供执行服务关联失败展示；Run 结算由调用方负责。
func (r *Runner) Run(ctx context.Context, params RunParams, deps RunDependencies, input *sessionentry.ContextEntry, executor ToolExecutor) (turnID identity.TurnID, err error) {
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
			return turnID, NewModelFailure(err)
		}
		turnID = identity.TurnID((identity.SecureIDGenerator{}).New("turn"))
		turn := sessionentry.TurnRecord{ID: turnID, RunID: params.ID, StartedAt: time.Now().UTC()}
		_, _, err = deps.AppendEntry(ctx, sessionentry.ContextEntry{
			ID:      turnID.String() + ":start",
			RunID:   params.ID.String(),
			TurnID:  turnID.String(),
			Payload: sessionentry.TurnStarted{Turn: turn},
		})
		if err != nil {
			return turnID, err
		}
		request := providerapi.Request{
			ID:              identity.NewModelRequestID(),
			RunID:           params.ID,
			PromptCacheKey:  params.SessionID.String(),
			Model:           params.Model,
			Context:         modelContext,
			Tools:           r.Tools.List(),
			MaxOutputTokens: params.Model.MaxOutputTokens,
			TurnID:          turnID,
		}
		r.emit(params, turnID, coreevent.TurnStarted{})
		finished, err := r.runTurn(ctx, params, deps, executor, request)
		turn.EndedAt = time.Now().UTC()
		var ending sessionentry.Payload = sessionentry.TurnCompleted{Turn: turn}
		if err != nil {
			ending = sessionentry.TurnFailed{Turn: turn}
			if ctx.Err() != nil {
				ending = sessionentry.TurnInterrupted{Turn: turn}
			}
		}
		_, _, saveErr := deps.AppendEntry(context.WithoutCancel(ctx), sessionentry.ContextEntry{
			ID:      turnID.String() + ":end",
			RunID:   params.ID.String(),
			TurnID:  turnID.String(),
			Payload: ending,
		})
		if saveErr != nil {
			return turnID, NewFailure(FailureStorage, saveErr)
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

// Request 接收一次模型响应；断流时同时返回已收到内容，由所属调用流程决定如何记录。
func (r *Runner) Request(ctx context.Context, params RunParams, deps RunDependencies, request providerapi.Request, kind hook.ModelRequestKind) (Response, error) {
	var response Response
	var err error
	for retry := 0; retry <= 3; retry++ {
		if retry > 0 {
			request.ID = identity.NewModelRequestID()
		}
		response, err = r.callModel(ctx, params, deps, request, kind, retry, func(event providerapi.Event) {
			if kind == hook.ModelRequestIteration {
				r.emitModelEvent(params, request.TurnID, event)
			}
		})
		if err == nil {
			return response, nil
		}
		r.emit(params, request.TurnID, coreevent.ErrorOccurred{Message: fmt.Sprintf("Model request failed (attempt %d/4): %v", retry+1, err)})
		if ctx.Err() != nil {
			return response, err
		}
		if failure, ok := errors.AsType[*Failure](err); ok && failure.code != FailureProvider {
			return response, err
		}
	}
	return response, err
}
