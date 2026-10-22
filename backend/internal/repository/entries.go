package repository

import (
	"context"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/session/entry"
)

// EntryList 的消息与序号边界来自同一次一致性读取。
type EntryList struct {
	SequenceBoundary uint64
	Entries          []sessionentry.ContextEntry
}

type EntryQuery struct {
	Stream sessionentry.StreamKey
	// IncludeUnadmitted 供恢复查询；普通查询隐藏尚未建立 Execution 的输入。
	IncludeUnadmitted bool
	ForContext        bool
}

// Entries 读取 Session 内存历史并顺序追加 Entry，不选择流或更新 Execution。
type Entries interface {
	Append(context.Context, sessionentry.Record) (sessionentry.ContextEntry, bool, error)
	Get(context.Context, sessionentry.StreamKey, string) (sessionentry.ContextEntry, error)
	List(context.Context, EntryQuery) (EntryList, error)
	SetContext(context.Context, sessionentry.StreamKey, appcontext.ModelContext) (*appcontext.ModelContext, error)
}
