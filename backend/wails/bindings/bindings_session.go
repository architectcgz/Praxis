package bindings

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	coordinator "praxis/internal/service/coordinator"
	sessionservice "praxis/internal/service/session"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"
)

type SessionBindings struct{ host Host }

// ListSessions 返回指定项目的持久化会话。
func (b *SessionBindings) ListSessions(projectID string) ([]dto.SessionSummary, error) {
	if err := validation.Value("projectId", projectID, "project_id"); err != nil {
		return nil, publicInputError(b.host, "SessionBindings.ListSessions.request", err)
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
	if err := validation.Struct(wire); err != nil {
		return dto.CreateSessionResponse{}, publicInputError(b.host, "SessionBindings.CreateSession.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.CreateSessionResponse{}, err
	}
	params := sessionservice.CreateInput{
		SessionID: identity.SessionID(wire.SessionID),
		RequestID: identity.RequestID(wire.RequestID),
		ProjectID: identity.ProjectID(wire.ProjectID),
	}
	result, err := services.Commands.CreateSession(ctx, params)
	if err != nil {
		return dto.CreateSessionResponse{}, publicError(b.host, "SessionBindings.CreateSession", err)
	}
	return dto.CreateSessionResponse{
		SessionID: result.Session.ID.String(),
		ProjectID: result.Session.ProjectID.String(),
	}, nil
}

// SelectSession 完成会话数据加载并返回当前分支的计时快照，后续命令直接使用已加载实例。
func (b *SessionBindings) SelectSession(sessionID, branchID, bindingID, pageID string) (dto.SessionSnapshot, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return dto.SessionSnapshot{}, publicInputError(b.host, "SessionBindings.SelectSession.request", err)
	}
	if err := validation.Value("branchId", branchID, "omitempty,id"); err != nil {
		return dto.SessionSnapshot{}, publicInputError(b.host, "SessionBindings.SelectSession.request", err)
	}
	if err := validation.Value("bindingId", bindingID, "omitempty,id"); err != nil {
		return dto.SessionSnapshot{}, publicInputError(b.host, "SessionBindings.SelectSession.request", err)
	}
	if err := validation.ID("pageId", pageID); err != nil {
		return dto.SessionSnapshot{}, publicInputError(b.host, "SessionBindings.SelectSession.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SessionSnapshot{}, err
	}
	view, err := services.Commands.SelectSession(ctx, identity.SessionID(sessionID), identity.BranchID(branchID), bindingID, pageID)
	if err != nil {
		return dto.SessionSnapshot{}, publicError(b.host, "SessionBindings.SelectSession", err)
	}
	return b.sessionSnapshot(ctx, services, view)
}

func (b *SessionBindings) sessionSnapshot(ctx context.Context, services Services, view coordinator.BindingView) (dto.SessionSnapshot, error) {
	agents, err := services.Agents.ListSessionAgents(ctx, view.Session.ID, view.Branch.ID, 100)
	if err != nil {
		return dto.SessionSnapshot{}, publicError(b.host, "SessionBindings.sessionSnapshot.agents", err)
	}
	result := dto.SessionSnapshot{
		BindingID: view.BindingID,
		Branch:    view.Branch,
		ID:        view.Session.ID.String(),
		ProjectID: view.Session.ProjectID.String(),
		Title:     view.Session.Title,
		CreatedAt: view.Session.CreatedAt,
		UpdatedAt: view.Session.UpdatedAt,
		Agents:    agents,
		Tasks:     make([]dto.QueuedTask, 0, len(view.Tasks)),
		Timings:   make([]dto.OperationTiming, 0),
		Histories: make(map[string][]dto.Entry, len(agents)),
	}
	for _, agent := range agents {
		entries, err := services.Commands.ListAgentEntries(ctx, view.Session.ID, agent.ID, view.Branch.ID)
		if err != nil {
			return dto.SessionSnapshot{}, publicError(b.host, "SessionBindings.sessionSnapshot.entries", err)
		}
		result.Histories[agent.ID.String()] = publicEntries(entries)
		records, err := services.Timings.ListAgentTimings(ctx, view.Session.ID, agent.ID, view.Branch.ID, 0)
		if err != nil {
			return dto.SessionSnapshot{}, publicError(b.host, "SessionBindings.sessionSnapshot.timings", err)
		}
		for _, record := range records {
			result.Timings = append(result.Timings, operationTiming(record))
		}
	}
	for _, value := range view.Tasks {
		result.Tasks = append(result.Tasks, dto.QueuedTask{
			ID:        value.ID.String(),
			Prompt:    value.Prompt,
			Sequence:  value.Sequence,
			CreatedAt: value.CreatedAt,
		})
	}
	return result, nil
}

// GetSessionUsageSummary 直接投影业务侧的一致用量快照。
func (b *SessionBindings) GetSessionUsageSummary(sessionID string) (dto.SessionUsageSummary, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return dto.SessionUsageSummary{}, publicInputError(b.host, "SessionBindings.GetSessionUsageSummary.request", err)
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
		Records:                  usageRecords(value.Records),
		InputTokens:              value.InputTokens,
		OutputTokens:             value.OutputTokens,
		CacheReadInputTokens:     value.CacheReadInputTokens,
		CacheCreationInputTokens: value.CacheCreationInputTokens,
		ReportedRequests:         value.ReportedRequests,
		OutputReports:            value.OutputReports,
		CacheReadReports:         value.CacheReadReports,
		CacheCreationReports:     value.CacheCreationReports,
		CacheReadRatio:           clonePointer(value.CacheReadRatio),
		CacheReadComplete:        value.CacheReadComplete,
	}, nil
}

func (b *SessionBindings) DeleteSession(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "SessionBindings.DeleteSession.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	if err := services.Commands.DeleteSession(ctx, identity.SessionID(sessionID)); err != nil {
		return publicError(b.host, "SessionBindings.DeleteSession", err)
	}
	return nil
}

func (b *SessionBindings) RenameSession(sessionID, title string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "SessionBindings.RenameSession.request", err)
	}
	title = strings.TrimSpace(title)
	if err := validation.Value("title", title, "required,notblank"); err != nil {
		return publicInputError(b.host, "SessionBindings.RenameSession.request", err)
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

func sessionSummary(value sessionmodel.SessionInfo) dto.SessionSummary {
	return dto.SessionSummary{
		ID:        value.ID.String(),
		ProjectID: value.ProjectID.String(),
		Title:     value.Title,
		CreatedAt: value.CreatedAt,
		UpdatedAt: value.UpdatedAt,
	}
}
