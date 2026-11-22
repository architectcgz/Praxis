package repository

import (
	"context"
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"time"
)

// Session 提供会话元数据与只读文件定位，不打开或持有会话历史 Writer。
type Session interface {
	Get(context.Context, identity.SessionID) (sessionmodel.SessionInfo, error)
	SetModelChoice(context.Context, identity.SessionID, modelapi.Choice, time.Time) error
	Create(context.Context, sessionmodel.SessionInfo) error
	ListByProject(context.Context, identity.ProjectID, int) ([]sessionmodel.SessionInfo, error)
	// UpdateTitle 仅当原标题等于 expectedTitle 时原子更新；未匹配记录返回 ErrRequestConflict。
	UpdateTitle(ctx context.Context, id identity.SessionID, expectedTitle, title string, at time.Time) error
	SetApprovalMode(context.Context, identity.SessionID, *security.ApprovalMode, time.Time) (sessionmodel.SessionInfo, error)
	Delete(context.Context, identity.SessionID) error
	SessionLogPath(context.Context, identity.SessionID) (string, error)
	IsEmpty(context.Context, identity.SessionID) (bool, error)
}

// SessionLoader 负责打开和恢复可运行 Session，仅由唯一运行期所有者调用。
type SessionLoader interface {
	Load(context.Context, identity.SessionID) (*sessionmodel.Session, error)
}
