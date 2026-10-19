package repository

import (
	"context"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/identity"
)

// Workspace 持久化共享文件系统资源的业务事实，不执行文件系统操作。
type Workspace interface {
	Get(context.Context, identity.WorkspaceID) (workspacemodel.Workspace, error)
	Save(context.Context, workspacemodel.Workspace) error
	ListByProject(context.Context, identity.ProjectID, int) ([]workspacemodel.Workspace, error)
}
