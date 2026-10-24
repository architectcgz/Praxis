package repository

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

// EntryList 的消息与序号边界来自同一次一致性读取。
type EntryList struct {
	SequenceBoundary uint64
	Entries          []sessionentry.ContextEntry
}

type EntryQuery struct {
	AgentID identity.AgentID
	// IncludeUnadmitted 供恢复查询；普通查询隐藏尚未建立 Execution 的输入。
	IncludeUnadmitted bool
	ForContext        bool
}

// Entries 按 AgentID 读取唯一历史并顺序追加 Entry，不保存执行上下文或更新 Execution。
type Entries interface {
	Append(context.Context, sessionentry.Record) (sessionentry.ContextEntry, bool, error)
	Get(context.Context, identity.AgentID, string) (sessionentry.ContextEntry, error)
	List(context.Context, EntryQuery) (EntryList, error)
}
