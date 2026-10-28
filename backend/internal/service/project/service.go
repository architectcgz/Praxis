// Package project 负责 Project 与初始 Workspace 的读取和写入用例。
package project

import (
	"context"
	"errors"
	"praxis/internal/clock"
	projectmodel "praxis/internal/core/project"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"sync"
)

// WorkspaceDirectory 提供项目创建所需的目录操作能力。
type WorkspaceDirectory interface {
	Ensure(context.Context, string) (created bool, err error)
}

// Dependencies 包含创建 Project 持久化状态所需的依赖。
type Dependencies struct {
	Projects   repository.Project
	Workspaces repository.Workspace
	Clock      clock.Clock
	Directory  WorkspaceDirectory
}

// Service 负责 Project 创建与查询，默认工作区由 Project 派生。
type Service struct {
	createMu   sync.Mutex
	projects   repository.Project
	workspaces repository.Workspace
	clock      clock.Clock
	directory  WorkspaceDirectory
}

// CreateInput 包含输入边界已校验的身份及规范化后的项目名称、绝对路径。
type CreateInput struct {
	RequestID   identity.RequestID
	ProjectID   identity.ProjectID
	WorkspaceID identity.WorkspaceID
	Name        string
	Path        string
}

// CreateProjectResult 返回 Project 及其初始 Workspace。
type CreateProjectResult struct {
	Project   projectmodel.Project
	Workspace workspacemodel.Workspace
}

// NewService 创建 Project 业务服务。
func NewService(deps Dependencies) *Service {
	return &Service{
		projects:   deps.Projects,
		workspaces: deps.Workspaces,
		clock:      deps.Clock,
		directory:  deps.Directory,
	}
}

// CreateProject 使用已校验的输入创建目录并保存项目事实；后续失败保留已创建目录供重试。
func (s *Service) CreateProject(ctx context.Context, params CreateInput) (CreateProjectResult, error) {
	s.createMu.Lock()
	defer s.createMu.Unlock()
	projects, err := s.projects.List(ctx, 0)
	if err != nil {
		return CreateProjectResult{}, err
	}
	for _, existing := range projects {
		if existing.ID != params.ProjectID && (existing.Path == params.Path || existing.DefaultWorkspaceID == params.WorkspaceID) {
			return CreateProjectResult{}, errorcode.ErrRequestConflict
		}
	}

	_, err = s.directory.Ensure(ctx, params.Path)
	if err != nil {
		return CreateProjectResult{}, err
	}
	var result CreateProjectResult
	err = func(txCtx context.Context) error {
		existing, err := s.projects.Get(txCtx, params.ProjectID)
		if err == nil {
			workspace, workspaceErr := s.workspaces.Get(txCtx, params.WorkspaceID)
			if workspaceErr != nil {
				return workspaceErr
			}
			if existing.Name != params.Name || existing.Path != params.Path ||
				existing.DefaultWorkspaceID != params.WorkspaceID ||
				workspace.ProjectID != params.ProjectID || workspace.Path != params.Path {
				return errorcode.ErrRequestConflict
			}
			result = CreateProjectResult{Project: existing, Workspace: workspace}
			return nil
		}
		if !errors.Is(err, errorcode.ErrNotFound) {
			return err
		}
		if _, err := s.workspaces.Get(txCtx, params.WorkspaceID); err == nil {
			return errorcode.ErrRequestConflict
		} else if !errors.Is(err, errorcode.ErrNotFound) {
			return err
		}
		at := s.clock.Now().UTC()
		workspace := workspacemodel.NewWorkspace(
			params.WorkspaceID,
			params.ProjectID,
			workspacemodel.WorkspaceProjectRoot,
			params.Path,
			at,
		)
		project := projectmodel.NewProject(params.ProjectID, params.Name, params.Path, workspace.ID, at)
		if err := s.projects.Save(txCtx, project); err != nil {
			return err
		}
		result = CreateProjectResult{Project: project, Workspace: workspace}
		return nil
	}(ctx)
	return result, err
}

// ListProjects 返回按索引查询的 Project 列表。
func (s *Service) ListProjects(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	return s.projects.List(ctx, limit)
}

// ListWorkspaces 返回指定 Project 下按索引查询的 Workspace 列表。
func (s *Service) ListWorkspaces(ctx context.Context, projectID identity.ProjectID, limit int) ([]workspacemodel.Workspace, error) {
	return s.workspaces.ListByProject(ctx, projectID, limit)
}
