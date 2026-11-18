package loop

import (
	"context"
	"errors"
	"fmt"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	entryPayload "praxis/internal/core/entry/payload"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"time"
)

// ToolExecutor 完成授权、审批等待和执行，返回实际结果及后置 Hook 所需的调用信息。
// Loop 提交真实回执后调用后置 Hook，历史写入由条目提交入口负责。
type ToolExecutor interface {
	Execute(context.Context, sessionentry.ToolCall) (toolexecution.CallResult, error)
}

type (
	ToolCall                   = providerapi.ToolCall
	ToolDefinition             = tool.ToolDefinition
	ModelContext               = appcontext.ModelContext
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)

// RunParams 保存一次执行的身份、系统提示词和冻结模型配置，供各轮模型请求共用。
type RunParams struct {
	ID           identity.RunID
	SourceTaskID identity.TaskID
	SessionID    identity.SessionID
	AgentID      identity.AgentID
	SystemPrompt string
	Model        modelapi.Config
}

// AppendEntry 同步提交条目，返回已接纳的只读引用、是否新增及保存错误。
// 保存失败仍可能返回已接纳条目，Loop 不因此重新执行已有副作用的工具。
type AppendEntry = sessionentry.AppendEntry

// RunDependencies 绑定本次 Loop 的历史读取、事实提交和计时能力，不持有执行或会话对象。
type RunDependencies struct {
	AcceptSubagentReports func(context.Context) error
	AcceptInstructions    func(context.Context) (bool, error)
	FinishInstructions    func(context.Context) (bool, error)
	Budget                *appcontext.Budget
	ReadEntries           func(context.Context) ([]*sessionentry.Entry, error)
	AppendEntry           AppendEntry
	ExecuteTools          func(func())
}

type FailureCode string

const (
	FailureProviderUnavailable FailureCode = "provider_unavailable"
	FailureProvider            FailureCode = "provider"
	FailureTool                FailureCode = "tool"
	FailurePolicyBlocked       FailureCode = "policy_blocked"
	FailureResourceLimit       FailureCode = "resource_limit"
	FailureStorage             FailureCode = "storage"
	FailureContract            FailureCode = "contract"
	FailureBusy                FailureCode = "busy"
	FailureInternal            FailureCode = "internal"
)

const (
	ErrBusy             errorcode.Code = "execution.busy"
	ErrContract         errorcode.Code = "execution.contract_error"
	ErrPolicyBlocked    errorcode.Code = "execution.policy_blocked"
	ErrApprovalRequired errorcode.Code = "execution.approval_required"
	ErrStorage          errorcode.Code = "execution.storage_error"
	ErrProvider         errorcode.Code = "execution.provider_error"
	ErrTool             errorcode.Code = "execution.tool_error"
	ErrInterrupted      errorcode.Code = "interrupted"
	ErrResourceLimit    errorcode.Code = "execution.resource_limit"
)

// Failure 保存执行能力边界的失败分类，供执行服务编排和上层结算使用。
type Failure struct {
	code    FailureCode
	cause   error
	message string
}

func (f *Failure) Error() string {
	if f.cause == nil {
		return string(f.code)
	}
	return f.cause.Error()
}

func (f *Failure) Unwrap() error { return f.cause }

func (f *Failure) Message() string { return f.message }

func fail(code FailureCode, operationCode errorcode.Code, message string) error {
	return &Failure{
		code:    code,
		cause:   fmt.Errorf("%s: %w", message, operationCode),
		message: message,
	}
}

// NewFailure 为执行能力边界保留明确的失败分类，供 Loop 和上层结算统一处理。
func NewFailure(code FailureCode, cause error) error {
	return &Failure{
		code:  code,
		cause: cause,
	}
}

// NewModelFailure 保留已有失败分类，将上下文容量错误与模型依赖错误分开。
func NewModelFailure(err error) error {
	if _, ok := errors.AsType[*Failure](err); ok {
		return err
	}
	if _, ok := errors.AsType[*appcontext.ContextWindowExceededError](err); ok {
		return NewFailure(FailureResourceLimit, fmt.Errorf("model context window exceeded: %w", err))
	}
	return NewFailure(FailureProvider, err)
}

// FailureClassification 供业务结算边界读取错误分类。
func FailureClassification(err error) FailureCode {
	if f, ok := errors.AsType[*Failure](err); ok {
		return f.code
	}
	if _, ok := errors.AsType[*appcontext.ContextWindowExceededError](err); ok {
		return FailureResourceLimit
	}
	if errors.Is(err, toolexecution.ErrRiskReviewFailed) {
		return FailureProvider
	}
	return FailureInternal
}

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
			return turnID, NewModelFailure(err)
		}
		turnID = identity.TurnID((identity.SecureIDGenerator{}).New("turn"))
		_, _, err = deps.AppendEntry(ctx, sessionentry.Entry{
			ID:        turnID.String() + ":start",
			RunID:     params.ID.String(),
			TurnID:    turnID.String(),
			CreatedAt: time.Now().UTC(),
			Payload:   entryPayload.TurnStarted{},
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
		endedAt := time.Now().UTC()
		var ending entryPayload.Payload = entryPayload.TurnCompleted{}
		if err != nil {
			ending = entryPayload.TurnFailed{}
			if ctx.Err() != nil {
				ending = entryPayload.TurnInterrupted{}
			}
		}
		_, _, saveErr := deps.AppendEntry(context.WithoutCancel(ctx), sessionentry.Entry{
			ID:        turnID.String() + ":end",
			RunID:     params.ID.String(),
			TurnID:    turnID.String(),
			CreatedAt: endedAt,
			Payload:   ending,
		})
		if saveErr != nil {
			return turnID, NewFailure(FailureStorage, saveErr)
		}
		switch ending.(type) {
		case entryPayload.TurnFailed:
			r.emit(params, turnID, coreevent.TurnFailed{})
		case entryPayload.TurnInterrupted:
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
	identity := runIdentity(params, "")
	identity.OperationID = hook.NewOperationID()
	if hookErr := r.HookDispatcher.AfterContextBuild(context.WithoutCancel(ctx), hook.ContextBuilt{
		Identity: identity,
		Context:  built,
	}); hookErr != nil {
		r.Logf("after context build hook failed run_id=%s: %v", params.ID, hookErr)
	}
	return built
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
