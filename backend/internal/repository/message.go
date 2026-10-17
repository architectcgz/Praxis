package repository

import (
	"context"

	"praxis/internal/contracts"
	"praxis/internal/core/agent"
	"praxis/internal/core/session"
	"praxis/internal/core/task"
	toolexecution "praxis/internal/core/tool_execution"
)

// MessageStream 是单个 Session 或 Agent 消息流的一致读取结果。
type MessageStream struct {
	SequenceBoundary uint64
	Messages         []session.MessageData
}

// MessageLoader 按 Agent 消息流归属读取消息及其序号边界；读取失败时返回错误。
type MessageLoader interface {
	LoadMessages(context.Context, contracts.SessionID, contracts.AgentID, int) (MessageStream, error)
}

// AssistantWrite 显式携带本次提交的完整消息、调用身份与可选最终证据。
type AssistantWrite struct {
	Message     session.MessageData
	ToolCalls   []toolexecution.Execution
	FinalOutput *task.FinalOutput
}

// MessageWriter 统一定义执行输出的原子持久化操作。
type MessageWriter interface {
	// 返回完整保存结果；只有本次新增且提交成功才返回 inserted=true。
	AppendAssistant(context.Context, AssistantWrite) (stored session.MessageData, inserted bool, err error)
	// 回执与 Task 输出字节在同一事务提交；幂等写入不重复计费。
	AppendToolResult(context.Context, session.MessageData) error
}

// MissingToolResult 是已声明但尚无回执的工具调用事实，不包含恢复决策。
type MissingToolResult struct {
	SessionID contracts.SessionID
	AgentID   contracts.AgentID
	TaskID    contracts.TaskID
	CallID    string
	RequestID string
	TurnID    contracts.TurnID
	Name      string
	Sequence  uint64
	CallIndex int
}

// MessageStreams 按 Agent 消息流归属读写消息流：主 Agent 使用 Session 流，其他 Agent 使用私有流。
// 消息流归属由 core/agent 决定，具体日志键位由存储实现决定，调用方不解释存储布局。
type MessageStreams interface {
	MessageLoader
	Writer(context.Context, contracts.SessionID, contracts.AgentID) (MessageWriter, error)
	ListMissingToolResults(context.Context) ([]MissingToolResult, error)
	// Append 幂等追加消息到该 Agent 所属的消息流；身份相同但内容不一致时返回冲突。
	Append(context.Context, agent.Agent, session.MessageData) (session.MessageData, error)
	// List 返回该 Agent 所属消息流中游标之后的记录副本；非正数 limit 表示不限制。
	List(context.Context, agent.Agent, uint64, int) ([]session.MessageData, error)
}
