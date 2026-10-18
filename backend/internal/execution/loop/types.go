package loop

import (
	"context"
	taskmodel "praxis/internal/agent"
	appcontext "praxis/internal/context"
	"praxis/internal/identity"
	"praxis/internal/tool"
	"praxis/internal/toolcall"
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

// TaskProgress 提供已提交的 Task 快照及模型消耗记录；事务由实现方负责。
// 工具调用次数和回执字节由工具准入、消息写入负责，Loop 只读取累计结果。
type TaskProgress interface {
	// LoadTask 返回当前持久化快照；读取失败时停止执行，不用旧预算继续运行。
	LoadTask(context.Context, identity.AgentExecutionID) (taskmodel.Execution, error)
	// RecordIteration 在模型请求前推进序号和请求次数；失败时不得调用模型。
	RecordIteration(context.Context, identity.AgentExecutionID, uint64) error
	// RecordSummaryRequest 推进唯一请求序号，不消耗 ReAct 迭代。
	RecordSummaryRequest(context.Context, identity.AgentExecutionID, uint64) error
	// RecordModelOutput 保存一次模型响应的文本消耗，取消后也不丢失已用额度。
	RecordModelOutput(context.Context, identity.AgentExecutionID, int) error
}
