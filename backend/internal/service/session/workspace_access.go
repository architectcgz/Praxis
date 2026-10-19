package session

import (
	"context"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/fault"
	"praxis/internal/identity"
	projectservice "praxis/internal/service/project"
)

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
