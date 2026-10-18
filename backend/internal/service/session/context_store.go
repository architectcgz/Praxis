package session

import (
	"context"
	"praxis/internal/identity"
)

// ContextStore 提供会话共享上下文条目的追加和查询能力。
type ContextStore interface {
	Append(context.Context, SessionContextEntry) error
	List(context.Context, identity.SessionID, string, int) ([]SessionContextEntry, error)
	GetByID(context.Context, identity.ContextEntryID) (SessionContextEntry, bool, error)
}
