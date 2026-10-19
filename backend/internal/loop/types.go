package loop

import (
	"context"
	appcontext "praxis/internal/core/context"
	messagemodel "praxis/internal/core/message"
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// ToolExecutor 执行已经登记的调用；等待审批时不产生工具回执。
type ToolExecutor interface {
	Execute(context.Context, tool.PreparedToolCall, toolexecution.ToolExecutionMetadata) (tool.ToolExecutionResult, error)
}

type (
	ToolCall                   = tool.ToolCall
	ToolDefinition             = tool.ToolDefinition
	ModelContext               = appcontext.ModelContext
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)

// ProgressTracker 提供绑定本次执行的进度业务操作，由 Execution Service 检查状态、额度并提交事务。
// 工具调用次数和回执字节由工具准入、消息写入负责，Loop 不直接修改执行实体。
type ProgressTracker interface {
	// LoadProgress 返回当前持久化快照；读取失败时停止执行，不用旧预算继续运行。
	LoadProgress(context.Context) (Progress, error)
	// RecordIteration 在模型请求前推进序号和请求次数；失败时不得调用模型。
	RecordIteration(context.Context, uint64) error
	// RecordSummaryRequest 推进唯一请求序号，不消耗 ReAct 迭代。
	RecordSummaryRequest(context.Context, uint64) error
	// RecordModelOutput 保存一次模型响应的文本消耗，取消后也不丢失已用额度。
	RecordModelOutput(context.Context, int) error
}

// EndReason 只描述 Loop 为什么返回，不表示业务持久化结果。
type EndReason string

const (
	EndCompleted        EndReason = "completed"
	EndAwaitingApproval EndReason = "awaiting_approval"
	endFailed           EndReason = "failed"
)

type Result struct{ Reason EndReason }

// Input 是调用方冻结的本次模型和工具执行输入。
type Input struct {
	Progress     ProgressTracker
	ID           identity.AgentExecutionID
	SessionID    identity.SessionID
	AgentID      identity.AgentID
	RequestID    identity.RequestID
	Config       Config
	Content      appcontext.Content
	Continuation []messagemodel.Message
	Writer       repository.MessageWriter
}
type Config struct {
	Model         modelapi.Config
	WorkspacePath string
}

// Progress 是 Loop 消费的已提交额度及输出证据。
type Progress struct {
	NextSequence    uint64
	Iterations      uint64
	FinalStopReason string
}

const MaxIterations = 30

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
