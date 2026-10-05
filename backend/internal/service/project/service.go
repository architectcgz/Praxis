// Package project 负责 Project 与初始 Workspace 的读取和写入用例。
package project

import (
	"praxis/internal/contracts"
	projectmodel "praxis/internal/core/project"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/utils/pathutil"

	"context"
	"errors"
	"fmt"
	"strings"

	"praxis/internal/repository"
	"praxis/internal/system"
)

// Config 包含创建 Project 持久化状态所需的依赖。
type Config struct {
	Transactions repository.TxRunner
	Projects     repository.ProjectRepository
	Workspaces   repository.WorkspaceRepository
	Clock        system.Clock
}

// Service owns Project mutations and their transaction boundaries.
type Service struct {
	tx         repository.TxRunner
	projects   repository.ProjectRepository
	workspaces repository.WorkspaceRepository
	clock      system.Clock
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
		"transactions": config.Transactions,
		"projects":     config.Projects,
		"workspaces":   config.Workspaces,
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
	}, nil
}

// CreateProject 在一个事务内持久化 Project、初始 Workspace 和幂等结果。
func (s *Service) CreateProject(ctx context.Context, params CreateProjectParams) (CreateProjectResult, error) {
	if ctx == nil {
		return CreateProjectResult{}, errors.New("create project context is required")
	}
	if params.ProjectID == "" || params.WorkspaceID == "" {
		return CreateProjectResult{}, contracts.New(contracts.InvalidRequest, "")
	}
	if params.Name == "" || strings.ContainsAny(params.Name, "\x00\r\n") || !pathutil.IsAbsoluteNormalized(params.Path) {
		return CreateProjectResult{}, contracts.New(contracts.ProjectWorkspaceInvalid, "")
	}
	var result CreateProjectResult
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
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
	if projectID == "" {
		return nil, contracts.New(contracts.InvalidRequest, "project id is required")
	}
	return s.workspaces.ListByProject(ctx, projectID, limit)
}
