package repository

import (
	projectmodel "praxis/internal/core/project"

	"context"
	"praxis/internal/core/identity"
)

// ProjectRepository 负责项目聚合的持久化。
type ProjectRepository interface {
	Get(context.Context, identity.ProjectID) (projectmodel.Project, error)
	Save(context.Context, projectmodel.Project) error
}

// ProjectListRepository 提供项目目录查询。
type ProjectListRepository interface {
	List(context.Context, int) ([]projectmodel.Project, error)
}
