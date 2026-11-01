package session

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/identity"
	"time"
)

const (
	EventBranchCreated        = "branch.created"
	EventExecutionSaved       = "agent_execution.saved"
	EventEntryAppended        = "entry.appended"
	EventModelRequestFinished = "agent_execution.model_request_finished"
	EventTimingStarted        = "agent_execution.timing_started"
	EventTimingFinished       = "agent_execution.timing_finished"
)

// Event 是会话提交和恢复使用的业务记录，不是进程内通知。
// Payload 按 Type 携带对应的分支、执行快照、条目、请求完成记录或计时记录。
type Event struct {
	Type      string
	Key       string
	AgentID   identity.AgentID
	BranchID  identity.BranchID
	CreatedAt time.Time
	Payload   any
}

// EventLog 同步追加业务记录并按顺序读取；会话服务负责事件编解码，存储只处理完整行。
// 同一会话由 Session 串行追加；返回成功不承诺断电持久化。
type EventLog interface {
	Read(context.Context, identity.SessionID, identity.AgentID, func(Event) error) error
	CreateSubagent(context.Context, identity.SessionID, identity.AgentID) error
	Subagents(context.Context, identity.SessionID) ([]identity.AgentID, error)
	AppendEvent(context.Context, identity.SessionID, Event) error
}

// ModelRequestFinished 将请求明细和接纳后的累计快照放在同一日志提交中。
type ModelRequestFinished struct {
	Request   agentexecution.ModelRequestRecord `json:"request"`
	Execution agentexecution.ExecutionRecord    `json:"execution"`
}
