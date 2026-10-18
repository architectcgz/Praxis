package project

import (
	"context"
	"praxis/internal/identity"
)

// Store 提供项目聚合的持久化能力。
type Store interface {
	Get(context.Context, identity.ProjectID) (Project, error)
	Save(context.Context, Project) error
}

// Reader 提供项目目录查询能力。
type Reader interface {
	List(context.Context, int) ([]Project, error)
}
