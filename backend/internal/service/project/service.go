// Package project 负责 Project 与初始 Workspace 的读取和写入用例。
package project

import (
	"praxis/internal/contracts"
	projectmodel "praxis/internal/core/project"
	workspacemodel "praxis/internal/core/workspace"

	"context"
	"errors"
	"fmt"

	"praxis/internal/logging"
	"praxis/internal/repository"
	"praxis/internal/system"
)

// WorkspaceDirectory 是项目创建所需的最小目录 Port。
type WorkspaceDirectory interface {
	Ensure(context.Context, string) (created bool, err error)
	RemoveEmpty(context.Context, string) error
}

// Config 包含创建 Project 持久化状态所需的依赖。
type Config struct {
	Transactions repository.TxRunner
	Projects     repository.ProjectRepository
	Workspaces   repository.WorkspaceRepository
	Clock        system.Clock
	Directory    WorkspaceDirectory
	Logger       *logging.Logger
}

// Service owns Project mutations and their transaction boundaries.
type Service struct {
	tx         repository.TxRunner
	projects   repository.ProjectRepository
	workspaces repository.WorkspaceRepository
	clock      system.Clock
	directory  WorkspaceDirectory
	logger     *logging.Logger
}

// CreateProjectParams contains the durable identity and initial workspace
// data for a new Project.
type CreateProjectParams struct {
	RequestID   contracts.RequestID
	ProjectID   contracts.ProjectID
	WorkspaceID contracts.WorkspaceID
	Name        string
	Path        string
}

// CreateProjectResult returns the Project and its initial Workspace.
type CreateProjectResult struct {
	Project   projectmodel.Project
	Workspace workspacemodel.Workspace
}

// NewService creates the Project application service.
func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions":        config.Transactions,
		"projects":            config.Projects,
		"workspaces":          config.Workspaces,
		"workspace directory": config.Directory,
	} {
		if value == nil {
			return nil, fmt.Errorf("project service %s is required", name)
		}
	}
	return &Service{
		tx:         config.Transactions,
		projects:   config.Projects,
		workspaces: config.Workspaces,
		clock:      system.ClockOrDefault(config.Clock),
		directory:  config.Directory,
		logger:     logging.NewFactory().Ensure(config.Logger),
	}, nil
}

// CreateProject 在一个事务内持久化 Project、初始 Workspace 和幂等结果。
func (s *Service) CreateProject(ctx context.Context, params CreateProjectParams) (CreateProjectResult, error) {
	if ctx == nil {
		return CreateProjectResult{}, errors.New("create project context is required")
	}
	createdDirectory, err := s.directory.Ensure(ctx, params.Path)
	if err != nil {
		return CreateProjectResult{}, err
	}
	var result CreateProjectResult
	err = s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.projects.Get(txCtx, params.ProjectID)
		if err == nil {
			workspace, workspaceErr := s.workspaces.Get(txCtx, params.WorkspaceID)
			if workspaceErr != nil {
				return workspaceErr
			}
			if existing.Name != params.Name || existing.Path != params.Path ||
				existing.DefaultWorkspaceID != params.WorkspaceID ||
				workspace.ProjectID != params.ProjectID || workspace.Path != params.Path {
				return contracts.ErrRequestConflict
			}
			result = CreateProjectResult{Project: existing, Workspace: workspace}
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		if _, err := s.workspaces.Get(txCtx, params.WorkspaceID); err == nil {
			return contracts.ErrRequestConflict
		} else if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		at := s.clock.Now().UTC()
		workspace, err := workspacemodel.NewWorkspace(
			params.WorkspaceID,
			params.ProjectID,
			workspacemodel.WorkspaceProjectRoot,
			params.Path,
			at,
		)
		if err != nil {
			return fmt.Errorf("create workspace: %w", err)
		}
		project, err := projectmodel.NewProject(params.ProjectID, params.Name, params.Path, workspace.ID, at)
		if err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		if err := s.projects.Save(txCtx, project); err != nil {
			return err
		}
		if err := s.workspaces.Save(txCtx, workspace); err != nil {
			return err
		}
		result = CreateProjectResult{Project: project, Workspace: workspace}
		return nil
	})
	if err != nil && createdDirectory {
		if removeErr := s.directory.RemoveEmpty(context.WithoutCancel(ctx), params.Path); removeErr != nil {
			s.logger.Warnf("回收未落库的项目目录失败 path=%s err=%v", params.Path, removeErr)
		}
	}
	return result, err
}

// ListProjects 返回按索引查询的 Project 列表。
func (s *Service) ListProjects(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	if ctx == nil {
		return nil, errors.New("project catalog context is required")
	}
	lister, ok := s.projects.(repository.ProjectListRepository)
	if !ok {
		return nil, errors.New("project catalog is unavailable")
	}
	return lister.List(ctx, limit)
}

// ListWorkspaces 返回指定 Project 下按索引查询的 Workspace 列表。
func (s *Service) ListWorkspaces(ctx context.Context, projectID contracts.ProjectID, limit int) ([]workspacemodel.Workspace, error) {
	if ctx == nil {
		return nil, errors.New("workspace catalog context is required")
	}
	return s.workspaces.ListByProject(ctx, projectID, limit)
}
