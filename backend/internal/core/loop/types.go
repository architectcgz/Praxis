package loop

import (
	"context"
	"praxis/internal/core/agent/execution/timing"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// ToolExecutor 执行已提交的完整调用，完成授权、审批等待和执行后返回已提交回执。
type ToolExecutor interface {
	Execute(context.Context, sessionentry.ToolCall) (sessionentry.ContextEntry, error)
}

type (
	ToolCall                   = providerapi.ToolCall
	ToolDefinition             = tool.ToolDefinition
	ModelContext               = appcontext.ModelContext
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)

// ExecutionOperations 已绑定本次执行；Loop 不接触历史、持久化方式或运行实例。
// NextContext 只在本轮结果提交后调用；PrepareContext 负责将给定上下文准备到可请求状态。
type ExecutionOperations interface {
	NextContext(context.Context) (appcontext.ModelContext, error)
	PrepareContext(context.Context, appcontext.ModelContext, []ToolDefinition, contextcompress.SummarizeFunc) (appcontext.ModelContext, error)
	AppendEntry(context.Context, sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error)
	PublishEntries(identity.TurnID, []sessionentry.ContextEntry)
	NextRequestSequence(context.Context) (uint64, error)
	RecordModelRequest(context.Context, uint64, bool) error
	RecordModelOutput(context.Context, int) error
	EntryFailure(error) error
}

// EndReason 只描述 Loop 为什么返回，不表示业务持久化结果。
type EndReason string

const (
	EndCompleted EndReason = "completed"
	endFailed    EndReason = "failed"
)

type Result struct{ Reason EndReason }

// Input 是业务上层准备好的本次执行数据，不携带服务、回调或持久化能力。
type Input struct {
	ID           identity.AgentExecutionID
	SourceTaskID identity.TaskID
	SessionID    identity.SessionID
	AgentID      identity.AgentID
	RequestID    identity.RequestID
	Model        modelapi.Config
	Context      appcontext.ModelContext
}

// ExecutionDependencies 只在本次 Run 中使用；共享 Runner 不保存单次执行的环境或等待状态。
type ExecutionDependencies struct {
	ToolExecutor ToolExecutor
	Execution    ExecutionOperations
	Timing       *timing.Recorder
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
