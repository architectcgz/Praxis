package workspace

import (
	"context"
	"praxis/internal/identity"
)

// Store 提供工作区聚合的持久化和项目范围查询能力。
type Store interface {
	Get(context.Context, identity.WorkspaceID) (Workspace, error)
	Save(context.Context, Workspace) error
	ListByProject(context.Context, identity.ProjectID, int) ([]Workspace, error)
}
