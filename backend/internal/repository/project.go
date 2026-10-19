package repository

import (
	"context"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/identity"
)

// Project 提供项目聚合的持久化和目录查询。
type Project interface {
	Get(context.Context, identity.ProjectID) (projectmodel.Project, error)
	Save(context.Context, projectmodel.Project) error
	List(context.Context, int) ([]projectmodel.Project, error)
}
