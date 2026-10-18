package session

import (
	"context"
	"praxis/internal/fault"
	"praxis/internal/identity"
	projectmodel "praxis/internal/service/project"
	workspacemodel "praxis/internal/workspace"
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
		return workspacemodel.Workspace{}, fault.New(projectmodel.ProjectWorkspaceInvalid, "current session workspace is unavailable")
	}
	return workspace, nil
}
