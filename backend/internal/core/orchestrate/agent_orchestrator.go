package orchestrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"praxis/internal/core/domain"
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
// a Grant is persisted. The model reference becomes immutable execution input.
type ModelResolver interface {
	ResolveModel(domain.AgentProfile) (domain.ModelRef, error)
}

// ModelSelectionResolver validates a user-selected model capability before it
// is frozen into an execution-specific CapabilityGrant.
type ModelSelectionResolver interface {
	ResolveModelSelection(string, string) (domain.ModelRef, error)
}

type AgentOrchestratorConfig struct {
	Transactions   persistence.TxRunner
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

type CreateSessionRequest struct {
	SessionID       domain.SessionID
	GroupID         domain.AgentGroupID
	AgentID         domain.AgentID
	Goal            string
	WorkspaceKey    string
	MaxConcurrent   int
	Profile         domain.AgentProfile
	TaskPacket      domain.TaskPacket
	ContextManifest domain.ContextManifest
	Grant           domain.CapabilityGrant
}

type CreateSessionResult struct {
	Session SessionSnapshot
	Group   domain.AgentGroup
	Agent   domain.Agent
}

type CreateProjectRequest struct {
	WorkspaceKey string
	Goal         string
}

type SessionSnapshot struct {
	ID           domain.SessionID
	Goal         string
	WorkspaceKey string
}

// CreateSession creates the minimum explicit Session -> AgentGroup -> Agent
// hierarchy and saves every execution input reference in the same transaction.
func (o *AgentOrchestrator) CreateSession(
	ctx context.Context,
	request CreateSessionRequest,
) (CreateSessionResult, error) {
	if ctx == nil {
		return CreateSessionResult{}, errors.New("create session context is required")
	}
	if !o.Ready() {
		return CreateSessionResult{}, commandError(CommandErrorNotReady)
	}
	if request.SessionID == "" {
		request.SessionID = domain.NewSessionID()
	}
	if request.GroupID == "" {
		request.GroupID = domain.NewAgentGroupID()
	}
	if request.AgentID == "" {
		request.AgentID = domain.NewAgentID()
	}
	if request.MaxConcurrent < 1 || !request.Profile.Valid() {
		return CreateSessionResult{}, commandError(CommandErrorInvalidRequest)
	}
	if err := request.TaskPacket.Validate(); err != nil {
		return CreateSessionResult{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	if err := request.ContextManifest.Validate(); err != nil {
		return CreateSessionResult{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	if err := request.Grant.Validate(); err != nil {
		return CreateSessionResult{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	if request.Grant.ContextManifestRef != request.ContextManifest.ID {
		return CreateSessionResult{}, commandError(CommandErrorInvalidRequest)
	}
	result := CreateSessionResult{}
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		at := o.clock.Now()
		session, err := domain.NewSession(request.SessionID, request.Goal, request.WorkspaceKey, at)
		if err != nil {
			return err
		}
		group, err := domain.NewAgentGroup(request.GroupID, session.ID, request.MaxConcurrent, at)
		if err != nil {
			return err
		}
		agent, err := domain.NewAgent(
			request.AgentID,
			session.ID,
			group.ID,
			request.Profile,
			request.TaskPacket.ID,
			request.ContextManifest.ID,
			request.Grant.ID,
			at,
		)
		if err != nil {
			return err
		}
		if request.Profile == domain.ProfilePrimary {
			if err := group.SetPrimary(agent.ID, at); err != nil {
				return err
			}
		}
		if err := o.packets.Save(txCtx, request.TaskPacket); err != nil {
			return err
		}
		if err := o.manifests.Save(txCtx, request.ContextManifest); err != nil {
			return err
		}
		if err := o.grants.Save(txCtx, request.Grant.Snapshot()); err != nil {
			return err
		}
		if err := o.sessions.Save(txCtx, session); err != nil {
			return err
		}
		if err := o.groups.Save(txCtx, group); err != nil {
			return err
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result = CreateSessionResult{
			Session: SessionSnapshot{ID: session.ID, Goal: session.Goal, WorkspaceKey: session.WorkspaceKey},
			Group:   group,
			Agent:   agent,
		}
		return nil
	})
	if err != nil {
		return CreateSessionResult{}, err
	}
	return result, nil
}

// CreateProject creates a workspace-backed Session with its first Primary Agent.
// The initial grant is intentionally read-only until the user approves broader access.
func (o *AgentOrchestrator) CreateProject(
	ctx context.Context,
	request CreateProjectRequest,
) (CreateSessionResult, error) {
	if ctx == nil {
		return CreateSessionResult{}, errors.New("create project context is required")
	}
	if !o.Ready() {
		return CreateSessionResult{}, commandError(CommandErrorNotReady)
	}
	workspaceKey := filepath.Clean(strings.TrimSpace(request.WorkspaceKey))
	if workspaceKey == "." || !filepath.IsAbs(workspaceKey) || strings.ContainsAny(workspaceKey, "\x00\r\n") {
		return CreateSessionResult{}, commandError(CommandErrorProjectWorkspaceInvalid)
	}
	return o.createPrimarySession(ctx, workspaceKey, request.Goal)
}

// CreateSessionForWorkspace creates another Primary Agent session for an
// existing project workspace selected from the project catalog.
func (o *AgentOrchestrator) CreateSessionForWorkspace(
	ctx context.Context,
	workspaceKey string,
	goal string,
) (CreateSessionResult, error) {
	if ctx == nil {
		return CreateSessionResult{}, errors.New("create session context is required")
	}
	if !o.Ready() {
		return CreateSessionResult{}, commandError(CommandErrorNotReady)
	}
	workspaceKey = filepath.Clean(strings.TrimSpace(workspaceKey))
	if workspaceKey == "." || !filepath.IsAbs(workspaceKey) || strings.ContainsAny(workspaceKey, "\x00\r\n") {
		return CreateSessionResult{}, commandError(CommandErrorProjectWorkspaceInvalid)
	}
	return o.createPrimarySession(ctx, workspaceKey, goal)
}

func (o *AgentOrchestrator) createPrimarySession(
	ctx context.Context,
	workspaceKey string,
	goal string,
) (CreateSessionResult, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		goal = "Work on " + filepath.Base(workspaceKey)
	}
	at := o.clock.Now().UTC()
	packet, err := domain.NewTaskPacket(domain.NewTaskPacketID(), goal, nil, nil)
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("create project task packet: %w", err)
	}
	manifest, err := domain.NewContextManifest(domain.NewContextManifestID(), goal, nil, at)
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("create project context manifest: %w", err)
	}
	model, err := o.models.ResolveModel(domain.ProfilePrimary)
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("resolve primary model: %w", err)
	}
	grant, err := domain.NewCapabilityGrant(domain.CapabilityGrantSpec{
		ID:           domain.NewCapabilityGrantID(),
		WorkspaceKey: workspaceKey,
		AllowedTools: []domain.ToolName{domain.ToolReadFile, domain.ToolListDir, domain.ToolSearchText,
			domain.ToolProposeDelegate, domain.ToolSubmitResult, domain.ToolSubmitBriefing},
		ReadScopes:           []string{workspaceKey},
		CanProposeDelegation: true,
		ResultPermissions: []domain.ResultPermission{
			domain.ResultPermissionAgentResult,
			domain.ResultPermissionBriefing,
		},
		Model:              model,
		ContextManifestRef: manifest.ID,
		ApprovalSource:     domain.ApprovalSourceUser,
	})
	if err != nil {
		return CreateSessionResult{}, fmt.Errorf("create project capability grant: %w", err)
	}
	return o.CreateSession(ctx, CreateSessionRequest{
		Goal:            goal,
		WorkspaceKey:    workspaceKey,
		MaxConcurrent:   1,
		Profile:         domain.ProfilePrimary,
		TaskPacket:      packet,
		ContextManifest: manifest,
		Grant:           grant,
	})
}

type SendInputRequest struct {
	AgentID         domain.AgentID
	RequestID       domain.RequestID
	Content         string
	ModelID         string
	Reasoning       string
	RuntimeSnapshot domain.RuntimeExecutionSnapshot
}

type SendInputResult struct {
	Execution       domain.AgentExecution
	ExistingRequest bool
	ActivationError string
}

// SendInput either returns the prior execution for RequestID or atomically
// creates one starting execution. Active agents never retain ordinary input.
func (o *AgentOrchestrator) SendInput(ctx context.Context, request SendInputRequest) (SendInputResult, error) {
	if ctx == nil {
		return SendInputResult{}, errors.New("send input context is required")
	}
	if !o.Ready() {
		return SendInputResult{}, commandError(CommandErrorNotReady)
	}
	if strings.TrimSpace(request.AgentID.String()) == "" ||
		strings.TrimSpace(request.RequestID.String()) == "" || strings.TrimSpace(request.Content) == "" {
		return SendInputResult{}, commandError(CommandErrorInvalidRequest)
	}
	if err := request.RuntimeSnapshot.Validate(); err != nil {
		return SendInputResult{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}

	var result SendInputResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := o.executions.FindByRequest(txCtx, request.AgentID, request.RequestID)
		if err == nil {
			if !executionRequestMatches(
				existing,
				domain.ExecutionUserInput,
				request.Content,
				request.RuntimeSnapshot,
			) {
				return domain.ErrRequestConflict
			}
			matches, matchErr := o.executionModelMatches(txCtx, existing, request.ModelID, request.Reasoning)
			if matchErr != nil {
				return matchErr
			}
			if !matches {
				return domain.ErrRequestConflict
			}
			result = SendInputResult{Execution: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		agent, err := o.agents.Get(txCtx, request.AgentID)
		if err != nil {
			return err
		}
		switch agent.State {
		case domain.AgentExecuting, domain.AgentPausing:
			return commandError(CommandErrorAgentExecuting)
		case domain.AgentIdle, domain.AgentWaiting, domain.AgentFailed, domain.AgentClosed:
		default:
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := o.rejectDeliveringInput(txCtx, agent.ID); err != nil {
			return err
		}
		if err := o.checkGroupCapacity(txCtx, agent.GroupID); err != nil {
			return err
		}
		grantID, err := o.selectedGrant(txCtx, agent, request.ModelID, request.Reasoning)
		if err != nil {
			return err
		}
		at := o.clock.Now()
		execution, err := domain.NewAgentExecution(
			domain.NewAgentExecutionID(),
			agent.SessionID,
			agent.ID,
			request.RequestID,
			domain.ExecutionUserInput,
			request.Content,
			domain.ExecutionInputSnapshot{
				TaskPacketID:      agent.TaskPacketID,
				ContextManifestID: agent.ContextManifestID,
				CapabilityGrantID: grantID,
				Runtime:           request.RuntimeSnapshot,
			},
			at,
		)
		if err != nil {
			return err
		}
		if err := agent.Start(execution.ID, at); err != nil {
			return err
		}
		if err := o.executions.Save(txCtx, execution); err != nil {
			return err
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Execution = execution
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrRequestConflict) {
			existing, lookupErr := o.executions.FindByRequest(ctx, request.AgentID, request.RequestID)
			if lookupErr != nil {
				return SendInputResult{}, lookupErr
			}
			if !executionRequestMatches(
				existing,
				domain.ExecutionUserInput,
				request.Content,
				request.RuntimeSnapshot,
			) {
				return SendInputResult{}, err
			}
			matches, matchErr := o.executionModelMatches(
				ctx,
				existing,
				request.ModelID,
				request.Reasoning,
			)
			if matchErr != nil {
				return SendInputResult{}, matchErr
			}
			if !matches {
				return SendInputResult{}, err
			}
			return SendInputResult{Execution: existing, ExistingRequest: true}, nil
		}
		return SendInputResult{}, err
	}
	if result.ExistingRequest || o.activator == nil {
		return result, nil
	}
	if err := o.activator.TryActivate(context.WithoutCancel(ctx), request.AgentID, o); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

func (o *AgentOrchestrator) selectedGrant(
	ctx context.Context,
	agent domain.Agent,
	modelID string,
	reasoning string,
) (domain.CapabilityGrantID, error) {
	base, err := o.grants.Get(ctx, agent.GrantID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(modelID) == "" && strings.TrimSpace(reasoning) == "" {
		return base.ID, nil
	}
	selector, ok := o.models.(ModelSelectionResolver)
	if !ok {
		return "", commandError(CommandErrorInvalidRequest)
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		modelID = base.Model.ID
	}
	model, err := selector.ResolveModelSelection(modelID, strings.TrimSpace(reasoning))
	if err != nil {
		return "", commandError(CommandErrorInvalidRequest)
	}
	if model == base.Model {
		return base.ID, nil
	}
	grant := base.Snapshot()
	grant.ID = domain.NewCapabilityGrantID()
	grant.Model = model
	if err := grant.Validate(); err != nil {
		return "", err
	}
	if err := o.grants.Save(ctx, grant); err != nil {
		return "", err
	}
	return grant.ID, nil
}

func (o *AgentOrchestrator) executionModelMatches(
	ctx context.Context,
	execution domain.AgentExecution,
	modelID string,
	reasoning string,
) (bool, error) {
	modelID = strings.TrimSpace(modelID)
	reasoning = strings.TrimSpace(reasoning)
	if modelID == "" && reasoning == "" {
		return true, nil
	}
	grant, err := o.grants.Get(ctx, execution.Input.CapabilityGrantID)
	if err != nil {
		return false, err
	}
	if modelID != "" && grant.Model.ID != modelID {
		return false, nil
	}
	if reasoning != "" && grant.Model.Reasoning != reasoning {
		return false, nil
	}
	return true, nil
}

type ResumeRequest struct {
	AgentID         domain.AgentID
	RequestID       domain.RequestID
	Content         string
	RuntimeSnapshot domain.RuntimeExecutionSnapshot
}

// QueueWorkRequest contains one independent task. The caller retains ID for
// retries; it is neither a user-input RequestID nor a transcript message.
type QueueWorkRequest struct {
	ID              domain.WorkItemID
	AgentID         domain.AgentID
	Prompt          string
	RuntimeSnapshot domain.RuntimeExecutionSnapshot
}

type QueueWorkResult struct {
	Work            domain.QueuedWork
	ExistingWork    bool
	ActivationError string
}

// EnqueueWork durably stores an independent task even while its Agent has an
// active execution. It only starts a task when the Agent is independently
// eligible, so it cannot bypass SendInput admission or alter a transcript.
func (o *AgentOrchestrator) EnqueueWork(ctx context.Context, request QueueWorkRequest) (QueueWorkResult, error) {
	if ctx == nil {
		return QueueWorkResult{}, errors.New("enqueue work context is required")
	}
	if !o.Ready() {
		return QueueWorkResult{}, commandError(CommandErrorNotReady)
	}
	if strings.TrimSpace(request.ID.String()) == "" || strings.TrimSpace(request.AgentID.String()) == "" ||
		strings.TrimSpace(request.Prompt) == "" {
		return QueueWorkResult{}, commandError(CommandErrorInvalidRequest)
	}
	if err := request.RuntimeSnapshot.Validate(); err != nil {
		return QueueWorkResult{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	result := QueueWorkResult{}
	startEligible := false
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := o.queuedWork.Get(txCtx, request.ID)
		if err == nil {
			if existing.AgentID != request.AgentID || existing.Prompt != strings.TrimSpace(request.Prompt) ||
				existing.Input.Runtime != request.RuntimeSnapshot {
				return commandError(CommandErrorInvalidRequest)
			}
			result.Work = existing
			result.ExistingWork = true
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		agent, err := o.agents.Get(txCtx, request.AgentID)
		if err != nil {
			return err
		}
		sequence, err := o.queuedWork.NextSequence(txCtx, agent.ID)
		if err != nil {
			return err
		}
		work, err := domain.NewQueuedWork(
			request.ID,
			agent.SessionID,
			agent.ID,
			sequence,
			request.Prompt,
			domain.ExecutionInputSnapshot{
				TaskPacketID:      agent.TaskPacketID,
				ContextManifestID: agent.ContextManifestID,
				CapabilityGrantID: agent.GrantID,
				Runtime:           request.RuntimeSnapshot,
			},
			o.clock.Now(),
		)
		if err != nil {
			return err
		}
		if err := o.queuedWork.Save(txCtx, work); err != nil {
			return err
		}
		result.Work = work
		startEligible = agent.State == domain.AgentIdle || agent.State == domain.AgentWaiting ||
			agent.State == domain.AgentFailed || agent.State == domain.AgentClosed
		return nil
	})
	if err != nil || result.ExistingWork || !startEligible {
		return result, err
	}
	started, err := o.StartNextQueuedWork(context.WithoutCancel(ctx), request.AgentID)
	if err != nil {
		result.ActivationError = err.Error()
	} else if started.ActivationError != "" {
		result.ActivationError = started.ActivationError
	}
	return result, nil
}

type QueuedWorkStartResult struct {
	Work            domain.QueuedWork
	Execution       domain.AgentExecution
	Started         bool
	ActivationError string
}

// StartNextQueuedWork applies the default FIFO policy. It is safe for both a
// post-command wakeup and recovery because the queue and execution transition
// share one transaction and scheduler activation is only an optimization.
func (o *AgentOrchestrator) StartNextQueuedWork(
	ctx context.Context,
	agentID domain.AgentID,
) (QueuedWorkStartResult, error) {
	if ctx == nil {
		return QueuedWorkStartResult{}, errors.New("start queued work context is required")
	}
	if strings.TrimSpace(agentID.String()) == "" {
		return QueuedWorkStartResult{}, commandError(CommandErrorInvalidRequest)
	}
	result := QueuedWorkStartResult{}
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		agent, err := o.agents.Get(txCtx, agentID)
		if err != nil {
			return err
		}
		if agent.State != domain.AgentIdle && agent.State != domain.AgentWaiting && agent.State != domain.AgentFailed &&
			agent.State != domain.AgentClosed {
			return nil
		}
		if err := o.rejectDeliveringInput(txCtx, agent.ID); err != nil {
			if hasCommandErrorCode(err, CommandErrorAgentUnavailable) {
				return nil
			}
			return err
		}
		if err := o.checkGroupCapacity(txCtx, agent.GroupID); err != nil {
			if hasCommandErrorCode(err, CommandErrorAgentUnavailable) {
				return nil
			}
			return err
		}
		work, err := o.queuedWork.FindNextPendingByAgent(txCtx, agent.ID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		at := o.clock.Now()
		execution, err := domain.NewQueuedWorkExecution(
			domain.NewAgentExecutionID(),
			agent.SessionID,
			agent.ID,
			work.ID,
			work.Input,
			at,
		)
		if err != nil {
			return err
		}
		if err := work.Start(execution.ID, at); err != nil {
			return err
		}
		if err := agent.Start(execution.ID, at); err != nil {
			return err
		}
		if err := o.executions.Save(txCtx, execution); err != nil {
			return err
		}
		if err := o.queuedWork.Save(txCtx, work); err != nil {
			return err
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Work = work
		result.Execution = execution
		result.Started = true
		return nil
	})
	if err != nil || !result.Started || o.activator == nil {
		return result, err
	}
	if err := o.activator.TryActivate(context.WithoutCancel(ctx), agentID, o); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

// Resume creates a fresh execution and deliberately does not reuse runtime
// channels, model streams, tool state, or the previous execution identity.
func (o *AgentOrchestrator) Resume(ctx context.Context, request ResumeRequest) (SendInputResult, error) {
	if ctx == nil {
		return SendInputResult{}, errors.New("resume context is required")
	}
	if !o.Ready() {
		return SendInputResult{}, commandError(CommandErrorNotReady)
	}
	if strings.TrimSpace(request.AgentID.String()) == "" || strings.TrimSpace(request.RequestID.String()) == "" {
		return SendInputResult{}, commandError(CommandErrorInvalidRequest)
	}
	if err := request.RuntimeSnapshot.Validate(); err != nil {
		return SendInputResult{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	var result SendInputResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := o.executions.FindByRequest(txCtx, request.AgentID, request.RequestID)
		if err == nil {
			if !executionRequestMatches(
				existing,
				domain.ExecutionResume,
				request.Content,
				request.RuntimeSnapshot,
			) {
				return domain.ErrRequestConflict
			}
			result = SendInputResult{Execution: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		agent, err := o.agents.Get(txCtx, request.AgentID)
		if err != nil {
			return err
		}
		if agent.State != domain.AgentPaused && agent.State != domain.AgentInterrupted {
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := o.rejectDeliveringInput(txCtx, agent.ID); err != nil {
			return err
		}
		if err := o.checkGroupCapacity(txCtx, agent.GroupID); err != nil {
			return err
		}
		at := o.clock.Now()
		execution, err := domain.NewAgentExecution(
			domain.NewAgentExecutionID(),
			agent.SessionID,
			agent.ID,
			request.RequestID,
			domain.ExecutionResume,
			request.Content,
			domain.ExecutionInputSnapshot{
				TaskPacketID:      agent.TaskPacketID,
				ContextManifestID: agent.ContextManifestID,
				CapabilityGrantID: agent.GrantID,
				Runtime:           request.RuntimeSnapshot,
			},
			at,
		)
		if err != nil {
			return err
		}
		if err := agent.Resume(execution.ID, at); err != nil {
			return err
		}
		if err := o.executions.Save(txCtx, execution); err != nil {
			return err
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Execution = execution
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrRequestConflict) {
			existing, lookupErr := o.executions.FindByRequest(ctx, request.AgentID, request.RequestID)
			if lookupErr != nil {
				return SendInputResult{}, lookupErr
			}
			if !executionRequestMatches(
				existing,
				domain.ExecutionResume,
				request.Content,
				request.RuntimeSnapshot,
			) {
				return SendInputResult{}, err
			}
			return SendInputResult{Execution: existing, ExistingRequest: true}, nil
		}
		return SendInputResult{}, err
	}
	if result.ExistingRequest || o.activator == nil {
		return result, nil
	}
	if err := o.activator.TryActivate(context.WithoutCancel(ctx), request.AgentID, o); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

type ControlRequest struct {
	ID      domain.AgentControlRequestID
	AgentID domain.AgentID
	Kind    domain.AgentControlKind
}

type ControlResult struct {
	Request           domain.AgentControlRequest
	ExistingRequest   bool
	CancellationError string
}

// RequestControl durably records Pause or Close before it signals runtime
// cancellation. A caller timeout cannot retract the committed control request.
func (o *AgentOrchestrator) RequestControl(ctx context.Context, request ControlRequest) (ControlResult, error) {
	if ctx == nil {
		return ControlResult{}, errors.New("control request context is required")
	}
	if !o.Ready() {
		return ControlResult{}, commandError(CommandErrorNotReady)
	}
	if strings.TrimSpace(request.ID.String()) == "" || strings.TrimSpace(request.AgentID.String()) == "" ||
		(request.Kind != domain.ControlPause && request.Kind != domain.ControlClose) {
		return ControlResult{}, commandError(CommandErrorInvalidRequest)
	}
	var result ControlResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := o.controls.Get(txCtx, request.ID)
		if err == nil {
			if existing.AgentID != request.AgentID || existing.Kind != request.Kind {
				return commandError(CommandErrorInvalidRequest)
			}
			result = ControlResult{Request: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		agent, err := o.agents.Get(txCtx, request.AgentID)
		if err != nil {
			return err
		}
		at := o.clock.Now()
		control, err := domain.NewAgentControlRequest(request.ID, agent.ID, agent.CurrentExecutionID, request.Kind, at)
		if err != nil {
			return err
		}
		if agent.State == domain.AgentExecuting {
			if err := agent.RequestPause(at); err != nil {
				return err
			}
		} else if agent.State == domain.AgentPausing {
			return commandError(CommandErrorAgentUnavailable)
		} else if request.Kind == domain.ControlClose {
			if err := agent.Close(at); err != nil {
				return err
			}
			if err := control.MarkApplied(at); err != nil {
				return err
			}
		} else {
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := o.controls.Save(txCtx, control); err != nil {
			return err
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Request = control
		return nil
	})
	if err != nil {
		return ControlResult{}, err
	}
	if result.ExistingRequest || result.Request.Status == domain.ControlApplied || o.canceller == nil {
		return result, nil
	}
	if err := o.canceller.Cancel(
		context.WithoutCancel(ctx),
		result.Request.AgentID,
		result.Request.TargetExecutionID,
		controlCancellationOutcome(result.Request.Kind),
	); err != nil {
		result.CancellationError = err.Error()
	}
	return result, nil
}

// MarkExecutionRunning records the product transition after runtime has
// reconciled its run-start receipt. It is idempotent for repeated activation.
func (o *AgentOrchestrator) MarkExecutionRunning(
	ctx context.Context,
	executionID domain.AgentExecutionID,
) error {
	if ctx == nil {
		return errors.New("mark running context is required")
	}
	return o.tx.InTx(ctx, func(txCtx context.Context) error {
		execution, err := o.executions.Get(txCtx, executionID)
		if err != nil {
			return err
		}
		if execution.Status == domain.ExecutionRunning {
			return nil
		}
		if execution.Status != domain.ExecutionStarting {
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := execution.MarkRunning(o.clock.Now()); err != nil {
			return err
		}
		return o.executions.Save(txCtx, execution)
	})
}

// ConfirmExecutionStart accepts a JSONL receipt after the runtime has fsynced
// both run_started and the source-request message. Only then may SQLite drop
// its temporary StartContent copy.
func (o *AgentOrchestrator) ConfirmExecutionStart(
	ctx context.Context,
	receipt coresession.ExecutionStartReceipt,
) error {
	if ctx == nil {
		return errors.New("execution start receipt context is required")
	}
	if strings.TrimSpace(receipt.ExecutionID.String()) == "" || strings.TrimSpace(receipt.EntryID) == "" {
		return commandError(CommandErrorInvalidRequest)
	}
	return o.tx.InTx(ctx, func(txCtx context.Context) error {
		execution, err := o.executions.Get(txCtx, receipt.ExecutionID)
		if err != nil {
			return err
		}
		if execution.RequestID != receipt.RequestID {
			return commandError(CommandErrorInvalidRequest)
		}
		if execution.Status == domain.ExecutionStarting {
			if err := execution.MarkRunning(o.clock.Now()); err != nil {
				return err
			}
		}
		if execution.Status != domain.ExecutionRunning && execution.Status != domain.ExecutionSettling {
			return commandError(CommandErrorAgentUnavailable)
		}
		if execution.StartContent != "" {
			if err := execution.ClearStartContent(receipt.InputDigest); err != nil {
				return err
			}
		}
		return o.executions.Save(txCtx, execution)
	})
}

type ContextDeliveryClaim struct {
	Delivery domain.ContextDelivery
	Claimed  bool
}

// ClaimContextDelivery blocks a concurrent input start while the JSONL
// artifact is being appended. It performs no transcript I/O itself.
func (o *AgentOrchestrator) ClaimContextDelivery(
	ctx context.Context,
	deliveryID domain.DeliveryID,
) (ContextDeliveryClaim, error) {
	if ctx == nil {
		return ContextDeliveryClaim{}, errors.New("context delivery claim context is required")
	}
	if strings.TrimSpace(deliveryID.String()) == "" {
		return ContextDeliveryClaim{}, commandError(CommandErrorInvalidRequest)
	}
	claim := ContextDeliveryClaim{}
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		delivery, err := o.deliveries.Get(txCtx, deliveryID)
		if err != nil {
			return err
		}
		claim.Delivery = delivery
		if delivery.Status == domain.ContextDeliveryDelivering {
			return nil
		}
		if delivery.Status != domain.ContextDeliveryPending {
			return nil
		}
		agent, err := o.agents.Get(txCtx, delivery.TargetAgentID)
		if err != nil {
			return err
		}
		if agent.State != domain.AgentIdle && agent.State != domain.AgentWaiting && agent.State != domain.AgentFailed &&
			agent.State != domain.AgentClosed {
			return nil
		}
		if err := delivery.Begin(o.clock.Now()); err != nil {
			return err
		}
		if err := o.deliveries.Save(txCtx, delivery); err != nil {
			return err
		}
		claim.Delivery = delivery
		claim.Claimed = true
		return nil
	})
	if err != nil {
		return ContextDeliveryClaim{}, err
	}
	return claim, nil
}

type ContextDeliveryCompletionRequest struct {
	DeliveryID       domain.DeliveryID
	ArtifactEntryRef string
	RuntimeSnapshot  domain.RuntimeExecutionSnapshot
}

type ContextDeliveryCompletion struct {
	Delivery         domain.ContextDelivery
	Execution        domain.AgentExecution
	ExistingDelivery bool
	ActivationError  string
}

// CompleteContextDelivery runs only after the target Agent JSONL has fsynced
// the artifact receipt. Delivery, eligible waits, Agent state, and the next
// context_delivery execution become one SQLite transaction.
func (o *AgentOrchestrator) CompleteContextDelivery(
	ctx context.Context,
	request ContextDeliveryCompletionRequest,
) (ContextDeliveryCompletion, error) {
	if ctx == nil {
		return ContextDeliveryCompletion{}, errors.New("context delivery completion context is required")
	}
	if strings.TrimSpace(request.DeliveryID.String()) == "" || strings.TrimSpace(request.ArtifactEntryRef) == "" {
		return ContextDeliveryCompletion{}, commandError(CommandErrorInvalidRequest)
	}
	if err := request.RuntimeSnapshot.Validate(); err != nil {
		return ContextDeliveryCompletion{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	result := ContextDeliveryCompletion{}
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		delivery, err := o.deliveries.Get(txCtx, request.DeliveryID)
		if err != nil {
			return err
		}
		if delivery.Status == domain.ContextDeliveryDelivered {
			if delivery.ArtifactEntryRef != request.ArtifactEntryRef {
				return domain.ErrRequestConflict
			}
			result.Delivery = delivery
			result.ExistingDelivery = true
			return nil
		}
		if delivery.Status != domain.ContextDeliveryDelivering {
			return commandError(CommandErrorAgentUnavailable)
		}
		agent, err := o.agents.Get(txCtx, delivery.TargetAgentID)
		if err != nil {
			return err
		}
		if agent.State != domain.AgentIdle && agent.State != domain.AgentWaiting && agent.State != domain.AgentFailed &&
			agent.State != domain.AgentClosed {
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := o.checkGroupCapacity(txCtx, agent.GroupID); err != nil {
			return err
		}
		at := o.clock.Now()
		if err := delivery.MarkDelivered(request.ArtifactEntryRef, at); err != nil {
			return err
		}
		waits, err := o.waits.ListUnresolvedByAgent(txCtx, agent.ID, 100)
		if err != nil {
			return err
		}
		for index := range waits {
			wait := &waits[index]
			if !waitTargets(wait, delivery.ID.String()) {
				continue
			}
			before := len(wait.ResolvedTargetIDs)
			if _, err := wait.ResolveTarget(delivery.ID.String(), at); err != nil {
				return err
			}
			if len(wait.ResolvedTargetIDs) != before {
				if err := o.waits.Save(txCtx, *wait); err != nil {
					return err
				}
			}
		}
		requestID := domain.RequestID("delivery:" + delivery.ID.String())
		execution, err := domain.NewAgentExecution(
			domain.NewAgentExecutionID(),
			agent.SessionID,
			agent.ID,
			requestID,
			domain.ExecutionContextDelivery,
			"",
			domain.ExecutionInputSnapshot{
				TaskPacketID:      agent.TaskPacketID,
				ContextManifestID: agent.ContextManifestID,
				CapabilityGrantID: agent.GrantID,
				Runtime:           request.RuntimeSnapshot,
			},
			at,
		)
		if err != nil {
			return err
		}
		if err := agent.Start(execution.ID, at); err != nil {
			return err
		}
		if err := o.deliveries.Save(txCtx, delivery); err != nil {
			return err
		}
		if err := o.executions.Save(txCtx, execution); err != nil {
			return err
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Delivery = delivery
		result.Execution = execution
		return nil
	})
	if err != nil {
		return ContextDeliveryCompletion{}, err
	}
	if result.ExistingDelivery || o.activator == nil {
		return result, nil
	}
	if err := o.activator.TryActivate(context.WithoutCancel(ctx), result.Execution.AgentID, o); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

type ExecutionSettlement struct {
	ExecutionID domain.AgentExecutionID
	Outcome     domain.ExecutionOutcome
	FailureCode domain.ExecutionFailureCode
}

// SettleExecution is called only after the runtime has fsynced its transcript
// settlement receipt. It applies Agent state and control request completion in
// the same SQLite transaction.
func (o *AgentOrchestrator) SettleExecution(ctx context.Context, settlement ExecutionSettlement) error {
	if ctx == nil {
		return errors.New("settlement context is required")
	}
	if strings.TrimSpace(settlement.ExecutionID.String()) == "" || !isKnownExecutionOutcome(settlement.Outcome) {
		return commandError(CommandErrorInvalidRequest)
	}
	var settledAgentID domain.AgentID
	var advanceQueue bool
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		execution, err := o.executions.Get(txCtx, settlement.ExecutionID)
		if err != nil {
			return err
		}
		if execution.Status == domain.ExecutionSettled {
			return nil
		}
		at := o.clock.Now()
		if execution.Status == domain.ExecutionStarting {
			if err := execution.MarkRunning(at); err != nil {
				return err
			}
		}
		if execution.Status == domain.ExecutionRunning {
			if err := execution.BeginSettlement(at); err != nil {
				return err
			}
		}
		if err := execution.Settle(settlement.Outcome, settlement.FailureCode, at); err != nil {
			return err
		}
		agent, err := o.agents.Get(txCtx, execution.AgentID)
		if err != nil {
			return err
		}
		if agent.CurrentExecutionID != execution.ID {
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := agent.Settle(settlement.Outcome, at); err != nil {
			return err
		}
		if execution.WorkItemID != "" {
			work, err := o.queuedWork.Get(txCtx, execution.WorkItemID)
			if err != nil {
				return err
			}
			if work.AgentID != agent.ID || work.ExecutionID != execution.ID {
				return commandError(CommandErrorAgentUnavailable)
			}
			if err := work.Settle(execution.ID, settlement.Outcome, settlement.FailureCode, at); err != nil {
				return err
			}
			if err := o.queuedWork.Save(txCtx, work); err != nil {
				return err
			}
			advanceQueue = settlement.Outcome == domain.ExecutionCompleted ||
				settlement.Outcome == domain.ExecutionYielded
		}
		controls, err := o.controls.ListOpenByAgent(txCtx, agent.ID, 100)
		if err != nil {
			return err
		}
		for index := range controls {
			control := &controls[index]
			if control.TargetExecutionID != execution.ID {
				continue
			}
			if control.Kind == domain.ControlClose {
				if err := agent.Close(at); err != nil {
					return err
				}
			}
			if err := control.MarkApplied(at); err != nil {
				return err
			}
			if err := o.controls.Save(txCtx, *control); err != nil {
				return err
			}
		}
		if err := o.executions.Save(txCtx, execution); err != nil {
			return err
		}
		settledAgentID = agent.ID
		return o.agents.Save(txCtx, agent)
	})
	if err != nil || !advanceQueue {
		return err
	}
	_, _ = o.StartNextQueuedWork(context.WithoutCancel(ctx), settledAgentID)
	return nil
}

// SettleRuntimeExecution is the narrow runtime callback after its independent
// JSONL settlement context has durably recorded the final receipt.
func (o *AgentOrchestrator) SettleRuntimeExecution(
	ctx context.Context,
	executionID domain.AgentExecutionID,
	outcome domain.ExecutionOutcome,
	failureCode domain.ExecutionFailureCode,
) error {
	return o.SettleExecution(ctx, ExecutionSettlement{
		ExecutionID: executionID,
		Outcome:     outcome,
		FailureCode: failureCode,
	})
}

// ApplyControlRequest finishes a requested control whose target execution is
// already durable-settled. Recovery uses this path for controls that were
// committed before a runtime callback or process notification was lost.
func (o *AgentOrchestrator) ApplyControlRequest(
	ctx context.Context,
	requestID domain.AgentControlRequestID,
) error {
	if ctx == nil {
		return errors.New("apply control request context is required")
	}
	if strings.TrimSpace(requestID.String()) == "" {
		return commandError(CommandErrorInvalidRequest)
	}
	return o.tx.InTx(ctx, func(txCtx context.Context) error {
		control, err := o.controls.Get(txCtx, requestID)
		if err != nil {
			return err
		}
		if control.Status == domain.ControlApplied {
			return nil
		}
		agent, err := o.agents.Get(txCtx, control.AgentID)
		if err != nil {
			return err
		}
		if control.TargetExecutionID != "" {
			execution, err := o.executions.Get(txCtx, control.TargetExecutionID)
			if err != nil {
				return err
			}
			if execution.Active() {
				return commandError(CommandErrorAgentUnavailable)
			}
		}
		at := o.clock.Now()
		if control.Kind == domain.ControlClose {
			if agent.State == domain.AgentExecuting || agent.State == domain.AgentPausing {
				return commandError(CommandErrorAgentUnavailable)
			}
			if err := agent.Close(at); err != nil {
				return err
			}
		}
		if err := control.MarkApplied(at); err != nil {
			return err
		}
		if err := o.controls.Save(txCtx, control); err != nil {
			return err
		}
		return o.agents.Save(txCtx, agent)
	})
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

func isKnownExecutionOutcome(outcome domain.ExecutionOutcome) bool {
	switch outcome {
	case domain.ExecutionCompleted, domain.ExecutionYielded, domain.ExecutionPaused,
		domain.ExecutionFailed, domain.ExecutionInterrupted:
		return true
	default:
		return false
	}
}

func waitTargets(wait *domain.WaitCondition, targetID string) bool {
	for _, candidate := range wait.TargetIDs {
		if candidate == targetID {
			return true
		}
	}
	return false
}

func controlCancellationOutcome(kind domain.AgentControlKind) domain.ExecutionOutcome {
	if kind == domain.ControlPause {
		return domain.ExecutionPaused
	}
	return domain.ExecutionInterrupted
}

func executionRequestMatches(
	execution domain.AgentExecution,
	reason domain.ExecutionReason,
	content string,
	runtime domain.RuntimeExecutionSnapshot,
) bool {
	if execution.Reason != reason || execution.Input.Runtime != runtime {
		return false
	}
	if reason != domain.ExecutionUserInput && reason != domain.ExecutionResume {
		return true
	}
	if content == "" && execution.StartContent == "" && execution.StartContentDigest == "" {
		return true
	}
	if execution.StartContent != "" {
		return execution.StartContent == content
	}
	if execution.StartContentDigest == "" {
		return false
	}
	digest := sha256.Sum256([]byte(content))
	return execution.StartContentDigest == hex.EncodeToString(digest[:])
}
