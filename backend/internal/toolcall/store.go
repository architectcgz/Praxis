package toolcall

import (
	"context"
	"praxis/internal/identity"
	messagemodel "praxis/internal/message"
	taskmodel "praxis/internal/task"
)

// Store 提供工具调用生命周期所需的最小持久化能力。
type Store interface {
	Get(context.Context, identity.ToolExecutionID) (Execution, error)
	FindByTaskCall(context.Context, identity.TaskID, string) (Execution, error)
	ListUnsettledBySession(context.Context, identity.SessionID) ([]Execution, error)
	ListByTask(context.Context, identity.TaskID) ([]Execution, error)
	ListAwaitingByAgent(context.Context, identity.AgentID) ([]Execution, error)
	Save(context.Context, Execution) error
}

// AssistantWrite 携带一次 Assistant 消息、同一轮声明的工具调用和最终输出证据。
type AssistantWrite struct {
	Message     messagemodel.Message
	ToolCalls   []Execution
	FinalOutput *taskmodel.FinalOutput
}

// Writer 提供执行输出所需的原子 Assistant 和工具回执写入能力。
type Writer interface {
	AppendAssistant(context.Context, AssistantWrite) (messagemodel.Message, bool, error)
	AppendToolResult(context.Context, messagemodel.Message) error
}

// WriterProvider 按执行归属提供原子输出写入器。
type WriterProvider interface {
	Writer(context.Context, identity.SessionID, identity.AgentID) (Writer, error)
}
