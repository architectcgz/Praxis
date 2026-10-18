package session

import (
	"context"
	"praxis/internal/identity"
	"time"
)

// Store 提供会话聚合的持久化和生命周期操作。
type Store interface {
	Get(context.Context, identity.SessionID) (Session, error)
	Save(context.Context, Session) error
	HasMessages(context.Context, identity.SessionID) (bool, error)
	Rename(context.Context, identity.SessionID, string, time.Time) error
	Delete(context.Context, identity.SessionID) ([]string, error)
}

// ListReader 提供会话目录查询能力。
type ListReader interface {
	List(context.Context, int) ([]Session, error)
}

// ProjectListReader 提供项目范围内的会话目录查询能力。
type ProjectListReader interface {
	ListByProject(context.Context, identity.ProjectID, int) ([]Session, error)
}
