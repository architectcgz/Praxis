package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"praxis/internal/core/domain"
	"praxis/internal/core/persistence"
	coreruntime "praxis/internal/core/runtime"
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
	TryActivate(context.Context, domain.AgentID, coreruntime.ExecutionLifecycle) error
}

type ExecutionCancellation interface {
	Cancel(
		ctx context.Context,
		agentID domain.AgentID,
		executionID domain.AgentExecutionID,
		outcome domain.ExecutionOutcome,
	) error
}

// ModelResolver maps an agent profile to a configured model snapshot before
// a Grant is persisted. The model selection becomes immutable execution input.
type ModelResolver interface {
	ResolveModel(domain.AgentProfile) (domain.ModelSelection, error)
}

// ModelSelectionResolver validates a user-selected model capability before it
// is frozen into an execution-specific CapabilityGrant.
type ModelSelectionResolver interface {
	ResolveModelSelection(string, string, string) (domain.ModelSelection, error)
}

type AgentOrchestratorConfig struct {
	Transactions   persistence.TxRunner
	Projects       persistence.ProjectRepository
	Workspaces     persistence.WorkspaceRepository
	Sessions       persistence.SessionRepository
	Groups         persistence.AgentGroupRepository
	Agents         persistence.AgentRepository
	Executions     persistence.AgentExecutionRepository
	QueuedWork     persistence.QueuedWorkRepository
	TaskPackets    persistence.TaskPacketRepository
	Manifests      persistence.ContextManifestRepository
	Grants         persistence.CapabilityGrantRepository
	Waits          persistence.WaitConditionRepository
	Controls       persistence.AgentControlRequestRepository
	Deliveries     persistence.ContextDeliveryRepository
	Clock          system.Clock
	Activator      ExecutionActivation
	Canceller      ExecutionCancellation
	Models         ModelResolver
	InitiallyReady bool
}

// AgentOrchestrator owns all durable command admission and product state
// mutation. Runtime components only receive already-created executions.
type AgentOrchestrator struct {
	tx         persistence.TxRunner
	projects   persistence.ProjectRepository
	workspaces persistence.WorkspaceRepository
	sessions   persistence.SessionRepository
	groups     persistence.AgentGroupRepository
	agents     persistence.AgentRepository
	executions persistence.AgentExecutionRepository
	queuedWork persistence.QueuedWorkRepository
	packets    persistence.TaskPacketRepository
	manifests  persistence.ContextManifestRepository
	grants     persistence.CapabilityGrantRepository
	waits      persistence.WaitConditionRepository
	controls   persistence.AgentControlRequestRepository
	deliveries persistence.ContextDeliveryRepository
	clock      system.Clock
	activator  ExecutionActivation
	canceller  ExecutionCancellation
	models     ModelResolver

	readinessMu sync.RWMutex
	ready       bool
}

type targetSystemClock struct{}

func (targetSystemClock) Now() time.Time { return time.Now().UTC() }

func NewAgentOrchestrator(config AgentOrchestratorConfig) (*AgentOrchestrator, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{name: "transactions", value: config.Transactions},
		{name: "projects", value: config.Projects},
		{name: "workspaces", value: config.Workspaces},
		{name: "sessions", value: config.Sessions},
		{name: "groups", value: config.Groups},
		{name: "agents", value: config.Agents},
		{name: "executions", value: config.Executions},
		{name: "queued work", value: config.QueuedWork},
		{name: "task packets", value: config.TaskPackets},
		{name: "context manifests", value: config.Manifests},
		{name: "capability grants", value: config.Grants},
		{name: "waits", value: config.Waits},
		{name: "controls", value: config.Controls},
		{name: "deliveries", value: config.Deliveries},
		{name: "model resolver", value: config.Models},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("agent orchestrator %s is required", required.name)
		}
	}
	clock := config.Clock
	if clock == nil {
		clock = targetSystemClock{}
	}
	return &AgentOrchestrator{
		tx:         config.Transactions,
		projects:   config.Projects,
		workspaces: config.Workspaces,
		sessions:   config.Sessions,
		groups:     config.Groups,
		agents:     config.Agents,
		executions: config.Executions,
		queuedWork: config.QueuedWork,
		packets:    config.TaskPackets,
		manifests:  config.Manifests,
		grants:     config.Grants,
		waits:      config.Waits,
		controls:   config.Controls,
		deliveries: config.Deliveries,
		clock:      clock,
		activator:  config.Activator,
		canceller:  config.Canceller,
		models:     config.Models,
		ready:      config.InitiallyReady,
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

func commandError(code string) *CommandError { return &CommandError{Code: code} }

func hasCommandErrorCode(err error, code string) bool {
	var command *CommandError
	return errors.As(err, &command) && command.Code == code
}

func (o *AgentOrchestrator) checkGroupCapacity(ctx context.Context, groupID domain.AgentGroupID) error {
	group, err := o.groups.Get(ctx, groupID)
	if err != nil {
		return err
	}
	active, err := o.executions.CountActiveByGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if active >= group.MaxConcurrent {
		return commandError(CommandErrorAgentUnavailable)
	}
	return nil
}

func (o *AgentOrchestrator) rejectDeliveringInput(ctx context.Context, agentID domain.AgentID) error {
	delivering, err := o.deliveries.HasDeliveringByTarget(ctx, agentID)
	if err != nil {
		return err
	}
	if delivering {
		return commandError(CommandErrorAgentUnavailable)
	}
	return nil
}
