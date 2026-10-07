package bindings

import (
	"praxis/internal/contracts"
	"praxis/internal/request"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

type SessionBindings struct {
	runtime Runtime
}

// ListSessions 返回指定项目的持久化会话。
func (b *SessionBindings) ListSessions(projectID string) ([]dto.SessionSummary, error) {
	if err := validation.ValidateProjectID(projectID); err != nil {
		return nil, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	sessions, err := services.Sessions.ListSessionsByProject(ctx, contracts.ProjectID(projectID), 100)
	if err != nil {
		return nil, publicError(b.runtime, "SessionBindings.ListSessions", err)
	}
	result := make([]dto.SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, dto.SessionSummary{
			ID:          session.ID.String(),
			ProjectID:   session.ProjectID.String(),
			WorkspaceID: session.WorkspaceID.String(),
			Title:       session.Title,
			CreatedAt:   session.CreatedAt,
			UpdatedAt:   session.UpdatedAt,
		})
	}
	return result, nil
}

func (b *SessionBindings) CreateSession(wire dto.CreateSessionRequest) (dto.CreateSessionResponse, error) {
	if err := validation.ValidateCreateSession(wire); err != nil {
		return dto.CreateSessionResponse{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.CreateSessionResponse{}, err
	}
	result, err := services.Sessions.CreateSession(ctx, request.CreateSession{
		SessionID:    contracts.SessionID(wire.SessionID),
		AgentID:      contracts.AgentID(wire.AgentID),
		RequestID:    contracts.RequestID(wire.RequestID),
		ProjectID:    contracts.ProjectID(wire.ProjectID),
		WorkspaceID:  contracts.WorkspaceID(wire.WorkspaceID),
		DefinitionID: contracts.AgentDefinitionID(wire.AgentDefinitionID),
	})
	if err != nil {
		return dto.CreateSessionResponse{}, publicError(b.runtime, "SessionBindings.CreateSession", err)
	}
	return dto.CreateSessionResponse{
		SessionID:         result.SessionID.String(),
		ProjectID:         result.ProjectID.String(),
		WorkspaceID:       result.WorkspaceID.String(),
		AgentID:           result.AgentID.String(),
		AgentDefinitionID: result.DefinitionID.String(),
	}, nil
}

func (b *SessionBindings) GetSession(sessionID string) (dto.SessionSnapshot, error) {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return dto.SessionSnapshot{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.SessionSnapshot{}, err
	}
	detail, err := services.Sessions.GetSessionDetail(ctx, contracts.SessionID(sessionID), 100)
	if err != nil {
		return dto.SessionSnapshot{}, publicError(b.runtime, "SessionBindings.GetSession", err)
	}
	result := dto.SessionSnapshot{
		ID:          detail.Session.ID.String(),
		ProjectID:   detail.Session.ProjectID.String(),
		WorkspaceID: detail.Session.WorkspaceID.String(),
		Title:       detail.Session.Title,
		CreatedAt:   detail.Session.CreatedAt,
		UpdatedAt:   detail.Session.UpdatedAt,
		Agents:      make([]dto.AgentSnapshot, 0, len(detail.Agents)),
	}
	for _, agent := range detail.Agents {
		result.Agents = append(result.Agents, agentSnapshot(agent))
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
		Records:              usageRecords(summary.Records),
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
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return err
	}
	if err := services.Sessions.DeleteSession(ctx, contracts.SessionID(sessionID)); err != nil {
		return publicError(b.runtime, "SessionBindings.DeleteSession", err)
	}
	return nil
}

// RenameSession 更新指定会话标题，并将业务错误转换为前端可识别的错误码。
func (b *SessionBindings) RenameSession(sessionID, title string) error {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return err
	}
	canonical, err := request.NewRenameSession(contracts.SessionID(sessionID), title)
	if err != nil {
		return publicError(b.runtime, "SessionBindings.RenameSession.request", err)
	}
	if err := services.Sessions.RenameSession(ctx, canonical); err != nil {
		return publicError(b.runtime, "SessionBindings.RenameSession", err)
	}
	return nil
}
