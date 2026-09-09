package sqlite

import (
	"context"
	domainfoundation "praxis/internal/domain/foundation"
	domainproject "praxis/internal/domain/project"
	"time"
)

func (s *Store) GetProject(ctx context.Context, id domainfoundation.ProjectID) (domainproject.Project, error) {
	var value domainproject.Project
	return value, s.loadPayload(ctx, "projects", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) ListProjects(ctx context.Context, limit int) ([]domainproject.Project, error) {
	return listTargetPayloads[domainproject.Project](
		ctx,
		s,
		`SELECT payload FROM projects ORDER BY updated_at DESC, id LIMIT ?`,
		[]any{targetLimit(limit)},
		"projects",
		func(value domainproject.Project) error { return value.Validate() },
	)
}

func (s *Store) SaveProject(ctx context.Context, value domainproject.Project) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO projects (id, name, path, default_workspace_id, state, created_at, updated_at, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name = excluded.name,
		 path = excluded.path,
		 default_workspace_id = excluded.default_workspace_id, state = excluded.state,
		 created_at = excluded.created_at, updated_at = excluded.updated_at, payload = excluded.payload`,
		value.ID.String(),
		value.Name,
		value.Path,
		value.DefaultWorkspaceID.String(),
		string(value.State),
		value.CreatedAt.UTC().Format(time.RFC3339Nano),
		value.UpdatedAt.UTC().Format(time.RFC3339Nano),
		payload,
	)
}

type ProjectRepository struct{ store *Store }

func (r ProjectRepository) Get(ctx context.Context, id domainfoundation.ProjectID) (domainproject.Project, error) {
	return r.store.GetProject(ctx, id)
}

func (r ProjectRepository) Save(ctx context.Context, value domainproject.Project) error {
	return r.store.SaveProject(ctx, value)
}

func (r ProjectRepository) List(ctx context.Context, limit int) ([]domainproject.Project, error) {
	return r.store.ListProjects(ctx, limit)
}
