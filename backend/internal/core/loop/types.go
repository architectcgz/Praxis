package loop

import (
	"context"
	appcontext "praxis/internal/core/context"
	messagemodel "praxis/internal/core/message"
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolcall"
	"praxis/internal/identity"
)

// ToolExecutionReader 只读取是否已有冻结的调用参数。
type ToolExecutionReader interface {
	FindByExecutionCall(context.Context, identity.AgentExecutionID, string) (toolcall.Execution, error)
}

// ToolExecutor 执行已经登记的调用；等待审批时不产生工具回执。
type ToolExecutor interface {
	Execute(context.Context, tool.PreparedToolCall, toolcall.ToolExecutionMetadata) (tool.ToolExecutionResult, error)
}

type (
	ToolCall                   = tool.ToolCall
	ToolDefinition             = tool.ToolDefinition
	ModelContext               = appcontext.ModelContext
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)

// ProgressStore 提供已提交的 执行输入 快照及模型消耗记录；事务由实现方负责。
// 工具调用次数和回执字节由工具准入、消息写入负责，Loop 只读取累计结果。
type ProgressStore interface {
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
	Progress     ProgressStore
	ID           identity.AgentExecutionID
	SessionID    identity.SessionID
	AgentID      identity.AgentID
	RequestID    identity.RequestID
	Config       Config
	Content      appcontext.Content
	Continuation []messagemodel.Message
	Writer       toolcall.Writer
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
