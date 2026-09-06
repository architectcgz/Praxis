package orchestrate

import (
	"context"
	"fmt"
	corecommand "praxis/internal/core/command"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainsecurity "praxis/internal/core/domain/security"
	"sync"

	"praxis/internal/core/persistence"
	coreruntime "praxis/internal/core/runtime"
	"praxis/internal/core/system"
)

const (
	CommandErrorAgentExecuting          = corecommand.ErrorAgentExecuting
	CommandErrorAgentUnavailable        = corecommand.ErrorAgentUnavailable
	CommandErrorInvalidRequest          = corecommand.ErrorInvalidRequest
	CommandErrorNotReady                = corecommand.ErrorNotReady
	CommandErrorProjectWorkspaceInvalid = corecommand.ErrorProjectWorkspaceInvalid
	CommandErrorModelNotConfigured      = corecommand.ErrorModelNotConfigured
)

type CommandError = corecommand.Error

type ExecutionActivation interface {
	TryActivate(context.Context, domainfoundation.AgentID, coreruntime.ExecutionLifecycle) error
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

type AgentOrchestratorConfig struct {
	Transactions     persistence.TxRunner
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
	SecurityResolver *SecurityResolver
	Clock            system.Clock
	IDs              system.IDGenerator
	Activator        ExecutionActivation
	Models           ModelResolver
	InitiallyReady   bool
}

// AgentOrchestrator owns all durable command admission and product state
// mutation. Runtime components only receive already-created executions.
type AgentOrchestrator struct {
	tx              persistence.TxRunner
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
	security        SecurityResolver
	clock           system.Clock
	ids             system.IDGenerator
	activator       ExecutionActivation
	models          ModelResolver

	readinessMu sync.RWMutex
	ready       bool
	lifecycleMu sync.RWMutex
	lifecycle   coreruntime.ExecutionLifecycle
}

func NewAgentOrchestrator(config AgentOrchestratorConfig) (*AgentOrchestrator, error) {
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
		{name: "queued work", value: config.QueuedWork},
		{name: "waits", value: config.Waits},
		{name: "controls", value: config.Controls},
		{name: "deliveries", value: config.Deliveries},
		{name: "command receipts", value: config.CommandReceipts},
		{name: "events", value: config.Events},
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
		security:        *security,
		clock:           clock,
		ids:             ids,
		activator:       config.Activator,
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

// SetExecutionLifecycle supplies receipt acknowledgement for delivery-created
// executions until the delivery command is migrated to an application service.
func (o *AgentOrchestrator) SetExecutionLifecycle(lifecycle coreruntime.ExecutionLifecycle) error {
	if lifecycle == nil {
		return fmt.Errorf("execution lifecycle is required")
	}
	o.lifecycleMu.Lock()
	o.lifecycle = lifecycle
	o.lifecycleMu.Unlock()
	return nil
}

func (o *AgentOrchestrator) executionLifecycle() coreruntime.ExecutionLifecycle {
	o.lifecycleMu.RLock()
	defer o.lifecycleMu.RUnlock()
	return o.lifecycle
}

func (o *AgentOrchestrator) newID(prefix string) string {
	return o.ids.New(prefix)
}

func commandError(code corecommand.ErrorCode) *CommandError { return corecommand.NewError(code) }

func hasCommandErrorCode(err error, code corecommand.ErrorCode) bool {
	return corecommand.HasError(err, code)
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
