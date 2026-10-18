// Package session 负责 Session 初始化、状态读取、上下文追加和 Primary Agent 准入。
package session

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/clock"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	"praxis/internal/fault"
	"praxis/internal/identity"
	agentmodel "praxis/internal/service/agent"
	projectmodel "praxis/internal/service/project"
	txrunner "praxis/internal/transaction"
	workspacemodel "praxis/internal/workspace"
	"sync"
)

// AgentDefinitionFactory 解析可复用的 Agent 定义。
type AgentDefinitionFactory func(identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)

// ActiveTaskCounter 提供会话删除前的执行存在性判断。
type ActiveTaskCounter interface {
	CountActiveBySession(context.Context, identity.SessionID) (int, error)
}

// Dependencies 包含初始化 Session 与 Primary Agent 所需的依赖。
type Dependencies struct {
	ConfigMu          *sync.RWMutex
	Transactions      txrunner.Runner
	HookDispatcher    *hook.Dispatcher
	Projects          projectmodel.Store
	Workspaces        workspacemodel.Store
	Sessions          Store
	Contexts          ContextStore
	Agents            agentmodel.Store
	Tasks             ActiveTaskCounter
	Definitions       AgentDefinitionFactory
	UsageRecords      func(context.Context, string) ([]modelapi.ModelUsageRecord, error)
	RemoveSessionData func(context.Context, identity.SessionID, []string) error
	TextReader        workspacemodel.TextReader
	Clock             clock.Clock
	IDs               identity.IDGenerator
}

// Service 负责 Session 初始化和主 Agent 查询。
type Service struct {
	configMu          *sync.RWMutex
	tx                txrunner.Runner
	hookDispatcher    *hook.Dispatcher
	projects          projectmodel.Store
	workspaces        workspacemodel.Store
	sessions          Store
	contexts          ContextStore
	agents            agentmodel.Store
	tasks             ActiveTaskCounter
	definitions       AgentDefinitionFactory
	usageRecords      func(context.Context, string) ([]modelapi.ModelUsageRecord, error)
	removeSessionData func(context.Context, identity.SessionID, []string) error
	textReader        workspacemodel.TextReader
	clock             clock.Clock
	ids               identity.IDGenerator

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
	Session Session
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
		"transactions":          deps.Transactions,
		"projects":              deps.Projects,
		"workspaces":            deps.Workspaces,
		"sessions":              deps.Sessions,
		"session contexts":      deps.Contexts,
		"agents":                deps.Agents,
		"tasks":                 deps.Tasks,
		"agent definitions":     deps.Definitions,
		"workspace text reader": deps.TextReader,
	} {
		if value == nil {
			return nil, fmt.Errorf("session service %s is required", name)
		}
	}
	return &Service{
		configMu:          deps.ConfigMu,
		tx:                deps.Transactions,
		hookDispatcher:    deps.HookDispatcher,
		projects:          deps.Projects,
		workspaces:        deps.Workspaces,
		sessions:          deps.Sessions,
		contexts:          deps.Contexts,
		agents:            deps.Agents,
		tasks:             deps.Tasks,
		definitions:       deps.Definitions,
		usageRecords:      deps.UsageRecords,
		removeSessionData: deps.RemoveSessionData,
		textReader:        deps.TextReader,
		clock:             clock.ClockOrDefault(deps.Clock),
		ids:               identity.IDsOrDefault(deps.IDs),
		createdSessions:   make(map[identity.SessionID]struct{}),
	}, nil
}

// SessionView 聚合会话及其 Agent 持久化状态，不包含应用展示数据。
type SessionView struct {
	Session Session
	Agents  []agentmodel.Agent
}

// ListSessionsByProject 返回指定 Project 下按索引查询的 Session 列表。
func (s *Service) ListSessionsByProject(ctx context.Context, projectID identity.ProjectID, limit int) ([]Session, error) {
	if ctx == nil {
		return nil, errors.New("project session catalog context is required")
	}
	lister, ok := s.sessions.(ProjectListReader)
	if !ok {
		return nil, errors.New("project session catalog is unavailable")
	}
	return lister.ListByProject(ctx, projectID, limit)
}

// ListSessions 返回持久化 Session 目录。
func (s *Service) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	if ctx == nil {
		return nil, errors.New("session catalog context is required")
	}
	lister, ok := s.sessions.(ListReader)
	if !ok {
		return nil, errors.New("session catalog is unavailable")
	}
	return lister.List(ctx, limit)
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
	return SessionView{Session: session, Agents: agents}, nil
}

// ListSessionContext 按稳定时间和 ID 游标读取 SessionContext 条目。
func (s *Service) ListSessionContext(ctx context.Context, sessionID identity.SessionID, afterID string, limit int) ([]SessionContextEntry, error) {
	if ctx == nil {
		return nil, fault.New(fault.InvalidRequest, "session context query is invalid")
	}
	if _, err := s.sessions.Get(ctx, sessionID); err != nil {
		return nil, err
	}
	return s.contexts.List(ctx, sessionID, afterID, limit)
}

// CreateSession 在一个事务中创建 Session 和主 Agent。
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
	err = s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.sessions.Get(txCtx, params.SessionID)
		if err == nil {
			agent, agentErr := s.agents.Get(txCtx, params.AgentID)
			if agentErr != nil {
				return fault.ErrRequestConflict
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
			return fault.New(projectmodel.ProjectWorkspaceInvalid, "")
		}
		workspace, err := s.workspaces.Get(txCtx, params.WorkspaceID)
		if err != nil {
			return err
		}
		if workspace.ProjectID != project.ID || workspace.State != workspacemodel.WorkspaceReady {
			return fault.New(projectmodel.ProjectWorkspaceInvalid, "")
		}
		at := s.clock.Now().UTC()
		session := NewSession(params.SessionID, project.ID, workspace.ID, at)
		agent := agentmodel.NewAgent(params.AgentID, session.ID, definition.ID, definition.Profile, at)
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

// CreateSessionForProject 为 Project 初始化主 Session。
func (s *Service) CreateSessionForProject(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, requestID identity.RequestID, projectID identity.ProjectID, workspaceID identity.WorkspaceID, definitionID identity.AgentDefinitionID) (CreateResult, error) {
	return s.CreateSession(ctx, CreateInput{
		RequestID: requestID, SessionID: sessionID, AgentID: agentID,
		ProjectID: projectID, WorkspaceID: workspaceID,
		DefinitionID: definitionID,
	})
}

// GetOrCreatePrimaryAgent 为已初始化的 Session 创建缺失的主 Agent。
func (s *Service) GetOrCreatePrimaryAgent(ctx context.Context, sessionID identity.SessionID, requestID identity.RequestID) (agentmodel.Agent, error) {
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
		if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		at := s.clock.Now().UTC()
		agent := agentmodel.NewAgent(identity.AgentID(s.ids.New("agent")), session.ID, definition.ID, definition.Profile, at)
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

type AppendContextResult struct {
	Entry         SessionContextEntry
	ExistingEntry bool
}

// AppendContextParams 是已由输入边界规范化的共享事实数据。
type AppendContextParams struct {
	EntryID           identity.ContextEntryID
	SessionID         identity.SessionID
	Kind              SessionContextKind
	SourceExecutionID identity.AgentExecutionID
	Content           string
}

// AppendSessionContext 按稳定 ID 幂等追加不可变共享上下文。
func (s *Service) AppendSessionContext(ctx context.Context, params AppendContextParams) (AppendContextResult, error) {
	if ctx == nil {
		return AppendContextResult{}, errors.New("append session context is required")
	}
	var result AppendContextResult
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		entry := NewSessionContextEntry(
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
		result.Entry = entry
		return nil
	})
	return result, err
}

// DeleteSession 删除没有活动执行的 Session 和关联文档。
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
	return cleanupErr
}

func (s *Service) deleteSession(ctx context.Context, sessionID identity.SessionID, onlyEmpty bool) error {
	var documentRefs []string
	deleted := false
	if err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		if onlyEmpty {
			if _, err := s.sessions.Get(txCtx, sessionID); errors.Is(err, fault.ErrNotFound) {
				return nil
			} else if err != nil {
				return err
			}
			// 消息检查和删除共用事务，避免并发输入提交后被误删。
			if hasMessages, err := s.hasSessionMessages(txCtx, sessionID); err != nil || hasMessages {
				return err
			}
		}
		// 活跃执行是删除用例的业务约束，和删除标记在同一事务内检查。
		active, err := s.tasks.CountActiveBySession(txCtx, sessionID)
		if err != nil {
			return err
		}
		if active > 0 {
			return agentmodel.ErrAgentExecuting
		}
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

func (s *Service) hasSessionMessages(ctx context.Context, sessionID identity.SessionID) (bool, error) {
	return s.sessions.HasMessages(ctx, sessionID)
}

// RenameSession 接收已规范化标题，并刷新会话目录排序使用的更新时间。
func (s *Service) RenameSession(ctx context.Context, sessionID identity.SessionID, title string) error {
	if ctx == nil {
		return errors.New("rename session context is required")
	}
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		return s.sessions.Rename(txCtx, sessionID, title, s.clock.Now())
	})
}
