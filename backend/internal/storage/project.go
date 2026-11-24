package storage

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/project"
	"slices"
)

type ProjectRepository struct{ root string }

func (r ProjectRepository) Get(ctx context.Context, id identity.ProjectID) (project.Project, error) {
	if err := ctx.Err(); err != nil {
		return project.Project{}, err
	}
	path := projectConfigPath(r.root, id)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return project.Project{}, errorcode.ErrNotFound
		}
		return project.Project{}, fmt.Errorf("read project config %s: %w", path, err)
	}
	var value project.Project
	if err := decodeJSON(data, &value); err != nil {
		return project.Project{}, fmt.Errorf("decode project config %s: %w", path, err)
	}
	if err := value.Validate(); err != nil {
		return project.Project{}, fmt.Errorf("invalid project config %s: %w", path, err)
	}
	if value.ID != id {
		return project.Project{}, fmt.Errorf("project config %s: identity does not match its directory", path)
	}
	return value, nil
}

// Save 完整替换项目记录；同目录临时文件避免发布不完整配置。
func (r ProjectRepository) Save(ctx context.Context, value project.Project) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode project config: %w", err)
	}
	path := projectConfigPath(r.root, value.ID)
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

func (r ProjectRepository) List(ctx context.Context, limit int) ([]project.Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(r.root)
	if err != nil {
		return nil, fmt.Errorf("list project directories: %w", err)
	}
	values := make([]project.Project, 0)
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
	slices.SortFunc(values, func(a, b project.Project) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	})
	if limit > 0 {
		values = values[:min(len(values), limit)]
	}
	return values, nil
}

func projectConfigPath(projectsRoot string, id identity.ProjectID) string {
	return filepath.Join(projectsRoot, id.String(), "project.json")
}

type LocalDirectory struct{}

// EnsureExists 确保绝对路径是目录，并报告目标目录是否由本次调用创建。
func (LocalDirectory) EnsureExists(ctx context.Context, absolutePath string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	info, err := os.Stat(absolutePath)
	if err == nil {
		if !info.IsDir() {
			return false, errors.New("project path is not a directory")
		}
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect project directory: %w", err)
	}
	if err := os.MkdirAll(absolutePath, 0o700); err != nil {
		return false, fmt.Errorf("create project directory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	return true, nil
}
