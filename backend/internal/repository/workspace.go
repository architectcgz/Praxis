package repository

import (
	workspacemodel "praxis/internal/core/workspace"

	"context"
	"praxis/internal/core/identity"
)

// WorkspaceRepository 负责项目执行工作区的持久化。
type WorkspaceRepository interface {
	Get(context.Context, identity.WorkspaceID) (workspacemodel.Workspace, error)
	Save(context.Context, workspacemodel.Workspace) error
	ListByProject(context.Context, identity.ProjectID, int) ([]workspacemodel.Workspace, error)
}
