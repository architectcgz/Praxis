// Package session 负责 Session 初始化、状态读取、上下文追加和 Primary Agent 准入。
package session

import (
	agentruntime "praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	contextmodel "praxis/internal/core/context"
	projectmodel "praxis/internal/core/project"
	securitymodel "praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	toolmodel "praxis/internal/core/tool_invocation"
	turnmodel "praxis/internal/core/turn"
	workflowmodel "praxis/internal/core/workflow"
	workspacemodel "praxis/internal/core/workspace"
	toolcontracts "praxis/internal/tools/contracts"

	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"praxis/internal/repository"
	"praxis/internal/system"
)

// AgentDefinitionFactory 解析可复用的 Agent 定义。
type AgentDefinitionFactory func(contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error)

// AgentSecurityPolicyFactory 根据 Agent 定义生成初始安全策略。
type AgentSecurityPolicyFactory func(workspacemodel.Workspace, contracts.AgentDefinitionID) (securitymodel.AgentSecurityPolicy, error)

// Config 包含初始化 Session 与 Primary Agent 所需的依赖。
type Config struct {
	Transactions      repository.TxRunner
	Projects          repository.ProjectRepository
	Workspaces        repository.WorkspaceRepository
	Sessions          repository.SessionRepository
	Contexts          repository.SessionContextRepository
	Policies          repository.AgentSecurityPolicyRepository
	Agents            repository.SessionAgentRepository
	Turns             repository.TurnRepository
	ToolInvocations   repository.ToolInvocationRepository
	QueuedWork        repository.QueuedWorkRepository
	Controls          repository.AgentControlCommandRepository
	SessionMessages   repository.SessionMessageRepository
	AgentMessages     repository.AgentMessageRepository
	Definitions       AgentDefinitionFactory
	PolicyFactory     AgentSecurityPolicyFactory
	Messages          repository.MessageLoader
	UsageRecords      func(context.Context, string) ([]agentruntime.ModelUsageRecord, error)
	RemoveSessionData func(context.Context, contracts.SessionID, []string) error
	Clock             system.Clock
	IDs               system.IDGenerator
}

// Service owns Session initialization and primary Agent lookup.
type Service struct {
	tx                repository.TxRunner
	projects          repository.ProjectRepository
	workspaces        repository.WorkspaceRepository
	sessions          repository.SessionRepository
	contexts          repository.SessionContextRepository
	policies          repository.AgentSecurityPolicyRepository
	agents            repository.SessionAgentRepository
	turns             repository.TurnRepository
	toolInvocations   repository.ToolInvocationRepository
	queuedWork        repository.QueuedWorkRepository
	controls          repository.AgentControlCommandRepository
	sessionMessages   repository.SessionMessageRepository
	agentMessages     repository.AgentMessageRepository
	definitions       AgentDefinitionFactory
	policyFactory     AgentSecurityPolicyFactory
	messages          repository.MessageLoader
	usageRecords      func(context.Context, string) ([]agentruntime.ModelUsageRecord, error)
	removeSessionData func(context.Context, contracts.SessionID, []string) error
	contextBuilder    contextmodel.ContextBuilder
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
	Policy       securitymodel.AgentSecurityPolicy
}

// CreateResult returns the initialized Session and primary Agent.
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
		"transactions":      config.Transactions,
		"projects":          config.Projects,
		"workspaces":        config.Workspaces,
		"sessions":          config.Sessions,
		"session contexts":  config.Contexts,
		"security policies": config.Policies,
		"agents":            config.Agents,
		"turns":             config.Turns,
		"tool invocations":  config.ToolInvocations,
		"queued work":       config.QueuedWork,
		"controls":          config.Controls,
		"session messages":  config.SessionMessages,
		"agent messages":    config.AgentMessages,
		"agent definitions": config.Definitions,
		"policy factory":    config.PolicyFactory,
		"message loader":    config.Messages,
	} {
		if value == nil {
			return nil, fmt.Errorf("session service %s is required", name)
		}
	}
	return &Service{
		tx:                config.Transactions,
		projects:          config.Projects,
		workspaces:        config.Workspaces,
		sessions:          config.Sessions,
		contexts:          config.Contexts,
		policies:          config.Policies,
		agents:            config.Agents,
		turns:             config.Turns,
		toolInvocations:   config.ToolInvocations,
		queuedWork:        config.QueuedWork,
		controls:          config.Controls,
		sessionMessages:   config.SessionMessages,
		agentMessages:     config.AgentMessages,
		definitions:       config.Definitions,
		policyFactory:     config.PolicyFactory,
		messages:          config.Messages,
		usageRecords:      config.UsageRecords,
		removeSessionData: config.RemoveSessionData,
		contextBuilder:    contextmodel.NewContextBuilder(0, 0),
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

// RecoverStaleTurns 将上一次进程异常退出留下的活动 Turn 收敛为中断。
// 工具调用结果未知时只写入短错误结果，不自动重放可能已经产生副作用的调用。
func (s *Service) RecoverStaleTurns(ctx context.Context) error {
	if ctx == nil {
		return errors.New("stale turn recovery context is required")
	}
	turns, err := s.turns.ListActive(ctx)
	if err != nil {
		return err
	}
	for index := range turns {
		if err := s.recoverStaleTurn(ctx, turns[index].ID); err != nil {
			return fmt.Errorf("recover stale turn %s: %w", turns[index].ID, err)
		}
	}
	return nil
}

func (s *Service) recoverStaleTurn(ctx context.Context, turnID contracts.TurnID) error {
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		turn, err := s.turns.Get(txCtx, turnID)
		if errors.Is(err, contracts.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !turn.Active() {
			return nil
		}
		agent, err := s.agents.Get(txCtx, turn.AgentID)
		if err != nil {
			return err
		}
		if agent.CurrentTurnID != turn.ID ||
			(agent.State != agentmodel.AgentExecuting && agent.State != agentmodel.AgentPausing) {
			return fmt.Errorf("active turn does not match agent state")
		}
		invocations, err := s.toolInvocations.ListUnsettledByTurn(txCtx, turn.ID)
		if err != nil {
			return err
		}
		at := s.clock.Now().UTC()
		outcome := turnmodel.TurnInterrupted
		failureCode := contracts.TurnFailureInterrupted
		controls, err := s.controls.ListOpenByAgent(txCtx, agent.ID, 100)
		if err != nil {
			return err
		}
		// 恢复只收敛状态，不重放执行；已提交的用户控制命令仍保留其取消原因。
		for index := range controls {
			control := &controls[index]
			if control.TargetTurnID != turn.ID {
				continue
			}
			if failureCode != contracts.TurnFailureRequestCanceled {
				outcome = turnmodel.TurnPaused
			}
			if control.Kind == workflowmodel.AgentControlCancel {
				outcome = turnmodel.TurnInterrupted
			}
			failureCode = contracts.TurnFailureRequestCanceled
			if err := control.MarkApplied(at); err != nil {
				return err
			}
			if err := s.controls.Save(txCtx, *control); err != nil {
				return err
			}
		}
		for index := range invocations {
			invocation := invocations[index]
			result, err := recoverToolInvocation(&invocation, at)
			if err != nil {
				return err
			}
			if err := s.toolInvocations.Save(txCtx, invocation); err != nil {
				return err
			}
			if err := s.appendRecoveredToolResult(txCtx, agent, turn, invocation, result, at); err != nil {
				return err
			}
		}
		if turn.Status == turnmodel.TurnStarting {
			if err := turn.MarkRunning(at); err != nil {
				return err
			}
		}
		if turn.Status == turnmodel.TurnRunning {
			if err := turn.BeginEnding(at); err != nil {
				return err
			}
		}
		if err := turn.End(outcome, failureCode, at); err != nil {
			return err
		}
		if err := agent.EndTurn(outcome, at); err != nil {
			return err
		}
		if turn.WorkItemID != "" {
			work, err := s.queuedWork.Get(txCtx, turn.WorkItemID)
			if err != nil {
				return err
			}
			if err := work.End(turn.ID, outcome, failureCode, at); err != nil {
				return err
			}
			if err := s.queuedWork.Save(txCtx, work); err != nil {
				return err
			}
		}
		if err := s.turns.Save(txCtx, turn); err != nil {
			return err
		}
		return s.agents.Save(txCtx, agent)
	})
}

func recoverToolInvocation(invocation *toolmodel.ToolInvocation, at time.Time) (toolcontracts.ToolResult, error) {
	var result toolcontracts.ToolResult
	switch invocation.Status {
	case toolmodel.ToolInvocationRequested, toolmodel.ToolInvocationAwaitingApproval, toolmodel.ToolInvocationApproved:
		result = toolcontracts.NewToolError(string(toolmodel.ToolFailureInterrupted), "")
		err := invocation.Interrupt(toolmodel.ToolInvocationResult{
			InlineContent: result.Payload,
			ErrorCode:     toolmodel.ToolFailureInterrupted,
		}, at)
		return result, err
	case toolmodel.ToolInvocationRunning:
		result = toolcontracts.NewToolError(string(toolmodel.ToolFailureResultUnknown), "")
		err := invocation.MarkResultUnknown(toolmodel.ToolInvocationResult{
			InlineContent: result.Payload,
			ErrorCode:     toolmodel.ToolFailureResultUnknown,
		}, at)
		return result, err
	default:
		return result, fmt.Errorf("tool invocation %s is not recoverable", invocation.ID)
	}
}

func (s *Service) appendRecoveredToolResult(
	ctx context.Context,
	agent agentmodel.Agent,
	turn turnmodel.Turn,
	invocation toolmodel.ToolInvocation,
	result toolcontracts.ToolResult,
	at time.Time,
) error {
	message := sessionmodel.MessageData{
		ID:         "tool-recovery:" + invocation.ID.String(),
		RequestID:  turn.RequestID.String(),
		TurnID:     turn.ID.String(),
		Role:       sessionmodel.RoleTool,
		AuthorKind: sessionmodel.AuthorTool,
		AuthorID:   string(invocation.Name),
		Blocks: []sessionmodel.Block{{
			Kind:    sessionmodel.BlockToolResult,
			Text:    result.Payload,
			CallID:  invocation.ProviderToolCallID,
			Name:    string(invocation.Name),
			IsError: true,
		}},
		CreatedAt: at,
	}
	if agent.CanReadSessionContext() {
		_, err := s.sessionMessages.Append(ctx, sessionmodel.SessionMessage{SessionID: agent.SessionID, Data: message})
		return err
	}
	_, err := s.agentMessages.Append(ctx, agentmodel.AgentMessage{AgentID: agent.ID, Data: message})
	return err
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
func (s *Service) ListSessionContext(ctx context.Context, sessionID contracts.SessionID, afterID string, limit int) ([]contextmodel.SessionContextEntry, error) {
	if ctx == nil {
		return nil, contracts.New(contracts.InvalidRequest, "session context query is invalid")
	}
	if _, err := s.sessions.Get(ctx, sessionID); err != nil {
		return nil, err
	}
	return s.contexts.List(ctx, sessionID, afterID, limit)
}

// CreateSession 在一个事务中创建 Session、主 Agent 和初始策略。
func (s *Service) CreateSession(ctx context.Context, params CreateParams) (CreateResult, error) {
	if ctx == nil {
		return CreateResult{}, errors.New("create session context is required")
	}
	// 创建与退出清理串行，保证已提交的新会话不会漏过清理范围。
	s.createdSessionsMu.Lock()
	defer s.createdSessionsMu.Unlock()
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
	var result CreateResult
	created := false
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

// GetOrCreatePrimaryAgent returns an existing Session Agent or atomically
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
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		if err := s.policies.Save(txCtx, agent.ID, policy); err != nil {
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
	Kind         contextmodel.SessionContextKind
	SourceTurnID contracts.TurnID
	Content      string
}

// AppendContextResult returns the durable entry and retry state.
type AppendContextResult struct {
	Entry         contextmodel.SessionContextEntry
	ExistingEntry bool
}

// AppendSessionContext 按稳定 ID 幂等追加不可变共享上下文。
func (s *Service) AppendSessionContext(ctx context.Context, params AppendContextParams) (AppendContextResult, error) {
	if ctx == nil {
		return AppendContextResult{}, errors.New("append session context is required")
	}
	var result AppendContextResult
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		entry, err := contextmodel.NewSessionContextEntry(
			params.EntryID,
			params.SessionID,
			params.Kind,
			params.SourceTurnID,
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
				existing.SourceTurnID != params.SourceTurnID || existing.Content != entry.Content {
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
	messages, err := s.sessionMessages.List(ctx, sessionID, 0, 1)
	if err != nil || len(messages) > 0 {
		return len(messages) > 0, err
	}
	agents, err := s.agents.ListBySession(ctx, sessionID, math.MaxInt)
	if err != nil {
		return false, err
	}
	for _, agent := range agents {
		messages, err := s.agentMessages.List(ctx, agent.ID, 0, 1)
		if err != nil || len(messages) > 0 {
			return len(messages) > 0, err
		}
	}
	return false, nil
}

// RenameSession 更新会话标题，并刷新会话目录排序所使用的更新时间。
func (s *Service) RenameSession(ctx context.Context, sessionID contracts.SessionID, title string) error {
	if ctx == nil {
		return errors.New("rename session context is required")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return contracts.InvalidValue("title", "session title is required")
	}
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		return s.sessions.Rename(txCtx, sessionID, title, s.clock.Now())
	})
}
