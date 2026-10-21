package storage

import (
	"cmp"
	"context"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/identity"
)

type ProjectRepository struct{ s *Store }

func (r ProjectRepository) Get(ctx context.Context, id identity.ProjectID) (projectmodel.Project, error) {
	return get[projectmodel.Project](ctx, r.s, "project", id.String())
}

// Save 保存单条项目事实，默认 Workspace 由该事实派生。
func (r ProjectRepository) Save(ctx context.Context, v projectmodel.Project) error {
	return save(ctx, r.s, "", "project", v.ID.String(), "project.saved", v)
}

// List 返回查询结果的独立副本，limit 为 0 时不限制。
func (r ProjectRepository) List(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	v, e := listIndexed[projectmodel.Project](ctx, r.s, collectionKey("project"), nil)
	return capped(sorted(v, func(a, b projectmodel.Project) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	}), limit), e
}
