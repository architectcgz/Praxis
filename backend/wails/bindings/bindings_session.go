package bindings

import (
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/fault"
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"
)

type SessionBindings struct{ host Host }

// ListSessions 返回指定项目的持久化会话。
func (b *SessionBindings) ListSessions(projectID string) ([]dto.SessionSummary, error) {
	if err := validation.ValidateProjectID(projectID); err != nil {
		return nil, err
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	values, err := services.Sessions.ListSessionsByProject(ctx, identity.ProjectID(projectID), 100)
	if err != nil {
		return nil, publicError(b.host, "SessionBindings.ListSessions", err)
	}
	result := make([]dto.SessionSummary, 0, len(values))
	for _, value := range values {
		result = append(result, sessionSummary(value))
	}
	return result, nil
}

func (b *SessionBindings) CreateSession(wire dto.CreateSessionRequest) (dto.CreateSessionResponse, error) {
	if err := validation.ValidateCreateSession(wire); err != nil {
		return dto.CreateSessionResponse{}, err
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.CreateSessionResponse{}, err
	}
	params := sessionservice.CreateInput{
		SessionID:    identity.SessionID(wire.SessionID),
		AgentID:      identity.AgentID(wire.AgentID),
		RequestID:    identity.RequestID(wire.RequestID),
		ProjectID:    identity.ProjectID(wire.ProjectID),
		WorkspaceID:  identity.WorkspaceID(wire.WorkspaceID),
		DefinitionID: identity.AgentDefinitionID(wire.AgentDefinitionID),
	}
	result, err := services.Sessions.CreateSession(ctx, params)
	if err != nil {
		return dto.CreateSessionResponse{}, publicError(b.host, "SessionBindings.CreateSession", err)
	}
	return dto.CreateSessionResponse{
		SessionID:         result.Session.ID.String(),
		ProjectID:         result.Session.ProjectID.String(),
		WorkspaceID:       result.Session.WorkspaceID.String(),
		AgentID:           result.Agent.ID.String(),
		AgentDefinitionID: result.Agent.DefinitionID.String(),
	}, nil
}

func (b *SessionBindings) GetSession(sessionID string) (dto.SessionSnapshot, error) {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return dto.SessionSnapshot{}, err
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SessionSnapshot{}, err
	}
	view, err := services.Sessions.GetSessionView(ctx, identity.SessionID(sessionID), 100)
	if err != nil {
		return dto.SessionSnapshot{}, publicError(b.host, "SessionBindings.GetSession", err)
	}
	result := dto.SessionSnapshot{
		ID:          view.Session.ID.String(),
		ProjectID:   view.Session.ProjectID.String(),
		WorkspaceID: view.Session.WorkspaceID.String(),
		Title:       view.Session.Title,
		CreatedAt:   view.Session.CreatedAt,
		UpdatedAt:   view.Session.UpdatedAt,
		Agents:      make([]dto.AgentSnapshot, 0, len(view.Agents)),
	}
	for _, value := range view.Agents {
		result.Agents = append(result.Agents, agentSnapshot(value))
	}
	return result, nil
}

// GetSessionUsageSummary 直接投影业务侧的一致用量快照。
func (b *SessionBindings) GetSessionUsageSummary(sessionID string) (dto.SessionUsageSummary, error) {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return dto.SessionUsageSummary{}, err
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SessionUsageSummary{}, err
	}
	value, err := services.Usages.GetSessionUsageSummary(ctx, identity.SessionID(sessionID))
	if err != nil {
		return dto.SessionUsageSummary{}, publicError(b.host, "SessionBindings.GetSessionUsageSummary", err)
	}
	return dto.SessionUsageSummary{
		Records:              usageRecords(value.Records),
		InputTokens:          value.InputTokens,
		CacheReadInputTokens: value.CacheReadInputTokens,
		CacheReadRatio:       clonePointer(value.CacheReadRatio),
		CacheReadComplete:    value.CacheReadComplete,
	}, nil
}

func (b *SessionBindings) DeleteSession(sessionID string) error {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return err
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	if err := services.Sessions.DeleteSession(ctx, identity.SessionID(sessionID)); err != nil {
		return publicError(b.host, "SessionBindings.DeleteSession", err)
	}
	return nil
}

func (b *SessionBindings) RenameSession(sessionID, title string) error {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		err := fault.InvalidValue("title", "session title is required")
		return publicError(b.host, "SessionBindings.RenameSession.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	if err := services.Sessions.RenameSession(ctx, identity.SessionID(sessionID), title); err != nil {
		return publicError(b.host, "SessionBindings.RenameSession", err)
	}
	return nil
}

func sessionSummary(value sessionmodel.Session) dto.SessionSummary {
	return dto.SessionSummary{
		ID:          value.ID.String(),
		ProjectID:   value.ProjectID.String(),
		WorkspaceID: value.WorkspaceID.String(),
		Title:       value.Title,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}
}
