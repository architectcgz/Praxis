package loop

import (
	"context"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
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
	ReadEntries           func(context.Context) ([]*sessionentry.ContextEntry, error)
	AppendEntry           AppendEntry
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
