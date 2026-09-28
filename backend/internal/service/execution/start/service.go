// Package start owns durable AgentExecution creation for user input and resume.
package start

import (
	agentmodel "praxis/internal/agent"
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"
	securitymodel "praxis/internal/security"

	"context"
	"errors"
	"fmt"
	"strings"

	appcontext "praxis/internal/context"
	"praxis/internal/repository"
	runtimecontract "praxis/internal/runtime"
	"praxis/internal/system"
)

// RuntimeActivator notifies the scheduler after an execution creation commits.
type RuntimeActivator interface {
	TryActivate(context.Context, executionmodel.AgentExecution, runtimecontract.ExecutionLifecycle) error
}

// PrimaryAgentProvider supplies a Session's durable primary Agent.
type PrimaryAgentProvider interface {
	GetOrCreatePrimaryAgent(context.Context, contracts.SessionID, contracts.RequestID) (agentmodel.Agent, error)
}

// ContextProvider 从 Session 状态和 transcript 构建完整的 Provider 无关上下文。
type ContextProvider interface {
	BuildContext(context.Context, agentmodel.Agent, string, string) (appcontext.BuildResult, error)
}

// AgentDefinitionProvider 提供创建 execution 输入快照所需的 Agent 定义。
type AgentDefinitionProvider interface {
	Definition(contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

// ExecutionModelSnapshotFactory 将当前模型配置冻结为 execution 的不可变输入。
type ExecutionModelSnapshotFactory interface {
	FreezeExecutionModel(string, string, string) (contracts.ExecutionModelSnapshot, error)
	FreezeDefaultExecutionModel(contracts.AgentDefinitionID) (contracts.ExecutionModelSnapshot, error)
}

// Config 包含执行启动服务所需的依赖。
type Config struct {
	Transactions    repository.TxRunner
	Workspaces      repository.WorkspaceRepository
	Sessions        repository.SessionRepository
	Policies        repository.AgentSecurityPolicyRepository
	Agents          repository.SessionAgentRepository
	Executions      repository.AgentExecutionRepository
	PrimaryAgent    PrimaryAgentProvider
	Security        *SecurityResolver
	ToolPermissions securitymodel.ToolPermissionPolicy
	RegisteredTools []contracts.ToolName
	Activator       RuntimeActivator
	Lifecycle       runtimecontract.ExecutionLifecycle
	Definitions     AgentDefinitionProvider
	Models          ExecutionModelSnapshotFactory
	ContextProvider ContextProvider
	Clock           system.Clock
	IDs             system.IDGenerator
}

// Service owns AgentExecution creation and post-commit activation.
type Service struct {
	tx              repository.TxRunner
	workspaces      repository.WorkspaceRepository
	sessions        repository.SessionRepository
	policies        repository.AgentSecurityPolicyRepository
	agents          repository.SessionAgentRepository
	executions      repository.AgentExecutionRepository
	primary         PrimaryAgentProvider
	security        SecurityResolver
	activator       RuntimeActivator
	lifecycle       runtimecontract.ExecutionLifecycle
	models          ExecutionModelSnapshotFactory
	definitions     AgentDefinitionProvider
	contextProvider ContextProvider
	clock           system.Clock
	ids             system.IDGenerator
}

// SendInputParams identifies one user-input execution request.
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

// ResumeParams identifies one resumed execution request.
type ResumeParams struct {
	AgentID   contracts.AgentID
	RequestID contracts.RequestID
	Content   string
	Context   appcontext.BuildResult
}

// Result reports the durable execution and whether an existing request won.
type Result struct {
	Execution       executionmodel.AgentExecution
	ExistingRequest bool
	ActivationError string
}

// NewService creates the execution start service.
func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions":            config.Transactions,
		"workspaces":              config.Workspaces,
		"sessions":                config.Sessions,
		"security policies":       config.Policies,
		"agents":                  config.Agents,
		"executions":              config.Executions,
		"primary agent provider":  config.PrimaryAgent,
		"agent definitions":       config.Definitions,
		"execution model factory": config.Models,
		"context provider":        config.ContextProvider,
	} {
		if value == nil {
			return nil, fmt.Errorf("execution start service %s is required", name)
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
		executions: config.Executions, primary: config.PrimaryAgent,
		security: *security, activator: config.Activator, lifecycle: config.Lifecycle,
		models: config.Models, definitions: config.Definitions, contextProvider: config.ContextProvider,
		clock: system.ClockOrDefault(config.Clock), ids: ids,
	}, nil
}

// SendInput creates one durable user-input execution and then requests activation.
func (s *Service) SendInput(ctx context.Context, params SendInputParams) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("send input context is required")
	}
	if params.RequestID == "" || strings.TrimSpace(params.Content) == "" || (params.SessionID == "" && params.AgentID == "") {
		return Result{}, contracts.New(contracts.InvalidRequest, "")
	}
	if strings.TrimSpace(params.ProviderID) == "" || strings.TrimSpace(params.ModelID) == "" {
		return Result{}, contracts.New(contracts.ModelNotConfigured, "")
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
		existing, err := s.executions.FindByRequest(txCtx, params.AgentID, params.RequestID)
		if err == nil {
			if !requestMatches(existing, executionmodel.ExecutionUserInput, params.Content) || !modelMatches(existing, params.ProviderID, params.ModelID, params.ReasoningLevel) {
				return contracts.ErrRequestConflict
			}
			result = Result{Execution: existing, ExistingRequest: true}
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
		active, err := s.executions.CountActiveBySession(txCtx, agent.SessionID)
		if err != nil {
			return err
		}
		if active >= 1 {
			return contracts.New(contracts.AgentUnavailable, "")
		}
		input, err := s.materializeExecutionInput(txCtx, agent, params.ProviderID, params.ModelID, params.ReasoningLevel, params.Content, params.Context)
		if err != nil {
			return err
		}
		at := s.clock.Now().UTC()
		execution, err := executionmodel.NewAgentExecution(contracts.AgentExecutionID(s.ids.New("execution")), agent.SessionID, agent.ID, params.RequestID, executionmodel.ExecutionUserInput, params.Content, input, at)
		if err != nil {
			return err
		}
		if err := agent.Start(execution.ID, at); err != nil {
			return err
		}
		if err := s.executions.Save(txCtx, execution); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Execution = execution
		return nil
	})
	if err != nil {
		if errors.Is(err, contracts.ErrRequestConflict) {
			existing, lookupErr := s.executions.FindByRequest(ctx, params.AgentID, params.RequestID)
			if lookupErr != nil {
				return Result{}, lookupErr
			}
			if !requestMatches(existing, executionmodel.ExecutionUserInput, params.Content) || !modelMatches(existing, params.ProviderID, params.ModelID, params.ReasoningLevel) {
				return Result{}, err
			}
			return Result{Execution: existing, ExistingRequest: true}, nil
		}
		return Result{}, err
	}
	return s.activate(ctx, result)
}

// Resume creates a new execution for a paused or interrupted Agent.
func (s *Service) Resume(ctx context.Context, params ResumeParams) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("resume context is required")
	}
	if params.AgentID == "" || params.RequestID == "" {
		return Result{}, contracts.New(contracts.InvalidRequest, "")
	}
	var result Result
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.executions.FindByRequest(txCtx, params.AgentID, params.RequestID)
		if err == nil {
			if !requestMatches(existing, executionmodel.ExecutionResume, params.Content) {
				return contracts.ErrRequestConflict
			}
			result = Result{Execution: existing, ExistingRequest: true}
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
		input, err := s.materializeExecutionInput(txCtx, agent, "", "", "", params.Content, params.Context)
		if err != nil {
			return err
		}
		at := s.clock.Now().UTC()
		execution, err := executionmodel.NewAgentExecution(contracts.AgentExecutionID(s.ids.New("execution")), agent.SessionID, agent.ID, params.RequestID, executionmodel.ExecutionResume, params.Content, input, at)
		if err != nil {
			return err
		}
		previous, err := s.executions.ListByAgent(txCtx, agent.ID, 1)
		if err != nil {
			return err
		}
		if len(previous) > 0 {
			execution.ParentExecutionID = previous[0].ID
		}
		if err := execution.Validate(); err != nil {
			return err
		}
		if err := agent.Resume(execution.ID, at); err != nil {
			return err
		}
		if err := s.executions.Save(txCtx, execution); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Execution = execution
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
	if err := s.activator.TryActivate(context.WithoutCancel(ctx), result.Execution, s.lifecycle); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

func requestMatches(execution executionmodel.AgentExecution, reason executionmodel.ExecutionReason, content string) bool {
	return execution.Reason == reason && (execution.StartContent == content || execution.StartContent == "" && content == "")
}

func modelMatches(execution executionmodel.AgentExecution, providerID, modelID, reasoningLevel string) bool {
	selection := execution.Input.Model
	return (providerID == "" || selection.ProviderID == providerID) &&
		(modelID == "" || selection.ModelID == modelID) &&
		(reasoningLevel == "" || selection.ReasoningLevel == reasoningLevel)
}
