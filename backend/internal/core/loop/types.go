package loop

import (
	"context"
	"praxis/internal/core/agent/execution/timing"
	appcontext "praxis/internal/core/context"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
)

// ToolExecutor 完成授权、审批等待和执行，返回实际结果及后置 Hook 所需的调用信息。
// 结果保存和后置 Hook 调度由 Loop 负责。
type ToolExecutor interface {
	Execute(context.Context, sessionentry.ToolCall) (toolexecution.CallResult, error)
}

type (
	ToolCall                   = providerapi.ToolCall
	ToolDefinition             = tool.ToolDefinition
	ModelContext               = appcontext.ModelContext
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)

// HistoryOperations 绑定本轮所属的历史能力：主执行使用 Session，子执行使用私有历史。
// NextContext 只在本轮条目追加后调用；Loop 不持有原始历史或存储实现。
type HistoryOperations interface {
	NextContext(context.Context) (appcontext.ModelContext, error)
	AppendEntry(context.Context, sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error)
	UpdateToolResult(context.Context, string, string) (sessionentry.ContextEntry, error)
	ReplaceContext(context.Context, []sessionentry.ContextEntry) error
	PublishEntries(identity.TurnID, []sessionentry.ContextEntry)
	EntryFailure(error) error
}

// ExecutionOperations 只记录本次执行的请求序号和模型消耗，不承担历史读写。
type ExecutionOperations interface {
	NextRequestSequence(context.Context) (uint64, error)
	RecordModelRequest(context.Context, uint64, bool) error
	RecordModelOutput(context.Context, int) error
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
	History      HistoryOperations
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
