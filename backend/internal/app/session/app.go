// Package session 提供会话应用入口和公开结果投影。
package session

import (
	"context"
	"sync"

	agentcontracts "praxis/internal/app/agent/contracts"
	sessioncontracts "praxis/internal/app/session/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/identity"
	"praxis/internal/request"
	businesssession "praxis/internal/service/session"
)

// App 适配会话业务；配置锁与模型修改、输入提交共用，避免读取混合配置。
type App struct {
	sessions *businesssession.Service
	configMu *sync.RWMutex
	logPath  func(context.Context, identity.SessionID) (string, error)
}

// New 注入会话业务、唯一配置锁和只读日志定位能力，不打开存储资源。
func New(sessions *businesssession.Service, configMu *sync.RWMutex, logPath func(context.Context, identity.SessionID) (string, error)) *App {
	return &App{
		sessions: sessions,
		configMu: configMu,
		logPath:  logPath,
	}
}

// ListSessionsByProject 返回项目会话目录；分页默认值由输入边界决定。
func (a *App) ListSessionsByProject(ctx context.Context, projectID string, limit int) ([]sessioncontracts.SessionSummary, error) {
	limit, err := request.ListLimit(limit)
	if err != nil {
		return nil, err
	}
	sessions, err := a.sessions.ListSessionsByProject(ctx, identity.ProjectID(projectID), limit)
	if err != nil {
		return nil, err
	}
	result := make([]sessioncontracts.SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, sessioncontracts.SessionSummary{
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

// GetSessionDetail 返回会话和 Agent 摘要，不暴露内部实体和执行配置。
func (a *App) GetSessionDetail(ctx context.Context, sessionID string, limit int) (sessioncontracts.SessionDetail, error) {
	limit, err := request.ListLimit(limit)
	if err != nil {
		return sessioncontracts.SessionDetail{}, err
	}
	view, err := a.sessions.GetSessionView(ctx, identity.SessionID(sessionID), limit)
	if err != nil {
		return sessioncontracts.SessionDetail{}, err
	}
	result := sessioncontracts.SessionDetail{
		Session: sessioncontracts.SessionSummary{
			ID:          view.Session.ID.String(),
			ProjectID:   view.Session.ProjectID.String(),
			WorkspaceID: view.Session.WorkspaceID.String(),
			Title:       view.Session.Title,
			CreatedAt:   view.Session.CreatedAt,
			UpdatedAt:   view.Session.UpdatedAt,
		},
		Agents: make([]agentcontracts.AgentDetail, 0, len(view.Agents)),
	}
	for _, agent := range view.Agents {
		result.Agents = append(result.Agents, agentcontracts.AgentDetail{
			ID:            agent.ID.String(),
			Name:          agentmodel.DisplayName(agent.DefinitionID),
			SessionID:     agent.SessionID.String(),
			DefinitionID:  agent.DefinitionID.String(),
			Profile:       string(agent.Profile),
			State:         string(agent.State),
			CurrentTaskID: agent.CurrentTaskID.String(),
			Tasks:         []agentcontracts.TaskInfo{},
		})
	}
	return result, nil
}

// CreateSession 接收 canonical 请求，创建会话及主 Agent 后返回公开身份。
func (a *App) CreateSession(ctx context.Context, canonical request.CreateSession) (sessioncontracts.CreateSessionResult, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	created, err := a.sessions.CreateSessionForProject(
		ctx,
		identity.SessionID(canonical.SessionID),
		identity.AgentID(canonical.AgentID),
		identity.RequestID(canonical.RequestID),
		identity.ProjectID(canonical.ProjectID),
		identity.WorkspaceID(canonical.WorkspaceID),
		identity.AgentDefinitionID(canonical.DefinitionID),
	)
	if err != nil {
		return sessioncontracts.CreateSessionResult{}, err
	}
	return sessioncontracts.CreateSessionResult{
		SessionID:    created.Session.ID.String(),
		ProjectID:    created.Session.ProjectID.String(),
		WorkspaceID:  created.Session.WorkspaceID.String(),
		AgentID:      created.Agent.ID.String(),
		DefinitionID: created.Agent.DefinitionID.String(),
	}, nil
}

// DeleteSession 删除指定会话，活动执行和清理约束由业务层处理。
func (a *App) DeleteSession(ctx context.Context, sessionID string) error {
	return a.sessions.DeleteSession(ctx, identity.SessionID(sessionID))
}

// RenameSession 传递已规范化标题，不在内部重复清洗或校验。
func (a *App) RenameSession(ctx context.Context, canonical request.RenameSession) error {
	return a.sessions.RenameSession(ctx, identity.SessionID(canonical.SessionID), canonical.Title)
}

// PreviewFile 返回当前会话工作区内的文本快照；授权仍由业务层完成。
func (a *App) PreviewFile(ctx context.Context, canonical request.FilePreview) (sessioncontracts.FilePreview, error) {
	value, err := a.sessions.PreviewFile(ctx, identity.SessionID(canonical.SessionID), canonical.Path)
	if err != nil {
		return sessioncontracts.FilePreview{}, err
	}
	return sessioncontracts.FilePreview{Path: value.Path, Content: value.Content}, nil
}

// SessionLogPath 只按会话身份定位现存日志，不接受调用方指定的本地路径。
func (a *App) SessionLogPath(ctx context.Context, sessionID string) (string, error) {
	return a.logPath(ctx, identity.SessionID(sessionID))
}
