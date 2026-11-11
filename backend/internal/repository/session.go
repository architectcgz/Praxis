package repository

import (
	"context"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"time"
)

// Session 提供会话元数据读写，不加载或持有会话历史。
type Session interface {
	Get(context.Context, identity.SessionID) (sessionmodel.SessionInfo, error)
	Create(context.Context, sessionmodel.SessionInfo, sessionmodel.Branch) error
	BranchCatalog(context.Context, identity.SessionID) (sessionmodel.BranchCatalog, error)
	CreateBranch(context.Context, identity.SessionID, sessionmodel.Branch) error
	SelectBranch(context.Context, identity.SessionID, identity.BranchID) error
	DeleteBranch(context.Context, identity.SessionID, identity.BranchID, identity.BranchID) error
	List(context.Context, int) ([]sessionmodel.SessionInfo, error)
	ListByProject(context.Context, identity.ProjectID, int) ([]sessionmodel.SessionInfo, error)
	// UpdateTitle 仅当原标题等于 expectedTitle 时原子更新；未匹配记录返回 ErrRequestConflict。
	UpdateTitle(ctx context.Context, id identity.SessionID, expectedTitle, title string, at time.Time) error
	SetApprovalMode(context.Context, identity.SessionID, *security.ApprovalMode, time.Time) (sessionmodel.SessionInfo, error)
	Delete(context.Context, identity.SessionID) error
}
