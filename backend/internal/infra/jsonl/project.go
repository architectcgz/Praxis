package jsonl

import (
	"cmp"
	"context"
	"praxis/internal/identity"
	projectmodel "praxis/internal/service/project"
)

type ProjectRepository struct{ s *Store }

func (r ProjectRepository) Get(ctx context.Context, id identity.ProjectID) (projectmodel.Project, error) {
	return get[projectmodel.Project](ctx, r.s, "project", id.String())
}

// Save 保存入口已处理的对象，事务未提交时只更新工作区。
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
