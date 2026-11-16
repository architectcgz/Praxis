package session

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/identity"
)

// EntryLog 绑定会话日志，实际文件写入、索引和解码由存储实现完成。
type EntryLog interface {
	CreateSubagent(context.Context, identity.AgentID) error
	AppendEntry(context.Context, identity.AgentID, sessionentry.ContextEntry) (bool, error)
	Sync(context.Context) error
	Node(context.Context, string) (sessionentry.Node, error)
	IndexedNodes(context.Context, []string) ([]sessionentry.Node, error)
	PathNodes(context.Context, string) ([]sessionentry.Node, error)
	Entries(context.Context, []string) ([]*sessionentry.ContextEntry, error)
	Requests(context.Context, []identity.TurnID) ([]sessionentry.ModelRequestRecord, error)
	Run(context.Context, identity.RunID) (sessionentry.RunRecord, error)
}
