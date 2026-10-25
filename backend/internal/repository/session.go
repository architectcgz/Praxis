package repository

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"time"
)

// Session 提供会话事实、目录及会话级消息存在性查询。
type Session interface {
	Get(context.Context, identity.SessionID) (sessionmodel.Session, error)
	Create(context.Context, sessionmodel.Session) error
	List(context.Context, int) ([]sessionmodel.Session, error)
	ListByProject(context.Context, identity.ProjectID, int) ([]sessionmodel.Session, error)
	HasEntries(context.Context, identity.SessionID) (bool, error)
	Rename(context.Context, identity.SessionID, string, time.Time) error
	NameFromInput(context.Context, identity.SessionID, string, time.Time) error
	Delete(context.Context, identity.SessionID, bool) error
}
