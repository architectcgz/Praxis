package loop

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	appcontext "praxis/internal/core/context"
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

// ExecutionProgress 由 Execution Service 原生提供请求预留与输出记录，不承担执行恢复。
// 风险审核也会消耗序号，每次请求都读取最新值，不在 Loop 缓存进度。
type ExecutionProgress interface {
	NextRequestSequence(context.Context, identity.AgentExecutionID) (uint64, error)
	RecordModelRequest(context.Context, identity.AgentExecutionID, uint64, bool) error
	RecordModelOutput(context.Context, identity.AgentExecutionID, int) error
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
	Config       Config
	Context      *appcontext.ModelContext
}

// ExecutionDependencies 只在本次 Run 中使用；共享 Runner 不保存单次执行的环境或等待状态。
type ExecutionDependencies struct {
	ToolExecutor ToolExecutor
	Assistant    AssistantCommitter
}

type Config struct {
	Model modelapi.Config
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

// AssistantWrite 是一次输出提交的输入；FinalOutput 用于校验，完成证据以消息为准。
type AssistantWrite struct {
	Entry       sessionentry.ContextEntry
	FinalOutput *agentmodel.FinalOutput
}

// AssistantCommitter 是绑定本次执行的输出提交能力，由 Execution Service 实现。
type AssistantCommitter interface {
	AppendAssistant(context.Context, AssistantWrite) (sessionentry.ContextEntry, bool, error)
	AppendSummary(context.Context, sessionentry.ContextEntry) (sessionentry.ContextEntry, error)
}
