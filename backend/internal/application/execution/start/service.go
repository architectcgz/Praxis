// Package start owns durable AgentExecution creation for user input and resume.
package start

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corecommand "praxis/internal/core/command"
	domainagent "praxis/internal/core/domain/agent"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainsecurity "praxis/internal/core/domain/security"
	"praxis/internal/core/persistence"
	coreruntime "praxis/internal/core/runtime"
	"praxis/internal/core/system"
)

// Readiness controls command admission while startup recovery is in progress.
type Readiness interface {
	Ready() bool
}

// PrimaryAgentProvider supplies a Session's durable primary Agent.
type PrimaryAgentProvider interface {
	GetOrCreatePrimaryAgent(context.Context, domainfoundation.SessionID, domainfoundation.RequestID) (domainagent.Agent, error)
}

// InputFactory freezes the current context, policy and model selection into
// an immutable execution input snapshot.
type InputFactory interface {
	MaterializeExecutionInput(context.Context, domainagent.Agent, string, string, string) (domainexecution.ExecutionInputSnapshot, error)
}

// RuntimeActivator notifies the scheduler after execution creation commits.
type RuntimeActivator interface {
	TryActivate(context.Context, domainfoundation.AgentID, coreruntime.ExecutionLifecycle) error
}

// ModelResolver resolves a durable model selection for resume requests.
type ModelResolver interface {
	ResolveModel(domainsecurity.AgentProfile) (domainsecurity.ModelSelection, error)
	ResolveModelSelection(string, string, string) (domainsecurity.ModelSelection, error)
}

// Config contains the ports required by the execution start service.
type Config struct {
	Transactions persistence.TxRunner
	Workspaces   persistence.WorkspaceRepository
	Sessions     persistence.SessionRepository
	Contexts     persistence.SessionContextRepository
	Policies     persistence.AgentSecurityPolicyRepository
	Agents       persistence.AgentRepository
	Executions   persistence.AgentExecutionRepository
	Deliveries   persistence.ContextDeliveryRepository
	Events       persistence.EventRepository
	Readiness    Readiness
	PrimaryAgent PrimaryAgentProvider
	Security     *SecurityResolver
	Activator    RuntimeActivator
	Lifecycle    coreruntime.ExecutionLifecycle
	Models       ModelResolver
	Clock        system.Clock
	IDs          system.IDGenerator
}

// Service owns AgentExecution creation and post-commit activation.
type Service struct {
	tx         persistence.TxRunner
	workspaces persistence.WorkspaceRepository
	sessions   persistence.SessionRepository
	contexts   persistence.SessionContextRepository
	policies   persistence.AgentSecurityPolicyRepository
	agents     persistence.AgentRepository
	executions persistence.AgentExecutionRepository
	deliveries persistence.ContextDeliveryRepository
	events     persistence.EventRepository
	readiness  Readiness
	primary    PrimaryAgentProvider
	security   SecurityResolver
	activator  RuntimeActivator
	lifecycle  coreruntime.ExecutionLifecycle
	models     ModelResolver
	clock      system.Clock
	ids        system.IDGenerator
}

// SendInputParams identifies one user-input execution request.
type SendInputParams struct {
	SessionID  domainfoundation.SessionID
	AgentID    domainfoundation.AgentID
	RequestID  domainfoundation.RequestID
	Content    string
	ProviderID string
	ModelID    string
	Reasoning  string
}

// ResumeParams identifies one resumed execution request.
type ResumeParams struct {
	AgentID   domainfoundation.AgentID
	RequestID domainfoundation.RequestID
	Content   string
}

// Result reports the durable execution and whether an existing request won.
type Result struct {
	Execution       domainexecution.AgentExecution
	ExistingRequest bool
	ActivationError string
}

// NewService creates the execution start service.
func NewService(config Config) (*Service, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{name: "transactions", value: config.Transactions},
		{name: "workspaces", value: config.Workspaces},
		{name: "sessions", value: config.Sessions},
		{name: "session contexts", value: config.Contexts},
		{name: "security policies", value: config.Policies},
		{name: "agents", value: config.Agents},
		{name: "executions", value: config.Executions},
		{name: "deliveries", value: config.Deliveries},
		{name: "events", value: config.Events},
		{name: "readiness", value: config.Readiness},
		{name: "primary agent provider", value: config.PrimaryAgent},
		{name: "model resolver", value: config.Models},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("execution start service %s is required", required.name)
		}
	}
	clock := config.Clock
	if clock == nil {
		clock = system.UTCClock{}
	}
	ids := config.IDs
	if ids == nil {
		ids = system.SecureIDGenerator{}
	}
	security := config.Security
	if security == nil {
		baseline, err := systemSecurityBaseline()
		if err != nil {
			return nil, err
		}
		resolved, err := NewSecurityResolver(baseline, ids)
		if err != nil {
			return nil, err
		}
		security = &resolved
	}
	return &Service{tx: config.Transactions, workspaces: config.Workspaces, sessions: config.Sessions, contexts: config.Contexts, policies: config.Policies, agents: config.Agents, executions: config.Executions, deliveries: config.Deliveries, events: config.Events, readiness: config.Readiness, primary: config.PrimaryAgent, security: *security, activator: config.Activator, lifecycle: config.Lifecycle, models: config.Models, clock: clock, ids: ids}, nil
}

// SendInput creates one durable user-input execution and then requests activation.
func (s *Service) SendInput(ctx context.Context, params SendInputParams) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("send input context is required")
	}
	if !s.readiness.Ready() {
		return Result{}, corecommand.NewError(corecommand.ErrorNotReady)
	}
	if params.RequestID == "" || strings.TrimSpace(params.Content) == "" || (params.SessionID == "" && params.AgentID == "") {
		return Result{}, corecommand.NewError(corecommand.ErrorInvalidRequest)
	}
	if strings.TrimSpace(params.ProviderID) == "" || strings.TrimSpace(params.ModelID) == "" {
		return Result{}, corecommand.NewError(corecommand.ErrorModelNotConfigured)
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
			if !requestMatches(existing, domainexecution.ExecutionUserInput, params.Content) || !modelMatches(existing, params.ProviderID, params.ModelID, params.Reasoning) {
				return domainfoundation.ErrRequestConflict
			}
			result = Result{Execution: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, domainfoundation.ErrNotFound) {
			return err
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		if agent.SessionID != params.SessionID && params.SessionID != "" {
			return corecommand.NewError(corecommand.ErrorInvalidRequest)
		}
		switch agent.State {
		case domainagent.AgentExecuting, domainagent.AgentPausing:
			return corecommand.NewError(corecommand.ErrorAgentExecuting)
		case domainagent.AgentIdle, domainagent.AgentWaiting, domainagent.AgentFailed, domainagent.AgentClosed:
		default:
			return corecommand.NewError(corecommand.ErrorAgentUnavailable)
		}
		delivering, err := s.deliveries.HasDeliveringByTarget(txCtx, agent.ID)
		if err != nil {
			return err
		}
		if delivering {
			return corecommand.NewError(corecommand.ErrorAgentUnavailable)
		}
		active, err := s.executions.CountActiveBySession(txCtx, agent.SessionID)
		if err != nil {
			return err
		}
		if active >= 1 {
			return corecommand.NewError(corecommand.ErrorAgentUnavailable)
		}
		input, err := s.MaterializeExecutionInput(txCtx, agent, params.ProviderID, params.ModelID, params.Reasoning)
		if err != nil {
			return err
		}
		at := s.clock.Now().UTC()
		execution, err := domainexecution.NewAgentExecution(domainfoundation.AgentExecutionID(s.ids.New("execution")), agent.SessionID, agent.ID, params.RequestID, domainexecution.ExecutionUserInput, params.Content, input, at)
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
		if err := s.appendStartedEvent(txCtx, execution, at); err != nil {
			return err
		}
		result.Execution = execution
		return nil
	})
	if err != nil {
		if errors.Is(err, domainfoundation.ErrRequestConflict) {
			existing, lookupErr := s.executions.FindByRequest(ctx, params.AgentID, params.RequestID)
			if lookupErr != nil {
				return Result{}, lookupErr
			}
			if !requestMatches(existing, domainexecution.ExecutionUserInput, params.Content) || !modelMatches(existing, params.ProviderID, params.ModelID, params.Reasoning) {
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
	if !s.readiness.Ready() || params.AgentID == "" || params.RequestID == "" {
		return Result{}, corecommand.NewError(corecommand.ErrorInvalidRequest)
	}
	var result Result
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.executions.FindByRequest(txCtx, params.AgentID, params.RequestID)
		if err == nil {
			if !requestMatches(existing, domainexecution.ExecutionResume, params.Content) {
				return domainfoundation.ErrRequestConflict
			}
			result = Result{Execution: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, domainfoundation.ErrNotFound) {
			return err
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		if agent.State != domainagent.AgentPaused && agent.State != domainagent.AgentInterrupted {
			return corecommand.NewError(corecommand.ErrorAgentUnavailable)
		}
		model, err := s.models.ResolveModel(agent.Profile)
		if err != nil {
			return fmt.Errorf("resolve model: %w", err)
		}
		input, err := s.MaterializeExecutionInput(txCtx, agent, model.ProviderID, model.ModelID, model.Reasoning)
		if err != nil {
			return err
		}
		at := s.clock.Now().UTC()
		execution, err := domainexecution.NewAgentExecution(domainfoundation.AgentExecutionID(s.ids.New("execution")), agent.SessionID, agent.ID, params.RequestID, domainexecution.ExecutionResume, params.Content, input, at)
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
		if err := s.appendStartedEvent(txCtx, execution, at); err != nil {
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
	if err := s.activator.TryActivate(context.WithoutCancel(ctx), result.Execution.AgentID, s.lifecycle); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

func (s *Service) appendStartedEvent(ctx context.Context, execution domainexecution.AgentExecution, at time.Time) error {
	return s.events.Append(ctx, domainfoundation.DomainEvent{ID: domainfoundation.EventID(s.ids.New("event")), Type: domainfoundation.EventExecutionStarted, SessionID: execution.SessionID, AgentID: execution.AgentID, AgentExecutionID: execution.ID, OccurredAt: at.UTC()})
}

func requestMatches(execution domainexecution.AgentExecution, reason domainexecution.ExecutionReason, content string) bool {
	return execution.Reason == reason && (execution.StartContent == content || execution.StartContent == "" && content == "")
}

func modelMatches(execution domainexecution.AgentExecution, providerID, modelID, reasoning string) bool {
	grant := execution.Input.Security.CapabilityGrant
	return (providerID == "" || grant.Model.ProviderID == providerID) && (modelID == "" || grant.Model.ModelID == modelID) && (reasoning == "" || grant.Model.Reasoning == reasoning)
}
