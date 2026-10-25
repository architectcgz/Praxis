package storage

import (
	"cmp"
	"context"
	projectmodel "praxis/internal/core/project"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/identity"
)

type WorkspaceRepository struct{ storage *Storage }

func (r WorkspaceRepository) Get(ctx context.Context, id identity.WorkspaceID) (workspacemodel.Workspace, error) {
	return get[workspacemodel.Workspace](ctx, r.storage, "workspace", id.String())
}

func (r WorkspaceRepository) Save(ctx context.Context, v workspacemodel.Workspace) error {
	return save(ctx, r.storage, "", "workspace", v.ID.String(), "workspace.saved", v)
}

func (r WorkspaceRepository) ListByProject(ctx context.Context, id identity.ProjectID, limit int) ([]workspacemodel.Workspace, error) {
	var result []workspacemodel.Workspace
	err := withState(ctx, r.storage, func(_ *Storage, records []logRecord) error {
		project, err := getRecord[projectmodel.Project](records, "project", id.String())
		if err != nil {
			return err
		}
		result, err = listRecords(records, "workspace", func(value workspacemodel.Workspace) bool { return value.ProjectID == id })
		if err == nil {
			result = append(result, workspacemodel.NewWorkspace(project.DefaultWorkspaceID, project.ID, workspacemodel.WorkspaceProjectRoot, project.Path, project.CreatedAt))
		}
		return err
	})
	return capped(sorted(result, func(a, b workspacemodel.Workspace) int { return cmp.Compare(a.ID, b.ID) }), limit), err
}
