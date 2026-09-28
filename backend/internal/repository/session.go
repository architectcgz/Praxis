package repository

import (
	contextmodel "praxis/internal/context"
	"praxis/internal/contracts"
	sessionmodel "praxis/internal/session"

	"context"
)

// SessionRepository 负责会话聚合的持久化。
type SessionRepository interface {
	Get(ctx context.Context, id contracts.SessionID) (sessionmodel.Session, error)
	Save(ctx context.Context, session sessionmodel.Session) error
}

// SessionListRepository 提供应用界面使用的会话目录查询。
type SessionListRepository interface {
	List(ctx context.Context, limit int) ([]sessionmodel.Session, error)
}

// ProjectSessionListRepository 提供项目下的会话目录查询。
type ProjectSessionListRepository interface {
	ListByProject(ctx context.Context, projectID contracts.ProjectID, limit int) ([]sessionmodel.Session, error)
}

// SessionContextRepository 负责会话上下文条目的追加和查询。
type SessionContextRepository interface {
	CurrentRevision(context.Context, contracts.SessionID) (uint64, error)
	Append(context.Context, contextmodel.SessionContextEntry, uint64) error
	List(context.Context, contracts.SessionID, uint64, int) ([]contextmodel.SessionContextEntry, error)
	GetByID(context.Context, contracts.ContextEntryID) (contextmodel.SessionContextEntry, bool, error)
}
