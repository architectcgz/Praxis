package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	domainfoundation "praxis/internal/domain/foundation"
	domainworkspace "praxis/internal/domain/workspace"
)

func (s *Store) GetWorkspace(ctx context.Context, id domainfoundation.WorkspaceID) (domainworkspace.Workspace, error) {
	row := s.Executor(ctx).QueryRowContext(ctx, `SELECT id, project_id, kind, path, state, revision, created_at, updated_at FROM workspaces WHERE id = ?`, id.String())
	var value domainworkspace.Workspace
	var createdAt, updatedAt string
	if err := row.Scan(&value.ID, &value.ProjectID, &value.Kind, &value.Path, &value.State, &value.Revision, &createdAt, &updatedAt); err == sql.ErrNoRows {
		return value, domainfoundation.ErrNotFound
	} else if err != nil {
		return value, fmt.Errorf("read workspace: %w", err)
	}
	var err error
	value.CreatedAt, err = parseTimestamp(createdAt, "workspace.createdAt")
	if err != nil {
		return value, err
	}
	value.UpdatedAt, err = parseTimestamp(updatedAt, "workspace.updatedAt")
	if err != nil {
		return value, err
	}
	return value, value.Validate()
}

func (s *Store) ListWorkspacesByProject(ctx context.Context, projectID domainfoundation.ProjectID, limit int) ([]domainworkspace.Workspace, error) {
	rows, err := s.Executor(ctx).QueryContext(ctx, `SELECT id, project_id, kind, path, state, revision, created_at, updated_at FROM workspaces WHERE project_id = ? ORDER BY id LIMIT ?`, projectID.String(), targetLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()
	values := make([]domainworkspace.Workspace, 0)
	for rows.Next() {
		var value domainworkspace.Workspace
		var createdAt, updatedAt string
		if err := rows.Scan(&value.ID, &value.ProjectID, &value.Kind, &value.Path, &value.State, &value.Revision, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		value.CreatedAt, err = parseTimestamp(createdAt, "workspace.createdAt")
		if err != nil {
			return nil, err
		}
		value.UpdatedAt, err = parseTimestamp(updatedAt, "workspace.updatedAt")
		if err != nil {
			return nil, err
		}
		if err := value.Validate(); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) SaveWorkspace(ctx context.Context, value domainworkspace.Workspace) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.execMutation(ctx, `INSERT INTO workspaces (id, project_id, kind, path, state, revision, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET project_id = excluded.project_id, kind = excluded.kind, path = excluded.path, state = excluded.state, revision = excluded.revision, created_at = excluded.created_at, updated_at = excluded.updated_at`, value.ID.String(), value.ProjectID.String(), string(value.Kind), value.Path, string(value.State), value.Revision, value.CreatedAt.UTC().Format(time.RFC3339Nano), value.UpdatedAt.UTC().Format(time.RFC3339Nano))
}

type WorkspaceRepository struct{ store *Store }

func (r WorkspaceRepository) Get(ctx context.Context, id domainfoundation.WorkspaceID) (domainworkspace.Workspace, error) {
	return r.store.GetWorkspace(ctx, id)
}
func (r WorkspaceRepository) Save(ctx context.Context, value domainworkspace.Workspace) error {
	return r.store.SaveWorkspace(ctx, value)
}
func (r WorkspaceRepository) ListByProject(ctx context.Context, projectID domainfoundation.ProjectID, limit int) ([]domainworkspace.Workspace, error) {
	return r.store.ListWorkspacesByProject(ctx, projectID, limit)
}
