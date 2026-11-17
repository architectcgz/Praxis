package bindings

import (
	"context"
	coordinator "praxis/internal/core/coordinator"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	approvalservice "praxis/internal/service/approval"
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
	result, err := services.Sessions.CreateSession(ctx, params)
	if err != nil {
		return dto.CreateSessionResponse{}, publicError(b.host, "SessionBindings.CreateSession", err)
	}
	return dto.CreateSessionResponse{
		SessionID: result.Session.ID.String(),
		ProjectID: result.Session.ProjectID.String(),
	}, nil
}

// SelectSession 完成会话加载并返回主历史、代理状态及队列快照，后续命令直接使用已加载实例。
func (b *SessionBindings) SelectSession(sessionID string) (dto.SessionSnapshot, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return dto.SessionSnapshot{}, publicInputError(b.host, "SessionBindings.SelectSession.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SessionSnapshot{}, err
	}
	view, err := services.Commands.SelectSession(ctx, identity.SessionID(sessionID))
	if err != nil {
		return dto.SessionSnapshot{}, publicError(b.host, "SessionBindings.SelectSession", err)
	}
	return b.sessionSnapshot(ctx, services, view)
}

func (b *SessionBindings) sessionSnapshot(ctx context.Context, services Services, view coordinator.SessionView) (dto.SessionSnapshot, error) {
	content, err := services.Commands.SessionContent(ctx, view.Session.ID)
	agents := content.Agents
	if err != nil {
		return dto.SessionSnapshot{}, publicError(b.host, "SessionBindings.sessionSnapshot.agents", err)
	}
	result := dto.SessionSnapshot{
		QueuePaused: view.QueuePaused,
		ID:          view.Session.ID.String(),
		ProjectID:   view.Session.ProjectID.String(),
		Title:       view.Session.Title,
		CreatedAt:   view.Session.CreatedAt,
		UpdatedAt:   view.Session.UpdatedAt,
		Agents:      publicAgents(agents),
		Tasks:       make([]dto.QueuedTask, 0, len(view.Tasks)),
		Entries:     map[string]dto.HistoryPage{view.Session.ID.String(): publicHistoryPage(content.Page)},
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
		TotalRequests:            value.TotalRequests,
		InputTokens:              value.InputTokens,
		OutputTokens:             value.OutputTokens,
		CacheReadInputTokens:     value.CacheReadInputTokens,
		CacheCreationInputTokens: value.CacheCreationInputTokens,
		ReportedRequests:         value.ReportedRequests,
		OutputReports:            value.OutputReports,
		CacheReadReports:         value.CacheReadReports,
		CacheCreationReports:     value.CacheCreationReports,
		CacheReadRatio:           value.CacheReadRatio,
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
	if err := services.Sessions.DeleteSession(ctx, identity.SessionID(sessionID)); err != nil {
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

func (b *SessionBindings) GetSessionSummary(sessionID string) (dto.SessionSummary, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return dto.SessionSummary{}, publicInputError(b.host, "GetSessionSummary", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SessionSummary{}, err
	}
	info, err := services.Sessions.GetSessionInfo(ctx, identity.SessionID(sessionID))
	if err != nil {
		return dto.SessionSummary{}, publicError(b.host, "GetSessionSummary", err)
	}
	return sessionSummary(info), nil
}

func (b *SessionBindings) CloseSession(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "CloseSession", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "CloseSession", services.Commands.CloseSession(ctx, identity.SessionID(sessionID)))
}

// ListModels 返回指定会话已加载的模型目录，不暴露凭据。
func (b *SessionBindings) ListModels(sessionID string) ([]dto.ModelOption, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return nil, publicInputError(b.host, "SessionBindings.ListModels.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	models, err := services.Sessions.ModelOptions(ctx, identity.SessionID(sessionID))
	if err != nil {
		return nil, publicError(b.host, "SessionBindings.ListModels", err)
	}
	result := make([]dto.ModelOption, 0, len(models))
	for _, option := range models {
		result = append(result, dto.ModelOption{
			ProviderID:            option.ProviderID,
			ModelID:               option.ModelID,
			Label:                 option.Label,
			ProviderName:          option.ProviderName,
			ReasoningLevels:       option.ReasoningLevels,
			DefaultReasoningLevel: option.DefaultReasoningLevel,
		})
	}
	return result, nil
}

// ReloadConfig 只重新加载指定会话；执行中拒绝重载，加载失败保留原配置。
func (b *SessionBindings) ReloadConfig(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "SessionBindings.ReloadConfig.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	if err := services.Commands.ReloadSessionConfig(ctx, identity.SessionID(sessionID)); err != nil {
		return publicError(b.host, "SessionBindings.ReloadConfig", err)
	}
	return nil
}

// SelectModel 只更新指定会话的选择；队列唤醒由 Coordinator 编排。
func (b *SessionBindings) SelectModel(wire dto.SelectModelRequest) error {
	wire.ProviderID = strings.TrimSpace(wire.ProviderID)
	wire.ModelID = strings.TrimSpace(wire.ModelID)
	wire.ReasoningLevel = strings.TrimSpace(wire.ReasoningLevel)
	if err := validation.Struct(wire); err != nil {
		return publicInputError(b.host, "SessionBindings.SelectModel.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	choice := sessionmodel.ModelChoice{
		ProviderID:     wire.ProviderID,
		ModelID:        wire.ModelID,
		ReasoningLevel: wire.ReasoningLevel,
	}
	if err := services.Commands.SelectModel(ctx, identity.SessionID(wire.SessionID), choice); err != nil {
		return publicError(b.host, "SessionBindings.SelectModel", err)
	}
	return nil
}

// GetModelSelection 返回会话自身的选择；查询不会设置模型或唤醒预约。
func (b *SessionBindings) GetModelSelection(sessionID string) (dto.ModelSelection, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return dto.ModelSelection{}, publicInputError(b.host, "SessionBindings.GetModelSelection.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.ModelSelection{}, err
	}
	choice, err := services.Sessions.SelectedModel(ctx, identity.SessionID(sessionID))
	if err != nil {
		return dto.ModelSelection{}, publicError(b.host, "SessionBindings.GetModelSelection", err)
	}
	return dto.ModelSelection{
		ProviderID:     choice.ProviderID,
		ModelID:        choice.ModelID,
		ReasoningLevel: choice.ReasoningLevel,
	}, nil
}

// RevealSessionFile 打开系统文件管理器定位会话日志；非法 ID、文件缺失或启动失败时返回公开错误。
func (b *SessionBindings) RevealSessionFile(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "SessionBindings.RevealSessionFile.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	path, err := services.SessionLogPath(ctx, identity.SessionID(sessionID))
	if err != nil {
		return publicError(b.host, "SessionBindings.RevealSessionFile", err)
	}
	command, err := revealFileCommand(path)
	if err != nil {
		return publicError(b.host, "SessionBindings.RevealSessionFile", err)
	}
	if err := command.Start(); err != nil {
		return publicError(b.host, "SessionBindings.RevealSessionFile", err)
	}
	// 文件管理器可能持续运行，不阻塞 binding；后台等待以释放子进程资源。
	go func() {
		if err := command.Wait(); err != nil {
			b.host.LogError("session file manager exited: %v", err)
		}
	}()
	return nil
}

// GetSessionApprovalMode 读取会话授权选择；空或非 canonical 会话身份返回校验错误。
func (b *SessionBindings) GetSessionApprovalMode(sessionID string) (dto.SessionApprovalMode, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return dto.SessionApprovalMode{}, publicInputError(b.host, "SessionBindings.GetSessionApprovalMode.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SessionApprovalMode{}, err
	}
	value, err := services.Approvals.GetApprovalMode(ctx, identity.SessionID(sessionID))
	if err != nil {
		return dto.SessionApprovalMode{}, publicError(b.host, "SessionBindings.GetSessionApprovalMode", err)
	}
	return approvalModeDTO(value), nil
}

// SetSessionApprovalMode 保存会话授权选择；null 恢复继承，有效模式在会话显式 reload 后更新。
func (b *SessionBindings) SetSessionApprovalMode(wire dto.SetSessionApprovalModeRequest) (dto.SessionApprovalMode, error) {
	if wire.Mode != nil {
		wire.Mode = new(strings.TrimSpace(*wire.Mode))
	}
	if err := validation.Struct(wire); err != nil {
		return dto.SessionApprovalMode{}, publicInputError(b.host, "SessionBindings.SetSessionApprovalMode.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SessionApprovalMode{}, err
	}
	var mode *security.ApprovalMode
	if wire.Mode != nil {
		mode = new(security.ApprovalMode(*wire.Mode))
	}
	value, err := services.Approvals.SetApprovalMode(ctx, identity.SessionID(wire.SessionID), mode)
	if err != nil {
		return dto.SessionApprovalMode{}, publicError(b.host, "SessionBindings.SetSessionApprovalMode", err)
	}
	return approvalModeDTO(value), nil
}

func approvalModeDTO(value approvalservice.ApprovalSettings) dto.SessionApprovalMode {
	result := dto.SessionApprovalMode{
		SessionID:     value.SessionID.String(),
		GlobalMode:    string(value.GlobalMode),
		EffectiveMode: string(value.EffectiveMode),
	}
	if value.SessionMode != nil {
		result.SessionMode = new(string(*value.SessionMode))
	}
	return result
}
