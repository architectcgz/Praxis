package loop

import (
	"context"
	appcontext "praxis/internal/core/context"
	messagemodel "praxis/internal/core/message"
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// ToolExecutor 执行已经登记的调用；等待审批时不产生工具回执。
type ToolExecutor interface {
	Execute(context.Context, identity.AgentExecutionID, string) (messagemodel.Message, bool, error)
	Resume(context.Context, identity.AgentExecutionID, string) (messagemodel.Message, bool, error)
}

type (
	ToolCall                   = modelapi.ToolCall
	ToolDefinition             = tool.ToolDefinition
	ModelContext               = appcontext.ModelContext
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)

// RequestSequencer 提供本次请求可使用的序号；请求 Hook 在调用模型前原子确认并推进序号。
// 风险审核也会消耗序号，每次请求都读取最新值，不保留进度或执行恢复能力。
type RequestSequencer interface {
	NextRequestSequence(context.Context, identity.AgentExecutionID) (uint64, error)
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
	ID        identity.AgentExecutionID
	SessionID identity.SessionID
	AgentID   identity.AgentID
	RequestID identity.RequestID
	Config    Config
	Content   appcontext.Content
}

// ExecutionDependencies 只在本次 Run 中使用；共享 Runner 不保存单次执行的环境或等待状态。
type ExecutionDependencies struct {
	ToolExecutor  ToolExecutor
	AwaitApproval func(context.Context) error
	Writer        repository.MessageWriter
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
