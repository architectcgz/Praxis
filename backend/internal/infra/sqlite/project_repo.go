package sqlite

import (
	"praxis/internal/contracts"
	projectmodel "praxis/internal/project"

	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *Store) GetProject(ctx context.Context, id contracts.ProjectID) (projectmodel.Project, error) {
	row := s.Executor(ctx).QueryRowContext(ctx, `SELECT id, name, path, default_workspace_id, state, created_at, updated_at FROM projects WHERE id = ?`, id.String())
	var value projectmodel.Project
	var createdAt, updatedAt string
	if err := row.Scan(&value.ID, &value.Name, &value.Path, &value.DefaultWorkspaceID, &value.State, &createdAt, &updatedAt); err == sql.ErrNoRows {
		return value, contracts.ErrNotFound
	} else if err != nil {
		return value, fmt.Errorf("read project: %w", err)
	}
	var err error
	value.CreatedAt, err = parseTimestamp(createdAt, "project.createdAt")
	if err != nil {
		return value, err
	}
	value.UpdatedAt, err = parseTimestamp(updatedAt, "project.updatedAt")
	if err != nil {
		return value, err
	}
	return value, value.Validate()
}

func (s *Store) ListProjects(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	rows, err := s.Executor(ctx).QueryContext(ctx, `SELECT id, name, path, default_workspace_id, state, created_at, updated_at FROM projects ORDER BY updated_at DESC, id LIMIT ?`, targetLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	values := make([]projectmodel.Project, 0)
	for rows.Next() {
		var value projectmodel.Project
		var createdAt, updatedAt string
		if err := rows.Scan(&value.ID, &value.Name, &value.Path, &value.DefaultWorkspaceID, &value.State, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		value.CreatedAt, err = parseTimestamp(createdAt, "project.createdAt")
		if err != nil {
			return nil, err
		}
		value.UpdatedAt, err = parseTimestamp(updatedAt, "project.updatedAt")
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

func (s *Store) SaveProject(ctx context.Context, value projectmodel.Project) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.execMutation(ctx, `INSERT INTO projects (id, name, path, default_workspace_id, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET name = excluded.name, path = excluded.path, default_workspace_id = excluded.default_workspace_id, state = excluded.state, created_at = excluded.created_at, updated_at = excluded.updated_at`, value.ID.String(), value.Name, value.Path, value.DefaultWorkspaceID.String(), string(value.State), value.CreatedAt.UTC().Format(time.RFC3339Nano), value.UpdatedAt.UTC().Format(time.RFC3339Nano))
}

type ProjectRepository struct{ store *Store }

func (r ProjectRepository) Get(ctx context.Context, id contracts.ProjectID) (projectmodel.Project, error) {
	return r.store.GetProject(ctx, id)
}
func (r ProjectRepository) Save(ctx context.Context, value projectmodel.Project) error {
	return r.store.SaveProject(ctx, value)
}
func (r ProjectRepository) List(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	return r.store.ListProjects(ctx, limit)
}
