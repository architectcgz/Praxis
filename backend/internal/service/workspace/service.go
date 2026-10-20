// Package workspace 负责工作区文件访问用例与会话范围校验。
package workspace

import (
	"context"
	"fmt"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/repository"
	projectservice "praxis/internal/service/project"
	workspaceio "praxis/internal/workspace"
)

// Dependencies 包含会话范围解析和受限文件读取所需的能力。
type Dependencies struct {
	Sessions   repository.Session
	Workspaces repository.Workspace
	TextReader workspaceio.TextReader
}

// Service 负责工作区文件访问，不管理会话生命周期。
type Service struct {
	sessions   repository.Session
	workspaces repository.Workspace
	textReader workspaceio.TextReader
}

// NewService 创建工作区文件访问服务。
func NewService(deps Dependencies) (*Service, error) {
	for name, value := range map[string]any{
		"sessions":    deps.Sessions,
		"workspaces":  deps.Workspaces,
		"text reader": deps.TextReader,
	} {
		if value == nil {
			return nil, fmt.Errorf("workspace service %s is required", name)
		}
	}
	return &Service{
		sessions:   deps.Sessions,
		workspaces: deps.Workspaces,
		textReader: deps.TextReader,
	}, nil
}

func (s *Service) workspaceForSession(ctx context.Context, sessionID identity.SessionID) (workspacemodel.Workspace, error) {
	if ctx == nil {
		return workspacemodel.Workspace{}, fault.New(fault.InvalidRequest, "a valid session is required")
	}
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
		return workspacemodel.Workspace{}, fault.New(projectservice.ProjectWorkspaceInvalid, "current session workspace is unavailable")
	}
	return workspace, nil
}
