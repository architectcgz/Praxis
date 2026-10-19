package repository

import (
	"context"
	"praxis/internal/core/message"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
)

// MessageStream 返回同一次一致性读取的消息和序号边界。
type MessageStream struct {
	SequenceBoundary uint64
	Messages         []message.Message
}

// MissingToolResult 是已声明但尚无回执的工具调用查询结果，不包含恢复决策。
type MissingToolResult struct {
	SessionID   identity.SessionID
	AgentID     identity.AgentID
	ExecutionID identity.AgentExecutionID
	CallID      string
	RequestID   string
	TurnID      identity.TurnID
	Name        string
	Sequence    uint64
	CallIndex   int
}

// AssistantWrite 在同一次提交中保存消息、工具调用和最终输出证据。
type AssistantWrite struct {
	Message     message.Message
	ToolCalls   []toolexecution.Execution
	FinalOutput *toolexecution.FinalOutput
}

// MessageReader 按 Session 和 Agent 的消息归属读取一致性历史。
type MessageReader interface {
	LoadMessages(context.Context, identity.SessionID, identity.AgentID, int) (MessageStream, error)
}

// ToolResultWriter 原子、幂等地保存工具回执及其输出消耗。
type ToolResultWriter interface {
	AppendToolResult(context.Context, message.Message) error
}

// MessageWriter 提供绑定消息归属的执行输出写入能力。
type MessageWriter interface {
	ToolResultWriter
	AppendAssistant(context.Context, AssistantWrite) (message.Message, bool, error)
}

// MessageWriterProvider 按执行归属创建原子输出写入器。
type MessageWriterProvider interface {
	Writer(context.Context, identity.SessionID, identity.AgentID) (MessageWriter, error)
}

// MessageStreams 根据持久化 Agent 归属选择消息流，调用方不决定日志键位。
type MessageStreams interface {
	MessageReader
	MessageWriterProvider
	ListMissingToolResults(context.Context) ([]MissingToolResult, error)
	Append(context.Context, identity.SessionID, identity.AgentID, message.Message) (message.Message, error)
	List(context.Context, identity.SessionID, identity.AgentID, uint64, int) ([]message.Message, error)
}
