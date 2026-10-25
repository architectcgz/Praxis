// Package session 负责 Session 初始化、会话模型选择、状态读取和上下文追加。
package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/hook"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	projectservice "praxis/internal/service/project"
	"slices"
	"sync"
)

// AgentDefinitionFactory 解析可复用的 Agent 定义。
type AgentDefinitionFactory func(identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)

// Dependencies 包含 Session 用例、会话模型选择与主 Agent 获取所需的依赖。
type Dependencies struct {
	ConfigMu       *sync.RWMutex
	ModelConfigs   ModelConfigResolver
	HookDispatcher *hook.Dispatcher
	Projects       repository.Project
	Workspaces     repository.Workspace
	Sessions       repository.Session
	Entries        repository.Entries
	MainAgent      func(context.Context, identity.SessionID) (*agentmodel.Agent, error)
	Definitions    AgentDefinitionFactory
	UsageRecords   repository.ModelUsage
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 管理 Session 用例，并持有会话所属的 Agent 实例、模型选择和输入队列。
type Service struct {
	configMu       *sync.RWMutex
	modelConfigs   ModelConfigResolver
	hookDispatcher *hook.Dispatcher
	projects       repository.Project
	workspaces     repository.Workspace
	sessions       repository.Session
	entries        repository.Entries
	mainAgent      func(context.Context, identity.SessionID) (*agentmodel.Agent, error)
	definitions    AgentDefinitionFactory
	usageRecords   repository.ModelUsage
	clock          clock.Clock
	ids            identity.IDGenerator

	// 两个索引引用同一批实例，不复制 Agent 状态；删除会话时一起释放。
	agentsMu        sync.RWMutex
	agents          map[identity.AgentID]*agentmodel.Agent
	agentsBySession map[identity.SessionID]map[identity.AgentID]*agentmodel.Agent

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
	Agent   *agentmodel.Agent
}

// NewService 创建 Session 业务服务。
func NewService(deps Dependencies) *Service {
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
		mainAgent:       deps.MainAgent,
		agents:          make(map[identity.AgentID]*agentmodel.Agent),
		agentsBySession: make(map[identity.SessionID]map[identity.AgentID]*agentmodel.Agent),
		definitions:     deps.Definitions,
		usageRecords:    deps.UsageRecords,
		clock:           deps.Clock,
		ids:             deps.IDs,
		createdSessions: make(map[identity.SessionID]struct{}),
	}
}

// SessionView 聚合持久化会话、内存 Agent 实例和本进程的输入待办。
type SessionView struct {
	Session sessionmodel.Session
	Agents  []*agentmodel.Agent
	Tasks   []sessionmodel.Task
}

// ListSessionsByProject 返回指定 Project 下按索引查询的 Session 列表。
func (s *Service) ListSessionsByProject(ctx context.Context, projectID identity.ProjectID, limit int) ([]sessionmodel.Session, error) {
	return s.sessions.ListByProject(ctx, projectID, limit)
}

// ListSessions 返回持久化 Session 目录。
func (s *Service) ListSessions(ctx context.Context, limit int) ([]sessionmodel.Session, error) {
	return s.sessions.List(ctx, limit)
}

// GetSessionView 返回 Session 及其持有的 Agent 实例列表。
func (s *Service) GetSessionView(ctx context.Context, sessionID identity.SessionID, limit int) (SessionView, error) {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return SessionView{}, err
	}
	agents := s.ListAgents(sessionID, limit)
	if len(agents) == 0 {
		return SessionView{}, errorcode.ErrNotFound
	}
	return SessionView{
		Session: session,
		Agents:  agents,
		Tasks:   s.ListTasks(sessionID, 0),
	}, nil
}

// CreateSession 保存会话及主 Agent 的身份引用，再创建内存实例；重复请求复用同一实例。
// OperationHook 覆盖本次创建尝试，Start/End 均在创建锁之外，End 观察提交结果或错误。
func (s *Service) CreateSession(ctx context.Context, params CreateInput) (result CreateResult, err error) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	if params.SessionID == "" {
		return CreateResult{}, errorcode.ErrInvalidRequest
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
	if s.closing {
		return CreateResult{}, errors.New("session service is closing")
	}
	definition, err := s.definitions(params.DefinitionID)
	if err != nil {
		return CreateResult{}, errorcode.ErrInvalidRequest
	}
	existing, err := s.sessions.Get(ctx, params.SessionID)
	if err == nil {
		if existing.ProjectID != params.ProjectID || existing.WorkspaceID != params.WorkspaceID ||
			existing.MainAgentID != params.AgentID || existing.AgentDefinitionID != definition.ID {
			return CreateResult{}, errorcode.ErrRequestConflict
		}
		agent, err := s.mainAgent(ctx, existing.ID)
		return CreateResult{Session: existing, Agent: agent}, err
	}
	if !errors.Is(err, errorcode.ErrNotFound) {
		return CreateResult{}, err
	}
	if _, err := s.GetAgent(params.AgentID); err == nil {
		return CreateResult{}, errorcode.ErrRequestConflict
	}
	project, err := s.projects.Get(ctx, params.ProjectID)
	if err != nil {
		return CreateResult{}, err
	}
	if project.State != projectmodel.ProjectActive {
		return CreateResult{}, projectservice.ErrWorkspaceInvalid
	}
	workspace, err := s.workspaces.Get(ctx, params.WorkspaceID)
	if err != nil {
		return CreateResult{}, err
	}
	if workspace.ProjectID != project.ID || workspace.State != workspacemodel.WorkspaceReady {
		return CreateResult{}, projectservice.ErrWorkspaceInvalid
	}
	at := s.clock.Now().UTC()
	session := sessionmodel.NewSession(params.SessionID, project.ID, workspace.ID, params.AgentID, definition.ID, at)
	if err := s.sessions.Create(ctx, session); err != nil {
		return CreateResult{}, err
	}
	s.createdSessions[params.SessionID] = struct{}{}
	agent, err := s.mainAgent(ctx, session.ID)
	return CreateResult{Session: session, Agent: agent}, err
}

// CreateSessionForProject 为 Project 初始化主 Session。
func (s *Service) CreateSessionForProject(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, requestID identity.RequestID, projectID identity.ProjectID, workspaceID identity.WorkspaceID, definitionID identity.AgentDefinitionID) (CreateResult, error) {
	return s.CreateSession(ctx, CreateInput{
		RequestID: requestID, SessionID: sessionID, AgentID: agentID,
		ProjectID: projectID, WorkspaceID: workspaceID,
		DefinitionID: definitionID,
	})
}

// RegisterAgent 接管实例引用；相同身份只保留一个实例，删除后的会话不能重新登记。
func (s *Service) RegisterAgent(ctx context.Context, agent *agentmodel.Agent) (*agentmodel.Agent, error) {
	s.agentsMu.Lock()
	defer s.agentsMu.Unlock()
	session, err := s.sessions.Get(ctx, agent.SessionID)
	if err != nil {
		return nil, err
	}
	if agent.IsMain() != (agent.ID == session.MainAgentID) || agent.IsMain() && agent.DefinitionID != session.AgentDefinitionID {
		return nil, errorcode.ErrRequestConflict
	}
	if existing := s.agents[agent.ID]; existing != nil {
		if existing.SessionID != agent.SessionID || existing.DefinitionID != agent.DefinitionID || existing.Kind != agent.Kind {
			return nil, errorcode.ErrRequestConflict
		}
		return existing, nil
	}
	s.agents[agent.ID] = agent
	if s.agentsBySession[agent.SessionID] == nil {
		s.agentsBySession[agent.SessionID] = make(map[identity.AgentID]*agentmodel.Agent)
	}
	s.agentsBySession[agent.SessionID][agent.ID] = agent
	return agent, nil
}

// GetAgent 返回会话持有的同一实例，不查询执行历史或重新计算状态。
func (s *Service) GetAgent(id identity.AgentID) (*agentmodel.Agent, error) {
	s.agentsMu.RLock()
	defer s.agentsMu.RUnlock()
	agent := s.agents[id]
	if agent == nil {
		return nil, errorcode.ErrNotFound
	}
	return agent, nil
}

// ListAgents 返回独立的引用列表，实例本身由 Session 持有。
func (s *Service) ListAgents(sessionID identity.SessionID, limit int) []*agentmodel.Agent {
	s.agentsMu.RLock()
	defer s.agentsMu.RUnlock()
	agents := slices.SortedFunc(maps.Values(s.agentsBySession[sessionID]), func(a, b *agentmodel.Agent) int {
		return cmp.Compare(a.ID, b.ID)
	})
	if limit > 0 {
		agents = agents[:min(len(agents), limit)]
	}
	return agents
}

// DeleteSession 删除没有活动执行的 Session。
func (s *Service) DeleteSession(ctx context.Context, sessionID identity.SessionID) error {
	return s.deleteSession(ctx, sessionID, false)
}

// CleanupEmptySessions 清理本次运行新建且没有消息的会话，并拒绝后续创建。
// 已删除的会话会被跳过；单个清理失败不影响其他会话，错误统一返回。
func (s *Service) CleanupEmptySessions(ctx context.Context) error {
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
	s.agentsMu.Lock()
	defer s.agentsMu.Unlock()
	if err := s.sessions.Delete(ctx, sessionID, onlyEmpty); err != nil {
		return err
	}
	if _, err := s.sessions.Get(ctx, sessionID); err == nil {
		// 空会话清理可能跳过已有消息的会话，此时保留其内存实例。
		return nil
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		return err
	}
	for id := range s.agentsBySession[sessionID] {
		delete(s.agents, id)
	}
	delete(s.agentsBySession, sessionID)
	delete(s.modelSelections, sessionID)
	if queue := s.taskQueues[sessionID]; queue != nil {
		queue.Clear()
		delete(s.taskQueues, sessionID)
	}
	return nil
}

// RenameSession 接收已规范化标题，并刷新会话目录排序使用的更新时间。
func (s *Service) RenameSession(ctx context.Context, sessionID identity.SessionID, title string) error {
	return s.sessions.Rename(ctx, sessionID, title, s.clock.Now())
}
