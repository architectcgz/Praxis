// Package start 负责用户输入和恢复操作的 Turn 创建。
package start

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	securitymodel "praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	turnmodel "praxis/internal/core/turn"

	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	runtimecontract "praxis/internal/agent_runtime"
	appcontext "praxis/internal/core/context"
	"praxis/internal/repository"
	"praxis/internal/system"
	"praxis/internal/utils/pathutil"
)

// RuntimeActivator 在 turn 创建提交后通知 Agent runtime。
type RuntimeActivator interface {
	Activate(context.Context, turnmodel.Turn, runtimecontract.TurnLifecycle) error
}

// PrimaryAgentProvider supplies a Session's durable primary Agent.
type PrimaryAgentProvider interface {
	GetOrCreatePrimaryAgent(context.Context, contracts.SessionID, contracts.RequestID) (agentmodel.Agent, error)
}

// ContextProvider 从执行主体可见的数据构建完整的 Provider 无关上下文。
type ContextProvider interface {
	BuildContext(context.Context, agentmodel.Agent, string, string, string) (appcontext.BuildResult, error)
}

// AgentDefinitionProvider 提供创建 turn 输入快照所需的 Agent 定义。
type AgentDefinitionProvider interface {
	Definition(contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

// ModelSnapshotFactory 将当前模型配置冻结为 turn 的不可变输入。
type ModelSnapshotFactory interface {
	FreezeTurnModel(string, string, string) (contracts.ModelSnapshot, error)
	FreezeDefaultTurnModel(contracts.AgentDefinitionID) (contracts.ModelSnapshot, error)
}

// Config 包含执行启动服务所需的依赖。
type Config struct {
	Transactions    repository.TxRunner
	Workspaces      repository.WorkspaceRepository
	Sessions        repository.SessionRepository
	Policies        repository.AgentSecurityPolicyRepository
	Agents          repository.SessionAgentRepository
	Turns           repository.TurnRepository
	SessionMessages repository.SessionMessageRepository
	AgentMessages   repository.AgentMessageRepository
	PrimaryAgent    PrimaryAgentProvider
	Security        *SecurityResolver
	ToolPermissions securitymodel.ToolPermissionPolicy
	RegisteredTools []contracts.ToolName
	Activator       RuntimeActivator
	Lifecycle       runtimecontract.TurnLifecycle
	Definitions     AgentDefinitionProvider
	// AgentDefinitionsDir 是已规范化的绝对配置目录，用于在准入失败时定位修复文件。
	AgentDefinitionsDir string
	Models              ModelSnapshotFactory
	ContextProvider     ContextProvider
	Clock               system.Clock
	IDs                 system.IDGenerator
}

// Service 负责持久化 Turn，并在提交后激活 runtime。
type Service struct {
	tx                  repository.TxRunner
	workspaces          repository.WorkspaceRepository
	sessions            repository.SessionRepository
	policies            repository.AgentSecurityPolicyRepository
	agents              repository.SessionAgentRepository
	turns               repository.TurnRepository
	sessionMessages     repository.SessionMessageRepository
	agentMessages       repository.AgentMessageRepository
	primary             PrimaryAgentProvider
	security            SecurityResolver
	securityMu          sync.RWMutex
	activator           RuntimeActivator
	lifecycle           runtimecontract.TurnLifecycle
	models              ModelSnapshotFactory
	definitions         AgentDefinitionProvider
	agentDefinitionsDir string
	contextProvider     ContextProvider
	clock               system.Clock
	ids                 system.IDGenerator
}

// SendInputParams 标识一次用户输入请求。
type SendInputParams struct {
	SessionID      contracts.SessionID
	AgentID        contracts.AgentID
	RequestID      contracts.RequestID
	Content        string
	ProviderID     string
	ModelID        string
	ReasoningLevel string
	Context        appcontext.BuildResult
}

// ResumeParams 标识一次回合恢复请求。
type ResumeParams struct {
	AgentID   contracts.AgentID
	RequestID contracts.RequestID
	Content   string
	Context   appcontext.BuildResult
}

// Result 返回持久化 Turn，并说明是否命中已有请求。
type Result struct {
	Turn            turnmodel.Turn
	ExistingRequest bool
	ActivationError string
}

// NewService 创建回合启动服务，依赖不完整时返回错误。
func NewService(config Config) (*Service, error) {
	if !pathutil.IsAbsoluteNormalized(config.AgentDefinitionsDir) {
		return nil, errors.New("turn start service agent definitions directory must be an absolute normalized path")
	}
	for name, value := range map[string]any{
		"transactions":           config.Transactions,
		"workspaces":             config.Workspaces,
		"sessions":               config.Sessions,
		"security policies":      config.Policies,
		"agents":                 config.Agents,
		"turns":                  config.Turns,
		"session messages":       config.SessionMessages,
		"agent messages":         config.AgentMessages,
		"primary agent provider": config.PrimaryAgent,
		"agent definitions":      config.Definitions,
		"turn model factory":     config.Models,
		"context provider":       config.ContextProvider,
	} {
		if value == nil {
			return nil, fmt.Errorf("turn start service %s is required", name)
		}
	}
	ids := system.IDsOrDefault(config.IDs)
	security := config.Security
	if security == nil {
		baseline, err := systemSecurityBaseline(config.RegisteredTools)
		if err != nil {
			return nil, err
		}
		resolved, err := NewSecurityResolver(baseline, config.ToolPermissions)
		if err != nil {
			return nil, err
		}
		security = &resolved
	}
	return &Service{
		tx: config.Transactions, workspaces: config.Workspaces, sessions: config.Sessions,
		policies: config.Policies, agents: config.Agents,
		turns: config.Turns, primary: config.PrimaryAgent,
		sessionMessages: config.SessionMessages, agentMessages: config.AgentMessages,
		security: *security, activator: config.Activator, lifecycle: config.Lifecycle,
		models: config.Models, definitions: config.Definitions, contextProvider: config.ContextProvider,
		agentDefinitionsDir: config.AgentDefinitionsDir,
		clock:               system.ClockOrDefault(config.Clock), ids: ids,
	}, nil
}

// SendInput 保存用户输入回合并请求激活，重复请求返回已有结果。
func (s *Service) SendInput(ctx context.Context, params SendInputParams) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("send input context is required")
	}
	if params.AgentID == "" {
		agent, err := s.primary.GetOrCreatePrimaryAgent(ctx, params.SessionID, params.RequestID)
		if err != nil {
			return Result{}, err
		}
		params.AgentID = agent.ID
	}
	var result Result
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.turns.FindByRequest(txCtx, params.AgentID, params.RequestID)
		if err == nil {
			if !requestMatches(existing, turnmodel.TurnUserInput, params.Content) || !modelMatches(existing, params.ProviderID, params.ModelID, params.ReasoningLevel) {
				return contracts.ErrRequestConflict
			}
			result = Result{Turn: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		if agent.SessionID != params.SessionID && params.SessionID != "" {
			return contracts.New(contracts.InvalidRequest, "")
		}
		if agent.State == agentmodel.AgentExecuting || agent.State == agentmodel.AgentPausing {
			return contracts.New(contracts.AgentExecuting, "")
		}
		if !agent.State.Startable() {
			return contracts.New(contracts.AgentUnavailable, "")
		}
		active, err := s.turns.CountActiveBySession(txCtx, agent.SessionID)
		if err != nil {
			return err
		}
		if active >= 1 {
			return contracts.New(contracts.AgentUnavailable, "")
		}
		input, err := s.materializeTurnInput(
			txCtx, agent, params.ProviderID, params.ModelID, params.ReasoningLevel,
			params.Content, "input:"+params.RequestID.String(), params.Context,
		)
		if err != nil {
			return err
		}
		at := s.clock.Now().UTC()
		turn, err := turnmodel.NewTurn(contracts.TurnID(s.ids.New("turn")), agent.SessionID, agent.ID, params.RequestID, turnmodel.TurnUserInput, params.Content, input, at)
		if err != nil {
			return err
		}
		if err := agent.Start(turn.ID, at); err != nil {
			return err
		}
		if err := s.turns.Save(txCtx, turn); err != nil {
			return err
		}
		if params.Content != "" {
			if err := s.appendInputMessage(txCtx, agent, turn, params.Content, at); err != nil {
				return err
			}
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Turn = turn
		return nil
	})
	if err != nil {
		if errors.Is(err, contracts.ErrRequestConflict) {
			existing, lookupErr := s.turns.FindByRequest(ctx, params.AgentID, params.RequestID)
			if lookupErr != nil {
				return Result{}, lookupErr
			}
			if !requestMatches(existing, turnmodel.TurnUserInput, params.Content) || !modelMatches(existing, params.ProviderID, params.ModelID, params.ReasoningLevel) {
				return Result{}, err
			}
			return Result{Turn: existing, ExistingRequest: true}, nil
		}
		return Result{}, err
	}
	return s.activate(ctx, result)
}

// Resume 为暂停或中断的 Agent 创建新的恢复回合。
func (s *Service) Resume(ctx context.Context, params ResumeParams) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("resume context is required")
	}
	var result Result
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.turns.FindByRequest(txCtx, params.AgentID, params.RequestID)
		if err == nil {
			if !requestMatches(existing, turnmodel.TurnResume, params.Content) {
				return contracts.ErrRequestConflict
			}
			result = Result{Turn: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		if agent.State != agentmodel.AgentPaused && agent.State != agentmodel.AgentInterrupted {
			return contracts.New(contracts.AgentUnavailable, "")
		}
		inputMessageID := ""
		if params.Content != "" {
			inputMessageID = "input:" + params.RequestID.String()
		}
		input, err := s.materializeTurnInput(
			txCtx, agent, "", "", "", params.Content, inputMessageID, params.Context,
		)
		if err != nil {
			return err
		}
		at := s.clock.Now().UTC()
		turn, err := turnmodel.NewTurn(contracts.TurnID(s.ids.New("turn")), agent.SessionID, agent.ID, params.RequestID, turnmodel.TurnResume, params.Content, input, at)
		if err != nil {
			return err
		}
		previous, err := s.turns.ListByAgent(txCtx, agent.ID, 1)
		if err != nil {
			return err
		}
		if len(previous) > 0 {
			turn.ParentTurnID = previous[0].ID
		}
		if err := turn.Validate(); err != nil {
			return err
		}
		if err := agent.Resume(turn.ID, at); err != nil {
			return err
		}
		if err := s.turns.Save(txCtx, turn); err != nil {
			return err
		}
		if params.Content != "" {
			if err := s.appendInputMessage(txCtx, agent, turn, params.Content, at); err != nil {
				return err
			}
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Turn = turn
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return s.activate(ctx, result)
}

func (s *Service) activate(ctx context.Context, result Result) (Result, error) {
	if result.ExistingRequest || s.activator == nil || s.lifecycle == nil {
		return result, nil
	}
	if err := s.activator.Activate(context.WithoutCancel(ctx), result.Turn, s.lifecycle); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

func (s *Service) appendInputMessage(
	ctx context.Context,
	agent agentmodel.Agent,
	turn turnmodel.Turn,
	content string,
	at time.Time,
) error {
	value := sessionmodel.MessageData{
		ID: "input:" + turn.RequestID.String(), RequestID: turn.RequestID.String(),
		TurnID: turn.ID.String(), Role: sessionmodel.RoleUser, AuthorKind: sessionmodel.AuthorUser,
		Blocks: []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: content}}, CreatedAt: at.UTC(),
	}
	if agent.CanReadSessionContext() {
		_, err := s.sessionMessages.Append(ctx, sessionmodel.SessionMessage{SessionID: agent.SessionID, Data: value})
		return err
	}
	_, err := s.agentMessages.Append(ctx, agentmodel.AgentMessage{AgentID: agent.ID, Data: value})
	return err
}

func requestMatches(turn turnmodel.Turn, reason turnmodel.TurnReason, content string) bool {
	return turn.Reason == reason && (turn.StartContent == content || turn.StartContent == "" && content == "")
}

func modelMatches(turn turnmodel.Turn, providerID, modelID, reasoningLevel string) bool {
	selection := turn.Input.Model
	return (providerID == "" || selection.ProviderID == providerID) &&
		(modelID == "" || selection.ModelID == modelID) &&
		(reasoningLevel == "" || selection.ReasoningLevel == reasoningLevel)
}

// UpdateToolPermissions 仅影响后续生成的 Turn 安全快照，不修改运行中的执行权限。
func (s *Service) UpdateToolPermissions(policy securitymodel.ToolPermissionPolicy) {
	s.securityMu.Lock()
	defer s.securityMu.Unlock()
	s.security.ToolPermissions = policy
}
