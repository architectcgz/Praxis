package orchestrate

import (
	"context"
	"errors"
	"fmt"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainsecurity "praxis/internal/core/domain/security"
	domainworkspace "praxis/internal/core/domain/workspace"
	"sync"

	"praxis/internal/core/persistence"
	coreruntime "praxis/internal/core/runtime"
	coresession "praxis/internal/core/session"
	"praxis/internal/core/system"
)

const (
	CommandErrorAgentExecuting          = "agent_executing"
	CommandErrorAgentUnavailable        = "agent_unavailable"
	CommandErrorInvalidRequest          = "invalid_request"
	CommandErrorNotReady                = "orchestration_not_ready"
	CommandErrorProjectWorkspaceInvalid = "project_workspace_invalid"
	CommandErrorModelNotConfigured      = "model_not_configured"
)

// CommandError is stable at the app boundary and never exposes persistence or
// provider error text as a user-facing command outcome.
type CommandError struct {
	Code string
}

func (e *CommandError) Error() string { return e.Code }

type ExecutionActivation interface {
	TryActivate(context.Context, domainfoundation.AgentID, coreruntime.ExecutionLifecycle) error
}

type ExecutionCancellation interface {
	Cancel(
		ctx context.Context,
		agentID domainfoundation.AgentID,
		executionID domainfoundation.AgentExecutionID,
		outcome domainexecution.ExecutionOutcome,
	) error
}

// ModelResolver maps an agent profile to a configured model snapshot before
// a Grant is persisted. The model selection becomes immutable execution input.
type ModelResolver interface {
	ResolveModel(domainsecurity.AgentProfile) (domainsecurity.ModelSelection, error)
}

// ModelSelectionResolver validates a user-selected model capability before it
// is frozen into an execution-specific CapabilityGrant.
type ModelSelectionResolver interface {
	ResolveModelSelection(string, string, string) (domainsecurity.ModelSelection, error)
}

// AgentMessageQuery is the core projection port for an Agent transcript. The
// storage adapter owns the physical JSONL file; core owns identity validation
// and the query boundary exposed to bindings.
type AgentMessageQuery func(context.Context, domainfoundation.SessionID, domainfoundation.AgentID, int) ([]coresession.AgentSessionMessage, error)

type AgentSecurityPolicyFactory func(domainworkspace.Workspace, domainsecurity.AgentProfile) (domainsecurity.AgentSecurityPolicy, error)

type AgentOrchestratorConfig struct {
	Transactions     persistence.TxRunner
	Projects         persistence.ProjectRepository
	Workspaces       persistence.WorkspaceRepository
	Sessions         persistence.SessionRepository
	Contexts         persistence.SessionContextRepository
	Policies         persistence.AgentSecurityPolicyRepository
	Agents           persistence.AgentRepository
	Executions       persistence.AgentExecutionRepository
	QueuedWork       persistence.QueuedWorkRepository
	Waits            persistence.WaitConditionRepository
	Controls         persistence.AgentControlRequestRepository
	Deliveries       persistence.ContextDeliveryRepository
	CommandReceipts  persistence.CommandReceiptRepository
	Events           persistence.EventRepository
	Messages         AgentMessageQuery
	SecurityResolver *SecurityResolver
	PolicyFactory    AgentSecurityPolicyFactory
	Clock            system.Clock
	IDs              system.IDGenerator
	Activator        ExecutionActivation
	Canceller        ExecutionCancellation
	Models           ModelResolver
	InitiallyReady   bool
}

// AgentOrchestrator owns all durable command admission and product state
// mutation. Runtime components only receive already-created executions.
type AgentOrchestrator struct {
	tx              persistence.TxRunner
	projects        persistence.ProjectRepository
	workspaces      persistence.WorkspaceRepository
	sessions        persistence.SessionRepository
	contexts        persistence.SessionContextRepository
	policies        persistence.AgentSecurityPolicyRepository
	agents          persistence.AgentRepository
	executions      persistence.AgentExecutionRepository
	queuedWork      persistence.QueuedWorkRepository
	waits           persistence.WaitConditionRepository
	controls        persistence.AgentControlRequestRepository
	deliveries      persistence.ContextDeliveryRepository
	commandReceipts persistence.CommandReceiptRepository
	events          persistence.EventRepository
	messages        AgentMessageQuery
	security        SecurityResolver
	policyFactory   AgentSecurityPolicyFactory
	clock           system.Clock
	ids             system.IDGenerator
	activator       ExecutionActivation
	canceller       ExecutionCancellation
	models          ModelResolver

	readinessMu sync.RWMutex
	ready       bool
}

func NewAgentOrchestrator(config AgentOrchestratorConfig) (*AgentOrchestrator, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{name: "transactions", value: config.Transactions},
		{name: "projects", value: config.Projects},
		{name: "workspaces", value: config.Workspaces},
		{name: "sessions", value: config.Sessions},
		{name: "session contexts", value: config.Contexts},
		{name: "security policies", value: config.Policies},
		{name: "agents", value: config.Agents},
		{name: "executions", value: config.Executions},
		{name: "queued work", value: config.QueuedWork},
		{name: "waits", value: config.Waits},
		{name: "controls", value: config.Controls},
		{name: "deliveries", value: config.Deliveries},
		{name: "command receipts", value: config.CommandReceipts},
		{name: "events", value: config.Events},
		{name: "message query", value: config.Messages},
		{name: "model resolver", value: config.Models},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("agent orchestrator %s is required", required.name)
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
	security := config.SecurityResolver
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
	return &AgentOrchestrator{
		tx:              config.Transactions,
		projects:        config.Projects,
		workspaces:      config.Workspaces,
		sessions:        config.Sessions,
		contexts:        config.Contexts,
		policies:        config.Policies,
		agents:          config.Agents,
		executions:      config.Executions,
		queuedWork:      config.QueuedWork,
		waits:           config.Waits,
		controls:        config.Controls,
		deliveries:      config.Deliveries,
		commandReceipts: config.CommandReceipts,
		events:          config.Events,
		messages:        config.Messages,
		security:        *security,
		policyFactory:   config.PolicyFactory,
		clock:           clock,
		ids:             ids,
		activator:       config.Activator,
		canceller:       config.Canceller,
		models:          config.Models,
		ready:           config.InitiallyReady,
	}, nil
}

// SetReady controls command admission during startup recovery and shutdown.
// It does not stop durable settlement that was already in progress.
func (o *AgentOrchestrator) SetReady(ready bool) {
	o.readinessMu.Lock()
	defer o.readinessMu.Unlock()
	o.ready = ready
}

func (o *AgentOrchestrator) Ready() bool {
	o.readinessMu.RLock()
	defer o.readinessMu.RUnlock()
	return o.ready
}

func (o *AgentOrchestrator) newID(prefix string) string {
	return o.ids.New(prefix)
}

func commandError(code string) *CommandError { return &CommandError{Code: code} }

func hasCommandErrorCode(err error, code string) bool {
	var command *CommandError
	return errors.As(err, &command) && command.Code == code
}

func (o *AgentOrchestrator) checkSessionCapacity(ctx context.Context, sessionID domainfoundation.SessionID) error {
	active, err := o.executions.CountActiveBySession(ctx, sessionID)
	if err != nil {
		return err
	}
	if active >= 1 {
		return commandError(CommandErrorAgentUnavailable)
	}
	return nil
}

func (o *AgentOrchestrator) rejectDeliveringInput(ctx context.Context, agentID domainfoundation.AgentID) error {
	delivering, err := o.deliveries.HasDeliveringByTarget(ctx, agentID)
	if err != nil {
		return err
	}
	if delivering {
		return commandError(CommandErrorAgentUnavailable)
	}
	return nil
}
