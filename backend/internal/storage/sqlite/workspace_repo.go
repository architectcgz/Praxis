package sqlite

import (
	"context"
	"time"

	"praxis/internal/core/domain"
)

func (s *Store) GetWorkspace(ctx context.Context, id domain.WorkspaceID) (domain.Workspace, error) {
	var value domain.Workspace
	return value, s.loadPayload(ctx, "workspaces", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) ListWorkspacesByProject(
	ctx context.Context,
	projectID domain.ProjectID,
	limit int,
) ([]domain.Workspace, error) {
	return listTargetPayloads[domain.Workspace](
		ctx,
		s,
		`SELECT payload FROM workspaces WHERE project_id = ? ORDER BY id LIMIT ?`,
		[]any{projectID.String(), targetLimit(limit)},
		"workspaces",
		func(value domain.Workspace) error { return value.Validate() },
	)
}

func (s *Store) SaveWorkspace(ctx context.Context, value domain.Workspace) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO workspaces (
			id, project_id, kind, path, state, revision, created_at, updated_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET project_id = excluded.project_id,
		 kind = excluded.kind, path = excluded.path, state = excluded.state,
		 revision = excluded.revision, created_at = excluded.created_at,
		 updated_at = excluded.updated_at, payload = excluded.payload`,
		value.ID.String(),
		value.ProjectID.String(),
		string(value.Kind),
		value.Path,
		string(value.State),
		value.Revision,
		value.CreatedAt.UTC().Format(time.RFC3339Nano),
		value.UpdatedAt.UTC().Format(time.RFC3339Nano),
		payload,
	)
}

type WorkspaceRepository struct{ store *Store }

func (r WorkspaceRepository) Get(ctx context.Context, id domain.WorkspaceID) (domain.Workspace, error) {
	return r.store.GetWorkspace(ctx, id)
}

func (r WorkspaceRepository) Save(ctx context.Context, value domain.Workspace) error {
	return r.store.SaveWorkspace(ctx, value)
}

func (r WorkspaceRepository) ListByProject(
	ctx context.Context,
	projectID domain.ProjectID,
	limit int,
) ([]domain.Workspace, error) {
	return r.store.ListWorkspacesByProject(ctx, projectID, limit)
}
