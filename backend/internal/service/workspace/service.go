// Package workspace 负责工作区文件访问用例与会话范围校验。
package workspace

import (
	"context"
	"fmt"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	projectservice "praxis/internal/service/project"
	workspaceio "praxis/internal/workspace"
	"sync"
)

// Dependencies 包含会话范围解析和受限文件读取所需的能力。
type Dependencies struct {
	Projects   repository.Project
	Sessions   repository.Session
	Workspaces repository.Workspace
	TextReader workspaceio.TextReader
}

// Service 负责工作区文件访问，不管理会话生命周期。
type Service struct {
	saveMu     sync.Mutex
	projects   repository.Project
	sessions   repository.Session
	workspaces repository.Workspace
	textReader workspaceio.TextReader
}

// NewService 创建工作区文件访问服务。
func NewService(deps Dependencies) *Service {
	return &Service{
		projects:   deps.Projects,
		sessions:   deps.Sessions,
		workspaces: deps.Workspaces,
		textReader: deps.TextReader,
	}
}

func (s *Service) workspaceForSession(ctx context.Context, sessionID identity.SessionID) (workspacemodel.Workspace, error) {
	if err := ctx.Err(); err != nil {
		return workspacemodel.Workspace{}, err
	}
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return workspacemodel.Workspace{}, err
	}
	workspace, err := s.workspaces.Get(ctx, session.WorkspaceID)
	if err != nil {
		return workspacemodel.Workspace{}, err
	}
	if workspace.ProjectID != session.ProjectID || workspace.State != workspacemodel.WorkspaceReady {
		return workspacemodel.Workspace{}, fmt.Errorf("current session workspace is unavailable: %w", projectservice.ErrWorkspaceInvalid)
	}
	return workspace, nil
}

// SaveWorkspace 校验项目引用和目录冲突后保存工作区；项目根工作区由 Project 派生。
func (s *Service) SaveWorkspace(ctx context.Context, value workspacemodel.Workspace) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if _, err := s.projects.Get(ctx, value.ProjectID); err != nil {
		return err
	}
	projects, err := s.projects.List(ctx, 0)
	if err != nil {
		return err
	}
	for _, project := range projects {
		if project.DefaultWorkspaceID == value.ID {
			return errorcode.ErrRequestConflict
		}
	}
	workspaces, err := s.workspaces.ListByProject(ctx, value.ProjectID, 0)
	if err != nil {
		return err
	}
	for _, workspace := range workspaces {
		if workspace.ID != value.ID && workspace.Path == value.Path {
			return errorcode.ErrRequestConflict
		}
	}
	sessions, err := s.sessions.List(ctx, 0)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if session.WorkspaceID == value.ID && session.ProjectID != value.ProjectID {
			return errorcode.ErrRequestConflict
		}
	}
	return s.workspaces.Save(ctx, value)
}
