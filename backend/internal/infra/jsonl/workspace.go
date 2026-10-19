package jsonl

import (
	"cmp"
	"context"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/identity"
)

type WorkspaceRepository struct{ s *Store }

// Get 读取指定对象，不存在时返回 fault.ErrNotFound。
func (r WorkspaceRepository) Get(ctx context.Context, id identity.WorkspaceID) (workspacemodel.Workspace, error) {
	return get[workspacemodel.Workspace](ctx, r.s, "workspace", id.String())
}

// Save 保存入口已处理的对象，事务未提交时只更新工作区。
func (r WorkspaceRepository) Save(ctx context.Context, v workspacemodel.Workspace) error {
	return save(ctx, r.s, "", "workspace", v.ID.String(), "workspace.saved", v)
}

// ListByProject 返回指定 Project 的对象副本，limit 为 0 时不限制。
func (r WorkspaceRepository) ListByProject(ctx context.Context, id identity.ProjectID, limit int) ([]workspacemodel.Workspace, error) {
	v, e := listIndexed[workspacemodel.Workspace](ctx, r.s, lookupKey{kind: "workspaces-project", owner: id.String()}, nil)
	return capped(sorted(v, func(a, b workspacemodel.Workspace) int { return cmp.Compare(a.ID, b.ID) }), limit), e
}
