package storage

import (
	"cmp"
	"context"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

type ProjectRepository struct{ storage *Store }

func (r ProjectRepository) Get(ctx context.Context, id identity.ProjectID) (projectmodel.Project, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return projectmodel.Project{}, err
	}
	defer r.storage.mu.RUnlock()
	value, ok := r.storage.state.projects[id]
	if !ok {
		return projectmodel.Project{}, errorcode.ErrNotFound
	}
	return value, nil
}

// Save 保存项目事实，默认 Workspace 由该事实派生。
func (r ProjectRepository) Save(ctx context.Context, value projectmodel.Project) error {
	if err := r.storage.lock(ctx); err != nil {
		return err
	}
	defer r.storage.mu.Unlock()
	if err := r.storage.appendEventLocked(ctx, "", "project.saved", "project", value.ID.String(), value); err != nil {
		return err
	}
	r.storage.state.projects[value.ID] = value
	return nil
}

func (r ProjectRepository) List(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return nil, err
	}
	defer r.storage.mu.RUnlock()
	values := make([]projectmodel.Project, 0, len(r.storage.state.projects))
	for _, value := range r.storage.state.projects {
		values = append(values, value)
	}
	return capped(sorted(values, func(a, b projectmodel.Project) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	}), limit), nil
}
