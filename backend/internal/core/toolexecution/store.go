package toolexecution

import (
	"context"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/identity"
)

// Store 提供工具执行从请求登记到结果结算所需的最小持久化能力。
type Store interface {
	Get(context.Context, identity.ToolExecutionID) (Execution, error)
	FindByExecutionCall(context.Context, identity.AgentExecutionID, string) (Execution, error)
	ListUnsettledBySession(context.Context, identity.SessionID) ([]Execution, error)
	ListByExecution(context.Context, identity.AgentExecutionID) ([]Execution, error)
	ListAwaitingByAgent(context.Context, identity.AgentID) ([]Execution, error)
	Save(context.Context, Execution) error
}

// AssistantWrite 携带一次 Assistant 消息、同一轮声明的工具调用和最终输出证据。
type AssistantWrite struct {
	Message     messagemodel.Message
	ToolCalls   []Execution
	FinalOutput *FinalOutput
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

// FinalOutput 保存最终 Assistant 的提交证据。
type FinalOutput struct {
	AssistantID string
	StopReason  string
}
