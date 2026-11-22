package bindings

import (
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
	"praxis/wails/validation"
	"strings"
	"time"
)

type SessionBindings struct{ host Host }

type SessionSnapshot struct {
	QueuePaused      bool                `json:"queuePaused"`
	ID               string              `json:"id"`
	ProjectID        string              `json:"projectId"`
	Title            string              `json:"title"`
	CreatedAt        time.Time           `json:"createdAt"`
	UpdatedAt        time.Time           `json:"updatedAt"`
	Tasks            []QueuedTask        `json:"tasks"`
	History          HistoryPage         `json:"history"`
	Models           []ModelOption       `json:"models"`
	ModelSelection   ModelSelection      `json:"modelSelection"`
	ApprovalMode     SessionApprovalMode `json:"approvalMode"`
	PendingApprovals []ToolApproval      `json:"pendingApprovals"`
}

type SessionSummary struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CreateSessionRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	ProjectID string `json:"projectId" validate:"project_id"`
}

type CreateSessionResponse struct {
	SessionID string `json:"sessionId"`
	ProjectID string `json:"projectId"`
}

type ModelOption struct {
	ProviderID            string   `json:"providerId"`
	ModelID               string   `json:"modelId"`
	Label                 string   `json:"label"`
	ProviderName          string   `json:"providerName"`
	ReasoningLevels       []string `json:"reasoningLevels"`
	DefaultReasoningLevel string   `json:"defaultReasoningLevel"`
}

// SelectModelRequest 只向指定会话提交完整的模型选择。
type SelectModelRequest struct {
	SessionID      string `json:"sessionId" validate:"session_id"`
	ProviderID     string `json:"providerId" validate:"required,config_id"`
	ModelID        string `json:"modelId" validate:"required,id"`
	ReasoningLevel string `json:"reasoningLevel" validate:"omitempty,id"`
}

// ModelSelection 只返回会话选择的公开字段，不包含完整模型配置。
type ModelSelection struct {
	ProviderID     string `json:"providerId"`
	ModelID        string `json:"modelId"`
	ReasoningLevel string `json:"reasoningLevel"`
}

// SetSessionApprovalModeRequest 设置会话覆盖；mode=null 表示继承全局授权模式。
type SetSessionApprovalModeRequest struct {
	SessionID string  `json:"sessionId" validate:"session_id"`
	Mode      *string `json:"mode" validate:"omitempty,oneof=ask auto yolo"`
}

// SessionApprovalMode 返回已保存的会话覆盖和当前已加载的有效模式；二者可在 reload 前不同。
type SessionApprovalMode struct {
	SessionID     string  `json:"sessionId"`
	GlobalMode    string  `json:"globalMode"`
	SessionMode   *string `json:"sessionMode"`
	EffectiveMode string  `json:"effectiveMode"`
}

// ListSessions 返回指定项目的持久化会话。
func (b *SessionBindings) ListSessions(projectID string) ([]SessionSummary, error) {
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
	result := make([]SessionSummary, 0, len(values))
	for _, value := range values {
		result = append(result, sessionSummary(value))
	}
	return result, nil
}

func (b *SessionBindings) CreateSession(wire CreateSessionRequest) (CreateSessionResponse, error) {
	if err := validation.Struct(wire); err != nil {
		return CreateSessionResponse{}, publicInputError(b.host, "SessionBindings.CreateSession.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return CreateSessionResponse{}, err
	}
	params := sessionservice.CreateInput{
		SessionID: identity.SessionID(wire.SessionID),
		ProjectID: identity.ProjectID(wire.ProjectID),
	}
	result, err := services.Sessions.CreateSession(ctx, params)
	if err != nil {
		return CreateSessionResponse{}, publicError(b.host, "SessionBindings.CreateSession", err)
	}
	return CreateSessionResponse{
		SessionID: result.Session.ID.String(),
		ProjectID: result.Session.ProjectID.String(),
	}, nil
}

// SelectSession 加载会话并返回完整初始快照（首屏历史、待办队列、模型目录、模型选择、审批模式与挂起审批）。
func (b *SessionBindings) SelectSession(sessionID string) (SessionSnapshot, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return SessionSnapshot{}, publicInputError(b.host, "SessionBindings.SelectSession.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return SessionSnapshot{}, err
	}
	view, err := services.Sessions.SelectSession(ctx, identity.SessionID(sessionID))
	if err != nil {
		return SessionSnapshot{}, publicError(b.host, "SessionBindings.SelectSession", err)
	}
	page, err := services.Sessions.AgentHistory(ctx, view.Session.ID, view.Session.MainAgentID(), nil, defaultHistoryLimit)
	if err != nil {
		return SessionSnapshot{}, publicError(b.host, "SessionBindings.SelectSession.history", err)
	}
	result := SessionSnapshot{
		QueuePaused: view.QueuePaused,
		ID:          view.Session.ID.String(),
		ProjectID:   view.Session.ProjectID.String(),
		Title:       view.Session.Title,
		CreatedAt:   view.Session.CreatedAt,
		UpdatedAt:   view.Session.UpdatedAt,
		Tasks:       make([]QueuedTask, 0, len(view.Tasks)),
		History:     publicHistoryPage(page),
		Models:      publicModelOptions(view.Models),
		ModelSelection: ModelSelection{
			ProviderID:     view.ModelChoice.ProviderID,
			ModelID:        view.ModelChoice.ModelID,
			ReasoningLevel: view.ModelChoice.ReasoningLevel,
		},
		ApprovalMode:     publicApprovalMode(view.Approval),
		PendingApprovals: publicToolApprovals(view.PendingApprovals),
	}
	for _, value := range view.Tasks {
		result.Tasks = append(result.Tasks, QueuedTask{
			ID:        value.ID.String(),
			Prompt:    value.Prompt,
			Sequence:  value.Sequence,
			CreatedAt: value.CreatedAt,
		})
	}
	return result, nil
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

func sessionSummary(value sessionmodel.SessionInfo) SessionSummary {
	return SessionSummary{
		ID:        value.ID.String(),
		ProjectID: value.ProjectID.String(),
		Title:     value.Title,
		CreatedAt: value.CreatedAt,
		UpdatedAt: value.UpdatedAt,
	}
}

func (b *SessionBindings) CloseSession(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "CloseSession", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "CloseSession", services.Sessions.CloseSession(ctx, identity.SessionID(sessionID)))
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
	if err := services.Sessions.ReloadSessionConfig(ctx, identity.SessionID(sessionID)); err != nil {
		return publicError(b.host, "SessionBindings.ReloadConfig", err)
	}
	return nil
}

// SelectModel 只更新指定会话的选择；队列唤醒由 Coordinator 编排。
func (b *SessionBindings) SelectModel(wire SelectModelRequest) error {
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
	if err := services.Sessions.SelectModel(ctx, identity.SessionID(wire.SessionID), choice); err != nil {
		return publicError(b.host, "SessionBindings.SelectModel", err)
	}
	return nil
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
	path, err := services.Sessions.SessionLogPath(ctx, identity.SessionID(sessionID))
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

// SetSessionApprovalMode 保存会话授权选择；null 恢复继承，有效模式在会话显式 reload 后更新。
func (b *SessionBindings) SetSessionApprovalMode(wire SetSessionApprovalModeRequest) (SessionApprovalMode, error) {
	if wire.Mode != nil {
		wire.Mode = new(strings.TrimSpace(*wire.Mode))
	}
	if err := validation.Struct(wire); err != nil {
		return SessionApprovalMode{}, publicInputError(b.host, "SessionBindings.SetSessionApprovalMode.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return SessionApprovalMode{}, err
	}
	var mode *security.ApprovalMode
	if wire.Mode != nil {
		mode = new(security.ApprovalMode(*wire.Mode))
	}
	value, err := services.Sessions.SetApprovalMode(ctx, identity.SessionID(wire.SessionID), mode)
	if err != nil {
		return SessionApprovalMode{}, publicError(b.host, "SessionBindings.SetSessionApprovalMode", err)
	}
	return publicApprovalMode(value), nil
}

func publicModelOptions(models []modelapi.Option) []ModelOption {
	result := make([]ModelOption, 0, len(models))
	for _, option := range models {
		levels := option.ReasoningLevels
		if levels == nil {
			levels = []string{}
		}
		result = append(result, ModelOption{
			ProviderID:            option.ProviderID,
			ModelID:               option.ModelID,
			Label:                 option.Label,
			ProviderName:          option.ProviderName,
			ReasoningLevels:       levels,
			DefaultReasoningLevel: option.DefaultReasoningLevel,
		})
	}
	return result
}

func publicApprovalMode(value sessionservice.ApprovalSettings) SessionApprovalMode {
	result := SessionApprovalMode{
		SessionID:     value.SessionID.String(),
		GlobalMode:    string(value.GlobalMode),
		EffectiveMode: string(value.EffectiveMode),
	}
	if value.SessionMode != nil {
		result.SessionMode = new(string(*value.SessionMode))
	}
	return result
}
