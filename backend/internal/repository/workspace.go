package repository

import (
	"praxis/internal/contracts"
	workspacemodel "praxis/internal/workspace"

	"context"
)

// WorkspaceRepository 负责项目执行工作区的持久化。
type WorkspaceRepository interface {
	Get(context.Context, contracts.WorkspaceID) (workspacemodel.Workspace, error)
	Save(context.Context, workspacemodel.Workspace) error
	ListByProject(context.Context, contracts.ProjectID, int) ([]workspacemodel.Workspace, error)
}
