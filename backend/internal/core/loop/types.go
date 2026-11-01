package loop

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
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
// 结果记录和后置 Hook 调度由执行服务负责。
type ToolExecutor interface {
	Execute(context.Context, sessionentry.ToolCall) (toolexecution.CallResult, error)
}

type (
	ToolCall                   = providerapi.ToolCall
	ToolDefinition             = tool.ToolDefinition
	ModelContext               = appcontext.ModelContext
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)

// ExecutionOperations 只记录本次执行的请求序号和模型消耗，不承担 Entry 读写。
type ExecutionOperations interface {
	NextRequestSequence(context.Context) (uint64, error)
	RecordModelRequest(context.Context, uint64, bool) error
	RecordModelResponse(context.Context, agentexecution.ModelRequestRecord) error
}

// ExecutionParams 保存一次执行的身份和冻结模型配置，供各轮模型请求共用。
type ExecutionParams struct {
	ID           identity.AgentExecutionID
	SourceTaskID identity.TaskID
	SessionID    identity.SessionID
	BranchID     identity.BranchID
	BindingID    string
	AgentID      identity.AgentID
	RequestID    identity.RequestID
	Model        modelapi.Config
}

// ExecutionDependencies 仅绑定本次执行的模型进度和计时能力。
type ExecutionDependencies struct {
	Execution ExecutionOperations
	Timing    *timing.Recorder
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
