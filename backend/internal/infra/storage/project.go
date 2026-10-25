package storage

import (
	"cmp"
	"context"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/identity"
)

type ProjectRepository struct{ storage *Storage }

func (r ProjectRepository) Get(ctx context.Context, id identity.ProjectID) (projectmodel.Project, error) {
	return get[projectmodel.Project](ctx, r.storage, "project", id.String())
}

// Save 保存单条项目事实，默认 Workspace 由该事实派生。
func (r ProjectRepository) Save(ctx context.Context, v projectmodel.Project) error {
	return save(ctx, r.storage, "", "project", v.ID.String(), "project.saved", v)
}

func (r ProjectRepository) List(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	values, err := list[projectmodel.Project](ctx, r.storage, "project", nil)
	return capped(sorted(values, func(a, b projectmodel.Project) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	}), limit), err
}
