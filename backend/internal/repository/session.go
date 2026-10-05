package repository

import (
	"praxis/internal/contracts"
	contextmodel "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"

	"context"
	"time"
)

// SessionRepository 负责会话聚合的持久化。
type SessionRepository interface {
	Get(ctx context.Context, id contracts.SessionID) (sessionmodel.Session, error)
	Save(ctx context.Context, session sessionmodel.Session) error
	// Rename 更新会话标题和更新时间。
	Rename(ctx context.Context, id contracts.SessionID, title string, updatedAt time.Time) error
	// Delete 删除指定会话及其持久化关联数据，并返回待清理的文档引用。
	Delete(ctx context.Context, id contracts.SessionID) ([]string, error)
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
	Append(context.Context, contextmodel.SessionContextEntry) error
	List(context.Context, contracts.SessionID, string, int) ([]contextmodel.SessionContextEntry, error)
	GetByID(context.Context, contracts.ContextEntryID) (contextmodel.SessionContextEntry, bool, error)
}
