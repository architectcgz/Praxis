// Package session 负责 Session 初始化、状态读取、上下文追加和 Primary Agent 准入。
package session

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	workspacemodel "praxis/internal/core/workspace"

	"context"
	"errors"
	"fmt"
	"sync"

	"praxis/internal/hooks"
	"praxis/internal/repository"
	"praxis/internal/request"
	"praxis/internal/system"
)

// AgentDefinitionFactory 解析可复用的 Agent 定义。
type AgentDefinitionFactory func(contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error)

// WorkspaceTextReader 是文件预览所需的最小读取 Port。
type WorkspaceTextReader interface {
	ReadText(context.Context, string, string, int64) (string, error)
}

// Config 包含初始化 Session 与 Primary Agent 所需的依赖。
type Config struct {
	Transactions      repository.TxRunner
	HookDispatcher    *hooks.Dispatcher
	Projects          repository.ProjectRepository
	Workspaces        repository.WorkspaceRepository
	Sessions          repository.SessionRepository
	Contexts          repository.SessionContextRepository
	Agents            repository.SessionAgentRepository
	Tasks             repository.TaskRepository
	Definitions       AgentDefinitionFactory
	Messages          repository.MessageStreams
	UsageRecords      func(context.Context, string) ([]model.ModelUsageRecord, error)
	RemoveSessionData func(context.Context, contracts.SessionID, []string) error
	TextReader        WorkspaceTextReader
	Clock             system.Clock
	IDs               system.IDGenerator
}

// Service owns Session initialization and primary Agent lookup.
type Service struct {
	tx                repository.TxRunner
	hookDispatcher    *hooks.Dispatcher
	projects          repository.ProjectRepository
	workspaces        repository.WorkspaceRepository
	sessions          repository.SessionRepository
	contexts          repository.SessionContextRepository
	agents            repository.SessionAgentRepository
	tasks             repository.TaskRepository
	definitions       AgentDefinitionFactory
	messages          repository.MessageStreams
	usageRecords      func(context.Context, string) ([]model.ModelUsageRecord, error)
	removeSessionData func(context.Context, contracts.SessionID, []string) error
	textReader        WorkspaceTextReader
	clock             system.Clock
	ids               system.IDGenerator

	createdSessionsMu sync.Mutex
	createdSessions   map[contracts.SessionID]struct{}
	closing           bool
}

// CreateParams contains the durable identity and initial state for a Session.
type CreateParams struct {
	RequestID    contracts.RequestID
	SessionID    contracts.SessionID
	AgentID      contracts.AgentID
	ProjectID    contracts.ProjectID
	WorkspaceID  contracts.WorkspaceID
	DefinitionID contracts.AgentDefinitionID
}

type CreateResult struct {
	Session sessionmodel.Session
	Agent   agentmodel.Agent
}

// NewService creates the Session application service.
func NewService(config Config) (*Service, error) {
	if config.UsageRecords == nil {
		return nil, errors.New("session service usage reader is required")
	}
	for name, value := range map[string]any{
		"transactions":          config.Transactions,
		"projects":              config.Projects,
		"workspaces":            config.Workspaces,
		"sessions":              config.Sessions,
		"session contexts":      config.Contexts,
		"agents":                config.Agents,
		"tasks":                 config.Tasks,
		"agent definitions":     config.Definitions,
		"message loader":        config.Messages,
		"workspace text reader": config.TextReader,
	} {
		if value == nil {
			return nil, fmt.Errorf("session service %s is required", name)
		}
	}
	return &Service{
		tx:                config.Transactions,
		hookDispatcher:    config.HookDispatcher,
		projects:          config.Projects,
		workspaces:        config.Workspaces,
		sessions:          config.Sessions,
		contexts:          config.Contexts,
		agents:            config.Agents,
		tasks:             config.Tasks,
		definitions:       config.Definitions,
		messages:          config.Messages,
		usageRecords:      config.UsageRecords,
		removeSessionData: config.RemoveSessionData,
		textReader:        config.TextReader,
		clock:             system.ClockOrDefault(config.Clock),
		ids:               system.IDsOrDefault(config.IDs),
		createdSessions:   make(map[contracts.SessionID]struct{}),
	}, nil
}

// SessionView 是渲染 Session 工作区所需的持久化详情视图。
type SessionView struct {
	Session sessionmodel.Session
	Agents  []agentmodel.Agent
}

// ListSessionsByProject 返回指定 Project 下按索引查询的 Session 列表。
func (s *Service) ListSessionsByProject(ctx context.Context, projectID contracts.ProjectID, limit int) ([]sessionmodel.Session, error) {
	if ctx == nil {
		return nil, errors.New("project session catalog context is required")
	}
	lister, ok := s.sessions.(repository.ProjectSessionListRepository)
	if !ok {
		return nil, errors.New("project session catalog is unavailable")
	}
	return lister.ListByProject(ctx, projectID, limit)
}

// ListSessions 返回桌面发现使用的持久化 Session 列表。
func (s *Service) ListSessions(ctx context.Context, limit int) ([]sessionmodel.Session, error) {
	if ctx == nil {
		return nil, errors.New("session catalog context is required")
	}
	lister, ok := s.sessions.(repository.SessionListRepository)
	if !ok {
		return nil, errors.New("session catalog is unavailable")
	}
	return lister.List(ctx, limit)
}

// GetSessionView 返回 Session 及其按索引查询的 Agent 列表。
func (s *Service) GetSessionView(ctx context.Context, sessionID contracts.SessionID, limit int) (SessionView, error) {
	if ctx == nil {
		return SessionView{}, errors.New("session query context is required")
	}
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return SessionView{}, err
	}
	agents, err := s.agents.ListBySession(ctx, sessionID, limit)
	if err != nil {
		return SessionView{}, fmt.Errorf("list agents in session %s: %w", sessionID, err)
	}
	return SessionView{Session: session, Agents: agents}, nil
}

// ListSessionContext 按稳定时间和 ID 游标读取 SessionContext 条目。
func (s *Service) ListSessionContext(ctx context.Context, sessionID contracts.SessionID, afterID string, limit int) ([]sessionmodel.SessionContextEntry, error) {
	if ctx == nil {
		return nil, contracts.New(contracts.InvalidRequest, "session context query is invalid")
	}
	if _, err := s.sessions.Get(ctx, sessionID); err != nil {
		return nil, err
	}
	return s.contexts.List(ctx, sessionID, afterID, limit)
}

// CreateSession 在一个事务中创建 Session 和主 Agent。
// OperationHook 覆盖本次创建尝试，Start/End 均在创建锁之外，End 观察提交结果或错误。
func (s *Service) CreateSession(ctx context.Context, params CreateParams) (result CreateResult, err error) {
	if ctx == nil {
		return CreateResult{}, errors.New("create session context is required")
	}
	ctx, scope := s.hookDispatcher.Start(ctx, hooks.Operation{
		Identity: hooks.Identity{
			SessionID: params.SessionID,
			AgentID:   params.AgentID,
		},
		Kind: hooks.OperationSessionCreate,
		Name: "create_session",
	})
	defer func() { scope.End(hooks.Result{Err: err}) }()
	// 创建与退出清理串行，保证已提交的新会话不会漏过清理范围。
	s.createdSessionsMu.Lock()
	// 先解锁再执行前面注册的 Scope.End，允许 Hook 回读仓储或重入 Service。
	defer s.createdSessionsMu.Unlock()
	created := false
	if s.closing {
		return CreateResult{}, errors.New("session service is closing")
	}
	if params.DefinitionID == "" {
		params.DefinitionID = agentmodel.DefinitionPrimary
	}
	definition, err := s.definitions(params.DefinitionID)
	if err != nil {
		return CreateResult{}, contracts.New(contracts.InvalidRequest, "")
	}
	err = s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.sessions.Get(txCtx, params.SessionID)
		if err == nil {
			agent, agentErr := s.agents.Get(txCtx, params.AgentID)
			if agentErr != nil {
				return contracts.ErrRequestConflict
			}
			if existing.ProjectID != params.ProjectID || existing.WorkspaceID != params.WorkspaceID ||
				agent.SessionID != params.SessionID || agent.DefinitionID != definition.ID || agent.Profile != definition.Profile {
				return contracts.ErrRequestConflict
			}
			result = CreateResult{Session: existing, Agent: agent}
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		if _, err := s.agents.Get(txCtx, params.AgentID); err == nil {
			return contracts.ErrRequestConflict
		} else if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		project, err := s.projects.Get(txCtx, params.ProjectID)
		if err != nil || project.State != projectmodel.ProjectActive {
			if err != nil {
				return err
			}
			return contracts.New(contracts.ProjectWorkspaceInvalid, "")
		}
		workspace, err := s.workspaces.Get(txCtx, params.WorkspaceID)
		if err != nil {
			return err
		}
		if workspace.ProjectID != project.ID || workspace.State != workspacemodel.WorkspaceReady {
			return contracts.New(contracts.ProjectWorkspaceInvalid, "")
		}
		at := s.clock.Now().UTC()
		session, err := sessionmodel.NewSession(params.SessionID, project.ID, workspace.ID, at)
		if err != nil {
			return err
		}
		agent, err := agentmodel.NewAgent(params.AgentID, session.ID, definition.ID, definition.Profile, at)
		if err != nil {
			return err
		}
		if err := s.sessions.Save(txCtx, session); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result = CreateResult{Session: session, Agent: agent}
		created = true
		return nil
	})
	if err == nil && created {
		s.createdSessions[params.SessionID] = struct{}{}
	}
	return result, err
}

// CreateSessionForProject initializes a primary Session for one Project.
func (s *Service) CreateSessionForProject(ctx context.Context, sessionID contracts.SessionID, agentID contracts.AgentID, requestID contracts.RequestID, projectID contracts.ProjectID, workspaceID contracts.WorkspaceID, definitionID contracts.AgentDefinitionID) (CreateResult, error) {
	return s.CreateSession(ctx, CreateParams{
		RequestID: requestID, SessionID: sessionID, AgentID: agentID,
		ProjectID: projectID, WorkspaceID: workspaceID,
		DefinitionID: definitionID,
	})
}

// creates the missing primary Agent for an already-initialized Session.
func (s *Service) GetOrCreatePrimaryAgent(ctx context.Context, sessionID contracts.SessionID, requestID contracts.RequestID) (agentmodel.Agent, error) {
	if ctx == nil {
		return agentmodel.Agent{}, errors.New("ensure primary agent context is required")
	}
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return agentmodel.Agent{}, err
	}
	agent, err := s.agents.GetBySessionAndDefinition(ctx, sessionID, agentmodel.DefinitionPrimary)
	if err == nil {
		return agent, nil
	}
	if !errors.Is(err, contracts.ErrNotFound) {
		return agentmodel.Agent{}, err
	}
	definition, err := s.definitions(agentmodel.DefinitionPrimary)
	if err != nil {
		return agentmodel.Agent{}, err
	}
	var created agentmodel.Agent
	err = s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.agents.GetBySessionAndDefinition(txCtx, sessionID, definition.ID)
		if err == nil {
			created = existing
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		at := s.clock.Now().UTC()
		agent, err := agentmodel.NewAgent(contracts.AgentID(s.ids.New("agent")), session.ID, definition.ID, definition.Profile, at)
		if err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		created = agent
		return nil
	})
	if err != nil {
		return agentmodel.Agent{}, err
	}
	return created, nil
}

// AppendContextParams describes one compare-and-append SessionContext write.
type AppendContextParams struct {
	EntryID      contracts.ContextEntryID
	SessionID    contracts.SessionID
	Kind         sessionmodel.SessionContextKind
	SourceTaskID contracts.TaskID
	Content      string
}

type AppendContextResult struct {
	Entry         sessionmodel.SessionContextEntry
	ExistingEntry bool
}

// AppendSessionContext 按稳定 ID 幂等追加不可变共享上下文。
func (s *Service) AppendSessionContext(ctx context.Context, params AppendContextParams) (AppendContextResult, error) {
	if ctx == nil {
		return AppendContextResult{}, errors.New("append session context is required")
	}
	var result AppendContextResult
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		entry, err := sessionmodel.NewSessionContextEntry(
			params.EntryID,
			params.SessionID,
			params.Kind,
			params.SourceTaskID,
			params.Content,
			s.clock.Now(),
		)
		if err != nil {
			return err
		}
		existing, found, err := s.contexts.GetByID(txCtx, params.EntryID)
		if err != nil {
			return err
		}
		if found {
			if existing.SessionID != params.SessionID || existing.Kind != params.Kind ||
				existing.SourceTaskID != params.SourceTaskID || existing.Content != entry.Content {
				return contracts.ErrRequestConflict
			}
			result = AppendContextResult{Entry: existing, ExistingEntry: true}
			return nil
		}
		if err := s.contexts.Append(txCtx, entry); err != nil {
			return err
		}
		result.Entry = entry
		return nil
	})
	return result, err
}

// DeleteSession 删除没有活动执行的 Session 和关联文档。
func (s *Service) DeleteSession(ctx context.Context, sessionID contracts.SessionID) error {
	if ctx == nil {
		return errors.New("delete session context is required")
	}
	return s.deleteSession(ctx, sessionID, false)
}

// CleanupEmptySessions 清理本次运行新建且没有消息的会话，并拒绝后续创建。
// 已删除的会话会被跳过；单个清理失败不影响其他会话，错误统一返回。
func (s *Service) CleanupEmptySessions(ctx context.Context) error {
	if ctx == nil {
		return errors.New("empty session cleanup context is required")
	}
	s.createdSessionsMu.Lock()
	defer s.createdSessionsMu.Unlock()
	s.closing = true
	var cleanupErr error
	for sessionID := range s.createdSessions {
		if err := s.deleteSession(ctx, sessionID, true); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup session %s: %w", sessionID, err))
			continue
		}
		delete(s.createdSessions, sessionID)
	}
	return cleanupErr
}

func (s *Service) deleteSession(ctx context.Context, sessionID contracts.SessionID, onlyEmpty bool) error {
	var documentRefs []string
	deleted := false
	if err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		if onlyEmpty {
			if _, err := s.sessions.Get(txCtx, sessionID); errors.Is(err, contracts.ErrNotFound) {
				return nil
			} else if err != nil {
				return err
			}
			// 消息检查和删除共用事务，避免并发输入提交后被误删。
			if hasMessages, err := s.hasSessionMessages(txCtx, sessionID); err != nil || hasMessages {
				return err
			}
		}
		var err error
		documentRefs, err = s.sessions.Delete(txCtx, sessionID)
		deleted = err == nil
		return err
	}); err != nil {
		return err
	}
	if deleted && s.removeSessionData != nil {
		if err := s.removeSessionData(context.WithoutCancel(ctx), sessionID, documentRefs); err != nil {
			return fmt.Errorf("remove session data: %w", err)
		}
	}
	return nil
}

func (s *Service) hasSessionMessages(ctx context.Context, sessionID contracts.SessionID) (bool, error) {
	return s.sessions.HasMessages(ctx, sessionID)
}

// RenameSession 更新会话标题，并刷新会话目录排序所使用的更新时间。
func (s *Service) RenameSession(ctx context.Context, canonical request.RenameSession) error {
	if ctx == nil {
		return errors.New("rename session context is required")
	}
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		return s.sessions.Rename(txCtx, canonical.SessionID, canonical.Title, s.clock.Now())
	})
}
