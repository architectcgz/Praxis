package repository

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

// SessionContext 追加和查询不可变的会话共享事实。
type SessionContext interface {
	Append(context.Context, sessionmodel.SessionContextEntry) error
	List(context.Context, identity.SessionID, string, int) ([]sessionmodel.SessionContextEntry, error)
	GetByID(context.Context, identity.ContextEntryID) (sessionmodel.SessionContextEntry, bool, error)
}
