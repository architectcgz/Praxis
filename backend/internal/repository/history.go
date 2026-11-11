package repository

import (
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

// LogPosition 只用于存储边界，不进入业务 Entry 或前端协议。
type LogPosition struct {
	BranchID identity.BranchID
	AgentID  identity.AgentID
	Offset   int64
	Length   int64
}

type IndexedEntry struct {
	Node     sessionentry.Node
	Position LogPosition
}

// HistoryUpdate 是一行完整日志产生的可重建投影，与连续索引进度一起提交。
type HistoryUpdate struct {
	Position   LogPosition
	Entry      *sessionentry.Node
	Run        *agentrun.HistorySummary
	EndEntryID string
	Request    *agentrun.ModelRequestRecord
	Calls      []string
	Results    []string
}

type RunLocation struct {
	Record     agentrun.HistorySummary
	EndEntryID string
	Position   LogPosition
}

type AgentHead struct {
	AgentID  identity.AgentID
	BranchID identity.BranchID
	EntryID  string
}
