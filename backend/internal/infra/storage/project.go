package storage

import (
	"cmp"
	"context"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/identity"
)

type ProjectRepository struct{ storage *Store }

func (r ProjectRepository) Get(ctx context.Context, id identity.ProjectID) (projectmodel.Project, error) {
	return r.storage.readProjectConfig(ctx, id)
}

func (r ProjectRepository) Save(ctx context.Context, value projectmodel.Project) error {
	return r.storage.writeProjectConfig(ctx, value)
}

func (r ProjectRepository) List(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	values, err := r.storage.listProjectConfigs(ctx)
	if err != nil {
		return nil, err
	}
	return capped(sorted(values, func(a, b projectmodel.Project) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	}), limit), nil
}
