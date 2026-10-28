package storage

import (
	"cmp"
	"context"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

type WorkspaceRepository struct{ storage *Store }

// Get 优先读取保存的工作区，再显式派生项目根目录工作区。
func (r WorkspaceRepository) Get(ctx context.Context, id identity.WorkspaceID) (workspacemodel.Workspace, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return workspacemodel.Workspace{}, err
	}
	defer r.storage.mu.RUnlock()
	if value, ok := r.storage.state.workspaces[id]; ok {
		return value, nil
	}
	for _, project := range r.storage.state.projects {
		if project.DefaultWorkspaceID == id {
			return workspacemodel.NewWorkspace(project.DefaultWorkspaceID, project.ID, workspacemodel.WorkspaceProjectRoot, project.Path, project.CreatedAt), nil
		}
	}
	return workspacemodel.Workspace{}, errorcode.ErrNotFound
}

func (r WorkspaceRepository) Save(ctx context.Context, value workspacemodel.Workspace) error {
	if err := r.storage.lock(ctx); err != nil {
		return err
	}
	defer r.storage.mu.Unlock()
	if err := r.storage.appendEventLocked(ctx, "", "workspace.saved", "workspace", value.ID.String(), value); err != nil {
		return err
	}
	r.storage.state.workspaces[value.ID] = value
	return nil
}

func (r WorkspaceRepository) ListByProject(ctx context.Context, id identity.ProjectID, limit int) ([]workspacemodel.Workspace, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return nil, err
	}
	defer r.storage.mu.RUnlock()
	project, ok := r.storage.state.projects[id]
	if !ok {
		return nil, errorcode.ErrNotFound
	}
	result := make([]workspacemodel.Workspace, 0)
	for _, value := range r.storage.state.workspaces {
		if value.ProjectID == id {
			result = append(result, value)
		}
	}
	if _, saved := r.storage.state.workspaces[project.DefaultWorkspaceID]; !saved {
		result = append(result, workspacemodel.NewWorkspace(project.DefaultWorkspaceID, project.ID, workspacemodel.WorkspaceProjectRoot, project.Path, project.CreatedAt))
	}
	return capped(sorted(result, func(a, b workspacemodel.Workspace) int { return cmp.Compare(a.ID, b.ID) }), limit), nil
}
