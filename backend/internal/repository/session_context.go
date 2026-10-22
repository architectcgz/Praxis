package repository

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

// SessionFacts 追加和查询不可变的会话共享事实。
type SessionFacts interface {
	Append(context.Context, sessionmodel.SharedFact) error
	List(context.Context, identity.SessionID, string, int) ([]sessionmodel.SharedFact, error)
	GetByID(context.Context, identity.ContextEntryID) (sessionmodel.SharedFact, bool, error)
}
