package repository

import (
	sessionmodel "praxis/internal/core/session"

	"context"
	"praxis/internal/core/identity"
	"time"
)

// SessionRepository 负责会话聚合的持久化。
type SessionRepository interface {
	Get(ctx context.Context, id identity.SessionID) (sessionmodel.Session, error)
	Save(ctx context.Context, session sessionmodel.Session) error
	// HasMessages 判断会话下是否已有任何消息，包含主 Agent 的会话流与协作 Agent 的私有流。
	// 删除会话前用它决定是否需要先清理消息内容，不暴露具体流。
	HasMessages(ctx context.Context, id identity.SessionID) (bool, error)
	// Rename 更新会话标题和更新时间。
	Rename(ctx context.Context, id identity.SessionID, title string, updatedAt time.Time) error
	// Delete 删除指定会话及其持久化关联数据，并返回待清理的文档引用。
	Delete(ctx context.Context, id identity.SessionID) ([]string, error)
}

// SessionListRepository 提供应用界面使用的会话目录查询。
type SessionListRepository interface {
	List(ctx context.Context, limit int) ([]sessionmodel.Session, error)
}

// ProjectSessionListRepository 提供项目下的会话目录查询。
type ProjectSessionListRepository interface {
	ListByProject(ctx context.Context, projectID identity.ProjectID, limit int) ([]sessionmodel.Session, error)
}

// SessionContextRepository 负责会话上下文条目的追加和查询。
type SessionContextRepository interface {
	Append(context.Context, sessionmodel.SessionContextEntry) error
	List(context.Context, identity.SessionID, string, int) ([]sessionmodel.SessionContextEntry, error)
	GetByID(context.Context, identity.ContextEntryID) (sessionmodel.SessionContextEntry, bool, error)
}
