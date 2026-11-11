package session

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"time"
)

const (
	EventRunSaved             = "agent_execution.saved"
	EventRunEnded             = "agent_execution.ended"
	EventEntryAppended        = "entry.appended"
	EventModelRequestStarted  = "agent_execution.model_request_started"
	EventModelRequestFinished = "agent_execution.model_request_finished"
)

// Event 是会话提交和恢复使用的业务记录，不是进程内通知。
// Payload 按 Type 携带执行快照、条目或模型请求状态；分支结构只保存在 SQLite。
type Event struct {
	Type      string
	Key       string
	AgentID   identity.AgentID
	BranchID  identity.BranchID
	CreatedAt time.Time
	Payload   any
}

// EventLog 绑定所属会话，同步追加业务记录；会话服务负责事件编解码，存储只处理完整行。
// 同一会话由 Session 串行追加；返回成功不承诺断电持久化。
type EventLog interface {
	CreateSubagent(context.Context, identity.BranchID, identity.AgentID) error
	AppendEvent(context.Context, Event) (bool, error)
	Sync(context.Context) error
	Node(context.Context, string) (sessionentry.Node, error)
	IndexedNodes(context.Context, []string) ([]sessionentry.Node, error)
	PathNodes(context.Context, string) ([]sessionentry.Node, error)
	TreeNodes(context.Context) ([]sessionentry.Node, error)
	Entries(context.Context, []string) ([]*sessionentry.ContextEntry, error)
	Requests(context.Context, []identity.TurnID) ([]agentrun.ModelRequestRecord, error)
	Run(context.Context, identity.RunID) (agentrun.RunRecord, error)
}

// RunEnded 将终态与稳定身份的结束节点放在同一次历史提交中。
type RunEnded struct {
	Run   agentrun.RunRecord        `json:"execution"`
	Entry sessionentry.ContextEntry `json:"entry"`
}

// ModelRequestSaved 将请求状态、用量和接纳后的累计快照放在同一日志提交中。
type ModelRequestSaved struct {
	Request agentrun.ModelRequestRecord `json:"request"`
	Run     agentrun.RunRecord          `json:"execution"`
}
