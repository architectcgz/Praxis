// Package session 负责 Session 初始化、会话模型选择、状态读取、上下文追加和 Primary Agent 准入。
package session

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/hook"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/repository"
	projectservice "praxis/internal/service/project"
	"sync"
)

// AgentDefinitionFactory 解析可复用的 Agent 定义。
type AgentDefinitionFactory func(identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)

// Dependencies 包含 Session 用例、会话模型选择与 Primary Agent 准入所需的依赖。
type Dependencies struct {
	ConfigMu       *sync.RWMutex
	ModelConfigs   ModelConfigResolver
	HookDispatcher *hook.Dispatcher
	Projects       repository.Project
	Workspaces     repository.Workspace
	Sessions       repository.Session
	Entries        repository.Entries
	Contexts       repository.SessionFacts
	Agents         repository.Agent
	Tasks          repository.AgentExecution
	Definitions    AgentDefinitionFactory
	UsageRecords   repository.ModelUsage
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 管理 Session 用例与当前进程内按会话隔离的模型选择。
type Service struct {
	configMu       *sync.RWMutex
	modelConfigs   ModelConfigResolver
	hookDispatcher *hook.Dispatcher
	projects       repository.Project
	workspaces     repository.Workspace
	sessions       repository.Session
	entries        repository.Entries
	contexts       repository.SessionFacts
	agents         repository.Agent
	tasks          repository.AgentExecution
	definitions    AgentDefinitionFactory
	usageRecords   repository.ModelUsage
	clock          clock.Clock
	ids            identity.IDGenerator

	selectionMu     sync.RWMutex
	modelSelections map[identity.SessionID]sessionmodel.ModelSelection
	taskQueues      map[identity.SessionID]*sessionmodel.TaskQueue

	createdSessionsMu sync.Mutex
	createdSessions   map[identity.SessionID]struct{}
	closing           bool
}

// CreateInput 包含 Session 的持久化身份和已确定的 Agent 定义。
type CreateInput struct {
	RequestID    identity.RequestID
	SessionID    identity.SessionID
	AgentID      identity.AgentID
	ProjectID    identity.ProjectID
	WorkspaceID  identity.WorkspaceID
	DefinitionID identity.AgentDefinitionID
}

type CreateResult struct {
	Session sessionmodel.Session
	Agent   agentmodel.Agent
}

// NewService 创建 Session 业务服务。
func NewService(deps Dependencies) (*Service, error) {
	if deps.ConfigMu == nil {
		deps.ConfigMu = &sync.RWMutex{}
	}
	if deps.UsageRecords == nil {
		return nil, errors.New("session service usage reader is required")
	}
	for name, value := range map[string]any{
		"projects":          deps.Projects,
		"workspaces":        deps.Workspaces,
		"sessions":          deps.Sessions,
		"entries":           deps.Entries,
		"session contexts":  deps.Contexts,
		"agents":            deps.Agents,
		"tasks":             deps.Tasks,
		"agent definitions": deps.Definitions,
		"model configs":     deps.ModelConfigs,
	} {
		if value == nil {
			return nil, fmt.Errorf("session service %s is required", name)
		}
	}
	return &Service{
		configMu:        deps.ConfigMu,
		modelConfigs:    deps.ModelConfigs,
		modelSelections: make(map[identity.SessionID]sessionmodel.ModelSelection),
		taskQueues:      make(map[identity.SessionID]*sessionmodel.TaskQueue),
		hookDispatcher:  deps.HookDispatcher,
		projects:        deps.Projects,
		workspaces:      deps.Workspaces,
		sessions:        deps.Sessions,
		entries:         deps.Entries,
		contexts:        deps.Contexts,
		agents:          deps.Agents,
		tasks:           deps.Tasks,
		definitions:     deps.Definitions,
		usageRecords:    deps.UsageRecords,
		clock:           clock.ClockOrDefault(deps.Clock),
		ids:             identity.IDsOrDefault(deps.IDs),
		createdSessions: make(map[identity.SessionID]struct{}),
	}, nil
}

// SessionView 聚合会话、Agent 持久化状态和本进程的输入待办。
type SessionView struct {
	Session sessionmodel.Session
	Agents  []agentmodel.Agent
	Tasks   []sessionmodel.Task
}

// ListSessionsByProject 返回指定 Project 下按索引查询的 Session 列表。
func (s *Service) ListSessionsByProject(ctx context.Context, projectID identity.ProjectID, limit int) ([]sessionmodel.Session, error) {
	if ctx == nil {
		return nil, errors.New("project session catalog context is required")
	}
	return s.sessions.ListByProject(ctx, projectID, limit)
}

// ListSessions 返回持久化 Session 目录。
func (s *Service) ListSessions(ctx context.Context, limit int) ([]sessionmodel.Session, error) {
	if ctx == nil {
		return nil, errors.New("session catalog context is required")
	}
	return s.sessions.List(ctx, limit)
}

// GetSessionView 返回 Session 及其按索引查询的 Agent 列表。
func (s *Service) GetSessionView(ctx context.Context, sessionID identity.SessionID, limit int) (SessionView, error) {
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
	if len(agents) == 0 {
		return SessionView{}, fault.ErrNotFound
	}
	return SessionView{
		Session: session,
		Agents:  agents,
		Tasks:   s.ListTasks(sessionID, 0),
	}, nil
}

// ListSessionContext 按稳定时间和 ID 游标读取 SessionContext 条目。
func (s *Service) ListSessionContext(ctx context.Context, sessionID identity.SessionID, afterID string, limit int) ([]sessionmodel.SharedFact, error) {
	if ctx == nil {
		return nil, fault.New(fault.InvalidRequest, "session context query is invalid")
	}
	if _, err := s.sessions.Get(ctx, sessionID); err != nil {
		return nil, err
	}
	return s.contexts.List(ctx, sessionID, afterID, limit)
}

// CreateSession 分别保存会话和初始 Agent；重复请求继续完成未就绪会话。
// OperationHook 覆盖本次创建尝试，Start/End 均在创建锁之外，End 观察提交结果或错误。
func (s *Service) CreateSession(ctx context.Context, params CreateInput) (result CreateResult, err error) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	if ctx == nil {
		return CreateResult{}, errors.New("create session context is required")
	}
	ctx, scope := s.hookDispatcher.Start(ctx, hook.Operation{
		Identity: hook.Identity{
			SessionID: params.SessionID,
			AgentID:   params.AgentID,
		},
		Kind: hook.OperationSessionCreate,
		Name: "create_session",
	})
	defer func() { scope.End(hook.Result{Err: err}) }()
	// 创建与退出清理串行，保证已提交的新会话不会漏过清理范围。
	s.createdSessionsMu.Lock()
	// 先解锁再执行前面注册的 Scope.End，允许 Hook 回读仓储或重入 Service。
	defer s.createdSessionsMu.Unlock()
	created := false
	if s.closing {
		return CreateResult{}, errors.New("session service is closing")
	}
	definition, err := s.definitions(params.DefinitionID)
	if err != nil {
		return CreateResult{}, fault.New(fault.InvalidRequest, "")
	}
	err = func(txCtx context.Context) error {
		existing, err := s.sessions.Get(txCtx, params.SessionID)
		if err == nil {
			if existing.ProjectID != params.ProjectID || existing.WorkspaceID != params.WorkspaceID {
				return fault.ErrRequestConflict
			}
			agent, agentErr := s.agents.Get(txCtx, params.AgentID)
			if errors.Is(agentErr, fault.ErrNotFound) {
				agent = agentmodel.NewAgent(params.AgentID, existing.ID, definition.ID, definition.Profile, existing.CreatedAt)
				agentErr = s.agents.Save(txCtx, agent)
			}
			if agentErr != nil {
				return agentErr
			}
			if existing.ProjectID != params.ProjectID || existing.WorkspaceID != params.WorkspaceID ||
				agent.SessionID != params.SessionID || agent.DefinitionID != definition.ID || agent.Profile != definition.Profile {
				return fault.ErrRequestConflict
			}
			result = CreateResult{Session: existing, Agent: agent}
			return nil
		}
		if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		if _, err := s.agents.Get(txCtx, params.AgentID); err == nil {
			return fault.ErrRequestConflict
		} else if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		project, err := s.projects.Get(txCtx, params.ProjectID)
		if err != nil || project.State != projectmodel.ProjectActive {
			if err != nil {
				return err
			}
			return fault.New(projectservice.ProjectWorkspaceInvalid, "")
		}
		workspace, err := s.workspaces.Get(txCtx, params.WorkspaceID)
		if err != nil {
			return err
		}
		if workspace.ProjectID != project.ID || workspace.State != workspacemodel.WorkspaceReady {
			return fault.New(projectservice.ProjectWorkspaceInvalid, "")
		}
		at := s.clock.Now().UTC()
		session := sessionmodel.NewSession(params.SessionID, project.ID, workspace.ID, at)
		agent := agentmodel.NewAgent(params.AgentID, session.ID, definition.ID, definition.Profile, at)
		if err := s.sessions.Save(txCtx, session); err != nil {
			return err
		}
		s.createdSessions[params.SessionID] = struct{}{}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result = CreateResult{Session: session, Agent: agent}
		created = true
		return nil
	}(ctx)
	if err == nil && created {
		s.createdSessions[params.SessionID] = struct{}{}
	}
	return result, err
}

// CreateSessionForProject 为 Project 初始化主 Session。
func (s *Service) CreateSessionForProject(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, requestID identity.RequestID, projectID identity.ProjectID, workspaceID identity.WorkspaceID, definitionID identity.AgentDefinitionID) (CreateResult, error) {
	return s.CreateSession(ctx, CreateInput{
		RequestID: requestID, SessionID: sessionID, AgentID: agentID,
		ProjectID: projectID, WorkspaceID: workspaceID,
		DefinitionID: definitionID,
	})
}

// GetOrCreatePrimaryAgent 为已初始化的 Session 创建缺失的主 Agent。
func (s *Service) GetOrCreatePrimaryAgent(ctx context.Context, sessionID identity.SessionID) (agentmodel.Agent, error) {
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
	if !errors.Is(err, fault.ErrNotFound) {
		return agentmodel.Agent{}, err
	}
	initialized, err := s.agents.ListBySession(ctx, sessionID, 1)
	if err != nil {
		return agentmodel.Agent{}, err
	}
	if len(initialized) == 0 {
		return agentmodel.Agent{}, fault.ErrRequestConflict
	}
	definition, err := s.definitions(agentmodel.DefinitionPrimary)
	if err != nil {
		return agentmodel.Agent{}, err
	}
	var created agentmodel.Agent
	err = func(txCtx context.Context) error {
		if err := RequireWritable(txCtx, s.sessions, sessionID); err != nil {
			return err
		}
		existing, err := s.agents.GetBySessionAndDefinition(txCtx, sessionID, definition.ID)
		if err == nil {
			created = existing
			return nil
		}
		if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		at := s.clock.Now().UTC()
		agent := agentmodel.NewAgent(identity.AgentID(s.ids.New("agent")), session.ID, definition.ID, definition.Profile, at)
		if err := s.agents.Save(txCtx, agent); err != nil {
			if errors.Is(err, fault.ErrRequestConflict) {
				created, err = s.agents.GetBySessionAndDefinition(txCtx, sessionID, definition.ID)
			}
			return err
		}
		created = agent
		return nil
	}(ctx)
	if err != nil {
		return agentmodel.Agent{}, err
	}
	return created, nil
}

type AppendContextResult struct {
	Entry         sessionmodel.SharedFact
	ExistingEntry bool
}

// AppendContextParams 是已由输入边界规范化的共享事实数据。
type AppendContextParams struct {
	EntryID           identity.ContextEntryID
	SessionID         identity.SessionID
	Kind              sessionmodel.SharedFactKind
	SourceExecutionID identity.AgentExecutionID
	Content           string
}

// AppendSessionContext 按稳定 ID 幂等追加不可变共享上下文。
func (s *Service) AppendSessionContext(ctx context.Context, params AppendContextParams) (AppendContextResult, error) {
	if ctx == nil {
		return AppendContextResult{}, errors.New("append session context is required")
	}
	var result AppendContextResult
	err := func(txCtx context.Context) error {
		if err := RequireWritable(txCtx, s.sessions, params.SessionID); err != nil {
			return err
		}
		entry := sessionmodel.NewSharedFact(
			params.EntryID,
			params.SessionID,
			params.Kind,
			params.SourceExecutionID,
			params.Content,
			s.clock.Now(),
		)
		existing, found, err := s.contexts.GetByID(txCtx, params.EntryID)
		if err != nil {
			return err
		}
		if found {
			if existing.SessionID != params.SessionID || existing.Kind != params.Kind ||
				existing.SourceExecutionID != params.SourceExecutionID || existing.Content != entry.Content {
				return fault.ErrRequestConflict
			}
			result = AppendContextResult{Entry: existing, ExistingEntry: true}
			return nil
		}
		if err := s.contexts.Append(txCtx, entry); err != nil {
			return err
		}
		result.Entry, _, err = s.contexts.GetByID(txCtx, entry.ID)
		return err
	}(ctx)
	return result, err
}

// DeleteSession 删除没有活动执行的 Session。
func (s *Service) DeleteSession(ctx context.Context, sessionID identity.SessionID) error {
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
	s.selectionMu.Lock()
	for _, queue := range s.taskQueues {
		queue.Clear()
	}
	clear(s.taskQueues)
	s.selectionMu.Unlock()
	return cleanupErr
}

func (s *Service) deleteSession(ctx context.Context, sessionID identity.SessionID, onlyEmpty bool) error {
	s.selectionMu.Lock()
	defer s.selectionMu.Unlock()
	if err := s.sessions.Delete(ctx, sessionID, onlyEmpty); err != nil {
		return err
	}
	delete(s.modelSelections, sessionID)
	if queue := s.taskQueues[sessionID]; queue != nil {
		queue.Clear()
		delete(s.taskQueues, sessionID)
	}
	return nil
}

// RenameSession 接收已规范化标题，并刷新会话目录排序使用的更新时间。
func (s *Service) RenameSession(ctx context.Context, sessionID identity.SessionID, title string) error {
	if ctx == nil {
		return errors.New("rename session context is required")
	}
	return s.sessions.Rename(ctx, sessionID, title, s.clock.Now())
}
