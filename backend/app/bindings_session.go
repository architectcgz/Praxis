package app

import (
	"context"

	"praxis/internal/contracts"
	"praxis/internal/core/domain"
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
	sessions, err := service.ListSessionsByProject(ctx, domain.ProjectID(projectID), 100)
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
		domain.ProjectID(request.ProjectID),
		domain.WorkspaceID(request.WorkspaceID),
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
	projection, err := service.ProjectSession(ctx, domain.SessionID(sessionID), 100)
	if err != nil {
		return contracts.SessionSnapshot{}, publicBindingError(err)
	}
	result := contracts.SessionSnapshot{
		ID:          projection.Session.ID.String(),
		ProjectID:   projection.Session.ProjectID.String(),
		WorkspaceID: projection.Session.WorkspaceID.String(),
		Goal:        projection.Session.Goal,
		CreatedAt:   projection.Session.CreatedAt,
		UpdatedAt:   projection.Session.UpdatedAt,
		Groups:      make([]contracts.GroupSnapshot, 0, len(projection.Groups)),
		Agents:      make([]contracts.AgentSnapshot, 0, len(projection.Agents)),
	}
	for _, group := range projection.Groups {
		result.Groups = append(result.Groups, contracts.GroupSnapshot{
			ID:             group.ID.String(),
			PrimaryAgentID: group.PrimaryAgentID.String(),
			MaxConcurrent:  group.MaxConcurrent,
		})
	}
	for _, agent := range projection.Agents {
		result.Agents = append(result.Agents, contracts.AgentSnapshot{
			ID:               agent.ID.String(),
			SessionID:        agent.SessionID.String(),
			GroupID:          agent.GroupID.String(),
			Profile:          string(agent.Profile),
			State:            string(agent.State),
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
