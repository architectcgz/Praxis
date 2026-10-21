package storage

import (
	"cmp"
	"context"
	projectmodel "praxis/internal/core/project"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/identity"
)

type WorkspaceRepository struct{ s *Store }

// Get 读取指定对象，不存在时返回 fault.ErrNotFound。
func (r WorkspaceRepository) Get(ctx context.Context, id identity.WorkspaceID) (workspacemodel.Workspace, error) {
	return get[workspacemodel.Workspace](ctx, r.s, "workspace", id.String())
}

// Save 独立保存非默认 Workspace，引用和唯一性在写入锁内检查。
func (r WorkspaceRepository) Save(ctx context.Context, v workspacemodel.Workspace) error {
	return save(ctx, r.s, "", "workspace", v.ID.String(), "workspace.saved", v)
}

// ListByProject 返回指定 Project 的对象副本，limit 为 0 时不限制。
func (r WorkspaceRepository) ListByProject(ctx context.Context, id identity.ProjectID, limit int) ([]workspacemodel.Workspace, error) {
	var result []workspacemodel.Workspace
	err := r.s.view(ctx, func() error {
		project, err := getLocked[projectmodel.Project](r.s, "project", id.String())
		if err != nil {
			return err
		}
		result, err = listIndexedLocked[workspacemodel.Workspace](r.s, lookupKey{kind: "workspaces-project", owner: id.String()}, nil)
		if err == nil {
			result = append(result, workspacemodel.NewWorkspace(project.DefaultWorkspaceID, project.ID, workspacemodel.WorkspaceProjectRoot, project.Path, project.CreatedAt))
		}
		return err
	})
	return capped(sorted(result, func(a, b workspacemodel.Workspace) int { return cmp.Compare(a.ID, b.ID) }), limit), err
}
