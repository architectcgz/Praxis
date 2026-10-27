package entry

import (
	"context"
	"praxis/internal/identity"
)

// List 的消息与序号边界来自同一次一致性读取。
type List struct {
	SequenceBoundary uint64
	Entries          []ContextEntry
}

type Query struct {
	AgentID identity.AgentID
	// IncludeUnadmitted 供恢复查询；普通查询隐藏尚未建立 Execution 的输入。
	IncludeUnadmitted bool
}

// Store 提供会话 Entry 的存储契约；实现负责顺序追加、归属校验和重复写入检查。
// 追加成功表示存储已接纳，不额外承诺磁盘同步完成。
type Store interface {
	Append(context.Context, Record) (ContextEntry, bool, error)
	Get(context.Context, identity.AgentID, string) (ContextEntry, error)
	List(context.Context, Query) (List, error)
	LoadContext(context.Context, identity.SessionID, identity.AgentID) ([]ContextEntry, error)
	SaveContext(context.Context, identity.SessionID, identity.AgentID, []ContextEntry) error
}
