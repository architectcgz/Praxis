package session

import (
	"context"

	"praxis/internal/contracts"
	workspacemodel "praxis/internal/core/workspace"
)

func (s *Service) workspaceForSession(ctx context.Context, sessionID contracts.SessionID) (workspacemodel.Workspace, error) {
	if ctx == nil || sessionID == "" {
		return workspacemodel.Workspace{}, contracts.New(contracts.InvalidRequest, "需要有效会话。")
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
	if err := workspace.Validate(); err != nil {
		return workspacemodel.Workspace{}, err
	}
	if workspace.ProjectID != session.ProjectID || workspace.State != workspacemodel.WorkspaceReady {
		return workspacemodel.Workspace{}, contracts.New(contracts.ProjectWorkspaceInvalid, "当前会话的工作区不可用。")
	}
	return workspace, nil
}
