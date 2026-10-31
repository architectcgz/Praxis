package storage

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"slices"
)

type ProjectRepository struct{ dataRoot string }

var _ repository.Project = ProjectRepository{}

func (r ProjectRepository) Get(ctx context.Context, id identity.ProjectID) (projectmodel.Project, error) {
	if err := ctx.Err(); err != nil {
		return projectmodel.Project{}, err
	}
	path := projectConfigPath(r.dataRoot, id)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return projectmodel.Project{}, errorcode.ErrNotFound
		}
		return projectmodel.Project{}, fmt.Errorf("read project config %s: %w", path, err)
	}
	var value projectmodel.Project
	if err := decodeJSON(data, &value); err != nil {
		return projectmodel.Project{}, fmt.Errorf("decode project config %s: %w", path, err)
	}
	if err := value.Validate(); err != nil {
		return projectmodel.Project{}, fmt.Errorf("invalid project config %s: %w", path, err)
	}
	if value.ID != id {
		return projectmodel.Project{}, fmt.Errorf("project config %s: identity does not match its directory", path)
	}
	return value, nil
}

// Save 完整替换项目记录；同目录临时文件避免发布不完整配置。
func (r ProjectRepository) Save(ctx context.Context, value projectmodel.Project) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode project config: %w", err)
	}
	path := projectConfigPath(r.dataRoot, value.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create project config directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "project.json.*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary project config: %w", err)
	}
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}()
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write project config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("flush project config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close project config: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("replace project config %s: %w", path, err)
	}
	return nil
}

func (r ProjectRepository) List(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(r.dataRoot, "projects"))
	if err != nil {
		return nil, fmt.Errorf("list project directories: %w", err)
	}
	values := make([]projectmodel.Project, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		value, err := r.Get(ctx, identity.ProjectID(entry.Name()))
		if errors.Is(err, errorcode.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	slices.SortFunc(values, func(a, b projectmodel.Project) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	})
	if limit > 0 {
		values = values[:min(len(values), limit)]
	}
	return values, nil
}

func projectConfigPath(dataRoot string, id identity.ProjectID) string {
	return filepath.Join(dataRoot, "projects", id.String(), "project.json")
}
