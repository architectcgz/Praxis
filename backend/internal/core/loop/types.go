package loop

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/agent/run/timing"
	appcontext "praxis/internal/core/context"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
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

// ModelProgress 提供请求序号、模型消耗和计时记录，不暴露执行生命周期或完整记录。
type ModelProgress interface {
	NextRequestSequence(context.Context) (uint64, error)
	RecordModelRequest(context.Context, uint64, agentrun.ModelRequestRecord, *timing.Record) error
	RecordModelResponse(context.Context, agentrun.ModelRequestRecord, *timing.Record) error
}

// RunParams 保存一次执行的身份和冻结模型配置，供各轮模型请求共用。
type RunParams struct {
	ID           identity.RunID
	SourceTaskID identity.TaskID
	SessionID    identity.SessionID
	BranchID     identity.BranchID
	BindingID    string
	AgentID      identity.AgentID
	RequestID    identity.RequestID
	Model        modelapi.Config
}

// Input 提供本次 Loop 的初始历史与提示词，条目内容由 Session 按只读值共享。
type Input struct {
	SystemPrompt string
	InputEntryID string
	Entries      []*sessionentry.ContextEntry
}

// AppendEntry 同步提交条目，返回已接纳的只读引用、是否新增及保存错误。
// 保存失败仍可能返回已接纳条目，Loop 不因此重新执行已有副作用的工具。
type AppendEntry func(context.Context, sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error)

// RunDependencies 绑定本次 Loop 的事实提交和计时能力，不持有执行或会话对象。
type RunDependencies struct {
	Progress    ModelProgress
	AppendEntry AppendEntry
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
