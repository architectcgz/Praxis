package loop

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
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

// ExecutionProgress 在本次 Run 上预留请求；Main 先保存，子执行仅更新内存。
type ExecutionProgress interface {
	NextRequestSequence(context.Context, *agentexecution.Run) (uint64, error)
	RecordModelRequest(context.Context, *agentexecution.Run, uint64, bool) error
	RecordModelOutput(context.Context, *agentexecution.Run, int) error
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
	Run          *agentexecution.Run
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
