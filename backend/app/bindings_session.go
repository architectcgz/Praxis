package app

import (
	"context"
	domainfoundation "praxis/internal/domain/foundation"

	"praxis/internal/contracts"
)

type SessionBindings struct {
	runtime *bindingRuntime
	service serviceRef[SessionService]
}

// ListSessions exposes the durable sessions belonging to one Project.
func (b *SessionBindings) ListSessions(projectID string) (response []contracts.SessionSummary, err error) {
	done := b.runtime.begin("ListSessions")
	defer func() { done(err) }()
	ctx, service, err := b.bindingContext()
	if err != nil {
		return nil, err
	}
	sessions, err := service.ListSessionsByProject(ctx, domainfoundation.ProjectID(projectID), 100)
	if err != nil {
		return nil, publicBindingError(err)
	}
	result := make([]contracts.SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, contracts.SessionSummary{
			ID:          session.ID.String(),
			ProjectID:   session.ProjectID.String(),
			WorkspaceID: session.WorkspaceID.String(),
			Goal:        session.Goal,
			CreatedAt:   session.CreatedAt,
			UpdatedAt:   session.UpdatedAt,
		})
	}
	return result, nil
}

func (b *SessionBindings) CreateSession(
	request contracts.CreateSessionRequest,
) (response contracts.CreateSessionResponse, err error) {
	done := b.runtime.begin("CreateSession")
	defer func() { done(err) }()
	ctx, service, err := b.bindingContext()
	if err != nil {
		return contracts.CreateSessionResponse{}, err
	}
	result, err := service.CreateSessionForProject(
		ctx,
		domainfoundation.SessionID(request.SessionID),
		domainfoundation.AgentID(request.AgentID),
		domainfoundation.RequestID(request.RequestID),
		domainfoundation.ProjectID(request.ProjectID),
		domainfoundation.WorkspaceID(request.WorkspaceID),
		request.Goal,
	)
	if err != nil {
		return contracts.CreateSessionResponse{}, publicBindingError(err)
	}
	return contracts.CreateSessionResponse{
		SessionID:   result.Session.ID.String(),
		ProjectID:   result.Session.ProjectID.String(),
		WorkspaceID: result.Session.WorkspaceID.String(),
		AgentID:     result.Agent.ID.String(),
		Goal:        result.Session.Goal,
	}, nil
}

func (b *SessionBindings) GetSession(
	sessionID string,
) (response contracts.SessionSnapshot, err error) {
	done := b.runtime.begin("GetSession")
	defer func() { done(err) }()
	ctx, service, err := b.bindingContext()
	if err != nil {
		return contracts.SessionSnapshot{}, err
	}
	sessionView, err := service.GetSessionView(ctx, domainfoundation.SessionID(sessionID), 100)
	if err != nil {
		return contracts.SessionSnapshot{}, publicBindingError(err)
	}
	result := contracts.SessionSnapshot{
		ID:          sessionView.Session.ID.String(),
		ProjectID:   sessionView.Session.ProjectID.String(),
		WorkspaceID: sessionView.Session.WorkspaceID.String(),
		Goal:        sessionView.Session.Goal,
		CreatedAt:   sessionView.Session.CreatedAt,
		UpdatedAt:   sessionView.Session.UpdatedAt,
		Agents:      make([]contracts.AgentSnapshot, 0, len(sessionView.Agents)),
	}
	for _, agent := range sessionView.Agents {
		result.Agents = append(result.Agents, contracts.AgentSnapshot{
			ID: agent.ID.String(), SessionID: agent.SessionID.String(),
			SecurityPolicyRevision: agent.SecurityPolicyRevision,
			Profile:                string(agent.Profile), State: string(agent.State),
			CurrentExecution: agent.CurrentExecutionID.String(),
			Executions:       make([]contracts.ExecutionSnapshot, 0),
		})
	}
	return result, nil
}

func (b *SessionBindings) bindingContext() (context.Context, SessionService, error) {
	ctx, err := b.runtime.context()
	if err != nil {
		return nil, nil, err
	}
	service := b.service.get()
	if service == nil {
		return nil, nil, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	return ctx, service, nil
}
