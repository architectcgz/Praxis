// Package session 负责 Session 初始化、状态读取、上下文追加和 Primary Agent 准入。
package session

import (
	agentmodel "praxis/internal/agent"
	contextmodel "praxis/internal/context"
	"praxis/internal/contracts"
	projectmodel "praxis/internal/project"
	securitymodel "praxis/internal/security"
	sessionmodel "praxis/internal/session"
	workspacemodel "praxis/internal/workspace"

	"context"
	"errors"
	"fmt"
	"strings"

	"praxis/internal/repository"
	runtimecontract "praxis/internal/runtime"
	"praxis/internal/system"
)

// AgentDefinitionFactory 解析可复用的 Agent 定义。
type AgentDefinitionFactory func(contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error)

// AgentSecurityPolicyFactory 根据 Agent 定义生成初始安全策略。
type AgentSecurityPolicyFactory func(workspacemodel.Workspace, contracts.AgentDefinitionID) (securitymodel.AgentSecurityPolicy, error)

// Config 包含初始化 Session 与 Primary Agent 所需的依赖。
type Config struct {
	Transactions  repository.TxRunner
	Projects      repository.ProjectRepository
	Workspaces    repository.WorkspaceRepository
	Sessions      repository.SessionRepository
	Contexts      repository.SessionContextRepository
	Policies      repository.AgentSecurityPolicyRepository
	Agents        repository.SessionAgentRepository
	Definitions   AgentDefinitionFactory
	PolicyFactory AgentSecurityPolicyFactory
	Transcripts   runtimecontract.TranscriptLoader
	Clock         system.Clock
	IDs           system.IDGenerator
}

// Service owns Session initialization and primary Agent lookup.
type Service struct {
	tx             repository.TxRunner
	projects       repository.ProjectRepository
	workspaces     repository.WorkspaceRepository
	sessions       repository.SessionRepository
	contexts       repository.SessionContextRepository
	policies       repository.AgentSecurityPolicyRepository
	agents         repository.SessionAgentRepository
	definitions    AgentDefinitionFactory
	policyFactory  AgentSecurityPolicyFactory
	transcripts    runtimecontract.TranscriptLoader
	contextBuilder contextmodel.ContextBuilder
	clock          system.Clock
	ids            system.IDGenerator
}

// CreateParams contains the durable identity and initial state for a Session.
type CreateParams struct {
	RequestID    contracts.RequestID
	SessionID    contracts.SessionID
	AgentID      contracts.AgentID
	ProjectID    contracts.ProjectID
	WorkspaceID  contracts.WorkspaceID
	DefinitionID contracts.AgentDefinitionID
	Policy       securitymodel.AgentSecurityPolicy
}

// CreateResult returns the initialized Session and primary Agent.
type CreateResult struct {
	Session sessionmodel.Session
	Agent   agentmodel.Agent
}

// NewService creates the Session application service.
func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions":      config.Transactions,
		"projects":          config.Projects,
		"workspaces":        config.Workspaces,
		"sessions":          config.Sessions,
		"session contexts":  config.Contexts,
		"security policies": config.Policies,
		"agents":            config.Agents,
		"agent definitions": config.Definitions,
		"policy factory":    config.PolicyFactory,
		"transcript cursor": config.Transcripts,
	} {
		if value == nil {
			return nil, fmt.Errorf("session service %s is required", name)
		}
	}
	return &Service{
		tx:             config.Transactions,
		projects:       config.Projects,
		workspaces:     config.Workspaces,
		sessions:       config.Sessions,
		contexts:       config.Contexts,
		policies:       config.Policies,
		agents:         config.Agents,
		definitions:    config.Definitions,
		policyFactory:  config.PolicyFactory,
		transcripts:    config.Transcripts,
		contextBuilder: contextmodel.NewContextBuilder(0, 0),
		clock:          system.ClockOrDefault(config.Clock),
		ids:            system.IDsOrDefault(config.IDs),
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
	if projectID == "" {
		return nil, invalidSessionQuery("project id is required")
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
	if sessionID == "" {
		return SessionView{}, invalidSessionQuery("session id is required")
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

// ListSessionContext 返回指定 revision 之后不可变的 SessionContext 条目。
func (s *Service) ListSessionContext(ctx context.Context, sessionID contracts.SessionID, afterRevision uint64, limit int) ([]contextmodel.SessionContextEntry, error) {
	if ctx == nil || sessionID == "" {
		return nil, invalidSessionQuery("session context query is invalid")
	}
	if _, err := s.sessions.Get(ctx, sessionID); err != nil {
		return nil, err
	}
	return s.contexts.List(ctx, sessionID, afterRevision, limit)
}

// CreateSession creates a Session, its primary Agent, initial policy and
// revision-one context in one transaction.
func (s *Service) CreateSession(ctx context.Context, params CreateParams) (CreateResult, error) {
	if ctx == nil {
		return CreateResult{}, errors.New("create session context is required")
	}
	if params.SessionID == "" || params.AgentID == "" || params.ProjectID == "" || params.WorkspaceID == "" {
		return CreateResult{}, contracts.New(contracts.InvalidRequest, "")
	}
	if params.DefinitionID == "" {
		params.DefinitionID = agentmodel.DefinitionPrimary
	}
	definition, err := s.definitions(params.DefinitionID)
	if err != nil {
		return CreateResult{}, contracts.New(contracts.InvalidRequest, "")
	}
	var result CreateResult
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
		policy := params.Policy
		if policy.Revision == 0 {
			policy, err = s.policyFactory(workspace, definition.ID)
			if err != nil {
				return err
			}
		}
		agent, err := agentmodel.NewAgent(params.AgentID, session.ID, definition.ID, definition.Profile, policy.Revision, at)
		if err != nil {
			return err
		}
		if err := s.sessions.Save(txCtx, session); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		if err := s.policies.Save(txCtx, agent.ID, policy); err != nil {
			return err
		}
		result = CreateResult{Session: session, Agent: agent}
		return nil
	})
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

// GetOrCreatePrimaryAgent returns an existing Session Agent or atomically
// creates the missing primary Agent for an already-initialized Session.
func (s *Service) GetOrCreatePrimaryAgent(ctx context.Context, sessionID contracts.SessionID, requestID contracts.RequestID) (agentmodel.Agent, error) {
	if ctx == nil {
		return agentmodel.Agent{}, errors.New("ensure primary agent context is required")
	}
	if requestID == "" || sessionID == "" {
		return agentmodel.Agent{}, contracts.New(contracts.InvalidRequest, "")
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
	workspace, err := s.workspaces.Get(ctx, session.WorkspaceID)
	if err != nil {
		return agentmodel.Agent{}, err
	}
	definition, err := s.definitions(agentmodel.DefinitionPrimary)
	if err != nil {
		return agentmodel.Agent{}, err
	}
	policy, err := s.policyFactory(workspace, definition.ID)
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
		agent, err := agentmodel.NewAgent(contracts.AgentID(s.ids.New("agent")), session.ID, definition.ID, definition.Profile, policy.Revision, at)
		if err != nil {
			return err
		}
		if err := s.policies.Save(txCtx, agent.ID, policy); err != nil {
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
	EntryID           contracts.ContextEntryID
	SessionID         contracts.SessionID
	ExpectedRevision  uint64
	Kind              contextmodel.SessionContextKind
	SourceExecutionID contracts.AgentExecutionID
	Content           string
}

// AppendContextResult returns the durable entry and retry state.
type AppendContextResult struct {
	Entry         contextmodel.SessionContextEntry
	ExistingEntry bool
}

// AppendSessionContext appends immutable shared context after checking the
// expected revision in the same transaction as its event and command receipt.
func (s *Service) AppendSessionContext(ctx context.Context, params AppendContextParams) (AppendContextResult, error) {
	if ctx == nil {
		return AppendContextResult{}, errors.New("append session context is required")
	}
	content := strings.TrimSpace(params.Content)
	if params.EntryID == "" || params.SessionID == "" || content == "" {
		return AppendContextResult{}, contracts.New(contracts.InvalidRequest, "")
	}
	var result AppendContextResult
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, found, err := s.contexts.GetByID(txCtx, params.EntryID)
		if err != nil {
			return err
		}
		if found {
			if existing.SessionID != params.SessionID || existing.Kind != params.Kind ||
				existing.SourceExecutionID != params.SourceExecutionID || existing.Content != content {
				return contracts.ErrRequestConflict
			}
			result = AppendContextResult{Entry: existing, ExistingEntry: true}
			return nil
		}
		current, err := s.contexts.CurrentRevision(txCtx, params.SessionID)
		if err != nil {
			return err
		}
		if current != params.ExpectedRevision {
			return contracts.ErrRevisionConflict
		}
		entry, err := contextmodel.NewSessionContextEntry(
			params.EntryID,
			params.SessionID,
			current+1,
			params.Kind,
			params.SourceExecutionID,
			content,
			s.clock.Now(),
		)
		if err != nil {
			return err
		}
		if err := s.contexts.Append(txCtx, entry, current); err != nil {
			return err
		}
		result.Entry = entry
		return nil
	})
	return result, err
}

// invalidSessionQuery 将非法读取请求转换为对外稳定的业务错误。
func invalidSessionQuery(message string) error {
	return contracts.New(contracts.InvalidRequest, message)
}
