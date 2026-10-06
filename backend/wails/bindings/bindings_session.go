package bindings

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"

	"praxis/wails/dto"
	"praxis/wails/validation"
)

type SessionBindings struct {
	runtime Runtime
}

// ListSessions exposes the durable sessions belonging to one Project.
func (b *SessionBindings) ListSessions(projectID string) ([]dto.SessionSummary, error) {
	if err := validation.ValidateProjectID(projectID); err != nil {
		return nil, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	sessions, err := service.Sessions.ListSessionsByProject(ctx, contracts.ProjectID(projectID), 100)
	if err != nil {
		return nil, publicError(b.runtime, "SessionBindings.ListSessions", err)
	}
	result := make([]dto.SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, dto.SessionSummary{
			ID: session.ID.String(), ProjectID: session.ProjectID.String(),
			WorkspaceID: session.WorkspaceID.String(), Title: session.Title,
			CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt,
		})
	}
	return result, nil
}

func (b *SessionBindings) CreateSession(request dto.CreateSessionRequest) (dto.CreateSessionResponse, error) {
	if err := validation.ValidateCreateSession(request); err != nil {
		return dto.CreateSessionResponse{}, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.CreateSessionResponse{}, err
	}
	result, err := service.Sessions.CreateSessionForProject(
		ctx,
		contracts.SessionID(request.SessionID),
		contracts.AgentID(request.AgentID),
		contracts.RequestID(request.RequestID),
		contracts.ProjectID(request.ProjectID),
		contracts.WorkspaceID(request.WorkspaceID),
		contracts.AgentDefinitionID(request.AgentDefinitionID),
	)
	if err != nil {
		return dto.CreateSessionResponse{}, publicError(b.runtime, "SessionBindings.CreateSession", err)
	}
	return dto.CreateSessionResponse{
		SessionID: result.Session.ID.String(), ProjectID: result.Session.ProjectID.String(),
		WorkspaceID: result.Session.WorkspaceID.String(), AgentID: result.Agent.ID.String(),
		AgentDefinitionID: result.Agent.DefinitionID.String(),
	}, nil
}

func (b *SessionBindings) GetSession(sessionID string) (dto.SessionSnapshot, error) {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return dto.SessionSnapshot{}, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.SessionSnapshot{}, err
	}
	view, err := service.Sessions.GetSessionView(ctx, contracts.SessionID(sessionID), 100)
	if err != nil {
		return dto.SessionSnapshot{}, publicError(b.runtime, "SessionBindings.GetSession", err)
	}
	result := dto.SessionSnapshot{
		ID: view.Session.ID.String(), ProjectID: view.Session.ProjectID.String(),
		WorkspaceID: view.Session.WorkspaceID.String(), Title: view.Session.Title,
		CreatedAt: view.Session.CreatedAt, UpdatedAt: view.Session.UpdatedAt,
		Agents: make([]dto.AgentSnapshot, 0, len(view.Agents)),
	}
	for _, agent := range view.Agents {
		result.Agents = append(result.Agents, dto.AgentSnapshot{
			ID: agent.ID.String(), Name: agentmodel.DisplayName(agent.DefinitionID), SessionID: agent.SessionID.String(),
			DefinitionID:           agent.DefinitionID.String(),
			SecurityPolicyRevision: agent.SecurityPolicyRevision,
			Profile:                string(agent.Profile), State: string(agent.State),
			CurrentTask: agent.CurrentTaskID.String(),
			Tasks:       make([]dto.TaskSnapshot, 0),
		})
	}
	return result, nil
}

// GetSessionUsageSummary 查询已保存用量，非法 Session ID 在 binding 边界拒绝。
func (b *SessionBindings) GetSessionUsageSummary(sessionID string) (dto.SessionUsageSummary, error) {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return dto.SessionUsageSummary{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.SessionUsageSummary{}, err
	}
	summary, err := services.Sessions.GetSessionUsageSummary(ctx, contracts.SessionID(sessionID))
	if err != nil {
		return dto.SessionUsageSummary{}, publicError(b.runtime, "SessionBindings.GetSessionUsageSummary", err)
	}
	return dto.SessionUsageSummary{
		Records:              summary.Records,
		InputTokens:          summary.InputTokens,
		CacheReadInputTokens: summary.CacheReadInputTokens,
		CacheReadRatio:       summary.CacheReadRatio,
		CacheReadComplete:    summary.CacheReadComplete,
	}, nil
}

// DeleteSession 删除指定会话，并将业务错误转换为前端可识别的错误码。
func (b *SessionBindings) DeleteSession(sessionID string) error {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return err
	}
	if err := service.Sessions.DeleteSession(ctx, contracts.SessionID(sessionID)); err != nil {
		return publicError(b.runtime, "SessionBindings.DeleteSession", err)
	}
	return nil
}

// RenameSession 更新指定会话标题，并将业务错误转换为前端可识别的错误码。
func (b *SessionBindings) RenameSession(sessionID, title string) error {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return err
	}
	if err := service.Sessions.RenameSession(ctx, contracts.SessionID(sessionID), title); err != nil {
		return publicError(b.runtime, "SessionBindings.RenameSession", err)
	}
	return nil
}
