package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"praxis/internal/agentruntime"
	"praxis/internal/contracts"
	"praxis/internal/core/domain"
	"praxis/internal/core/orchestrate"
	coresession "praxis/internal/core/session"
	"praxis/internal/logging"
	"praxis/internal/providers/registry"
	"praxis/internal/storage"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const agentOutputEventName = "praxis:agent-output"

// App owns the Wails lifecycle context and is the minimal desktop binding entry point.
type App struct {
	mu                sync.RWMutex
	ctx               context.Context
	service           CoreService
	projectRoot       string
	logger            *logging.Logger
	startupIssue      *contracts.StartupIssue
	unsubscribeOutput func()
}

// CoreService is the narrow application-facing surface. The concrete
// orchestrator remains the owner of durable state and command admission.
type CoreService interface {
	Ready() bool
	ProjectSession(context.Context, domain.SessionID, int) (orchestrate.SessionProjection, error)
	ProjectAgent(context.Context, domain.AgentID, int) (orchestrate.AgentProjection, error)
	ListAgentMessages(context.Context, domain.AgentID, int) ([]coresession.AgentSessionMessage, error)
	SendInput(context.Context, orchestrate.SendInputRequest) (orchestrate.SendInputResult, error)
	Resume(context.Context, orchestrate.ResumeRequest) (orchestrate.SendInputResult, error)
	RequestControl(context.Context, orchestrate.ControlRequest) (orchestrate.ControlResult, error)
	EnqueueWork(context.Context, orchestrate.QueueWorkRequest) (orchestrate.QueueWorkResult, error)
}

type sessionCatalogService interface {
	ListSessions(context.Context, int) ([]domain.Session, error)
}

type projectCreatorService interface {
	CreateProject(context.Context, orchestrate.CreateProjectRequest) (orchestrate.CreateSessionResult, error)
}

type sessionCreatorService interface {
	CreateSessionForWorkspace(context.Context, string, string) (orchestrate.CreateSessionResult, error)
}

type modelCatalogService interface {
	ListModels() []registry.ModelOption
}

type runtimeLoggerProvider interface {
	RuntimeLogger() *logging.Logger
}

type agentOutputSubscriber interface {
	SubscribeAgentOutput(agentruntime.AgentOutputObserver) func()
}

func New(loggers ...*logging.Logger) *App {
	factory := logging.NewFactory()
	logger := factory.Nop()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = factory.Ensure(loggers[0])
	}
	return &App{logger: logger}
}

// SetDataRoot keeps project creation on the same resolved root as storage and
// configuration. Wails bindings never choose an independent filesystem root.
func (a *App) SetDataRoot(root storage.DataRoot) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.projectRoot = root.Projects
	a.logger.Infof("data root resolved root=%s", root.Root)
}

// SetCoreService injects the composition-root orchestrator without allowing
// Wails or frontend code to access its repositories directly.
func (a *App) SetCoreService(service CoreService) {
	var unsubscribe func()
	if subscriber, ok := service.(agentOutputSubscriber); ok {
		unsubscribe = subscriber.SubscribeAgentOutput(a.emitAgentOutput)
	}
	a.mu.Lock()
	previousUnsubscribe := a.unsubscribeOutput
	a.service = service
	a.startupIssue = nil
	a.unsubscribeOutput = unsubscribe
	factory := logging.NewFactory()
	a.logger = factory.Nop()
	if provider, ok := service.(runtimeLoggerProvider); ok {
		a.logger = factory.Ensure(provider.RuntimeLogger())
	}
	logger := a.logger
	a.mu.Unlock()
	if previousUnsubscribe != nil {
		previousUnsubscribe()
	}
	logger.Infof("core service injected ready=%t", service != nil && service.Ready())
}

// SetStartupError preserves a safe diagnostic for Readiness when composition
// fails before a CoreService can be exposed to Wails.
func (a *App) SetStartupError(err error) {
	issue := startupIssue(err)
	a.mu.Lock()
	a.startupIssue = issue
	logger := a.logger
	a.mu.Unlock()
	logger.Errorf("startup failed code=%s: %v", issue.Code, err)
}

// Startup stores the Wails runtime context for binding calls.
func (a *App) Startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	logger := a.logger
	a.mu.Unlock()
	logger.Infof("Wails startup callback completed")
}

// Shutdown closes the composition root before releasing the binding context.
// The shutdown budget is detached from Wails' callback context so a canceled
// UI context cannot leave SQLite or a runtime actor open.
func (a *App) Shutdown(ctx context.Context) {
	a.mu.Lock()
	service := a.service
	logger := a.logger
	unsubscribe := a.unsubscribeOutput
	a.service = nil
	a.ctx = nil
	a.unsubscribeOutput = nil
	a.mu.Unlock()
	if unsubscribe != nil {
		unsubscribe()
	}
	logger.Infof("Wails shutdown callback started")
	closer, ok := service.(interface{ Close(context.Context) error })
	if !ok {
		logger.Warnf("core service does not implement Close")
		return
	}
	closeContext := context.Background()
	if ctx != nil {
		closeContext = context.WithoutCancel(ctx)
	}
	closeContext, cancel := context.WithTimeout(closeContext, 30*time.Second)
	defer cancel()
	logger.Infof("Wails shutdown callback closing core service")
	if err := closer.Close(closeContext); err != nil {
		logger.Errorf("core service close failed: %v", err)
		return
	}
}

func (a *App) emitAgentOutput(event agentruntime.AgentOutputEvent) {
	if event.AgentID == "" || event.ExecutionID == "" {
		return
	}
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx == nil {
		return
	}
	wailsruntime.EventsEmit(ctx, agentOutputEventName, event)
}

// Ping is a minimal desktop binding smoke endpoint and will be removed once domain bindings stabilize.
func (a *App) Ping() string {
	a.logDebug("binding Ping")
	return "pong"
}

func (a *App) Readiness() contracts.HealthSnapshot {
	a.mu.RLock()
	service, issue := a.service, a.startupIssue
	a.mu.RUnlock()
	if issue != nil {
		copy := *issue
		a.logDebug("binding Readiness ready=false issue=%s", issue.Code)
		return contracts.HealthSnapshot{Ready: false, Issue: &copy}
	}
	ready := service != nil && service.Ready()
	a.logDebug("binding Readiness ready=%t", ready)
	return contracts.HealthSnapshot{Ready: ready}
}

func startupIssue(err error) *contracts.StartupIssue {
	var configurationError *registry.ConfigurationError
	if errors.As(err, &configurationError) {
		message := "Configuration could not be loaded."
		if configurationError.Err != nil {
			message = configurationError.Err.Error()
		}
		return &contracts.StartupIssue{
			Code:    contracts.ErrorCodeConfiguration,
			Path:    configurationError.Path,
			Message: message,
		}
	}
	return &contracts.StartupIssue{
		Code:    contracts.ErrorCodeInternal,
		Message: "Praxis could not start. See the desktop log for details.",
	}
}

// ListSessions exposes only session metadata so the UI can discover durable
// workspaces without loading every Agent transcript or aggregate projection.
func (a *App) ListSessions() (response []contracts.SessionSummary, err error) {
	done := a.beginBinding("ListSessions")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return nil, err
	}
	catalog, ok := service.(sessionCatalogService)
	if !ok {
		return nil, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	sessions, err := catalog.ListSessions(ctx, 100)
	if err != nil {
		return nil, publicBindingError(err)
	}
	result := make([]contracts.SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, contracts.SessionSummary{
			ID:           session.ID.String(),
			Goal:         session.Goal,
			WorkspaceKey: session.WorkspaceKey,
			CreatedAt:    session.CreatedAt,
			UpdatedAt:    session.UpdatedAt,
		})
	}
	return result, nil
}

// ListModels exposes only confirmed model labels and their declared reasoning
// capabilities. Provider endpoints and credentials remain private.
func (a *App) ListModels() (response []contracts.ModelOption, err error) {
	done := a.beginBinding("ListModels")
	defer func() { done(err) }()
	_, service, err := a.bindingContext()
	if err != nil {
		return nil, err
	}
	catalog, ok := service.(modelCatalogService)
	if !ok {
		return nil, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	models := catalog.ListModels()
	result := make([]contracts.ModelOption, 0, len(models))
	for _, model := range models {
		result = append(result, contracts.ModelOption{
			ID: model.ID, Label: model.Label, ProviderLabel: model.ProviderLabel,
			Reasoning: contracts.ReasoningOption{
				Supported: model.Reasoning.Supported,
				Levels:    append([]string{}, model.Reasoning.Levels...),
				Default:   model.Reasoning.Default,
			},
			DefaultProfiles: append([]string{}, model.DefaultProfiles...),
		})
	}
	return result, nil
}

func (a *App) CreateProject(
	request contracts.CreateProjectRequest,
) (response contracts.CreateProjectResponse, err error) {
	done := a.beginBinding("CreateProject")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.CreateProjectResponse{}, err
	}
	creator, ok := service.(projectCreatorService)
	if !ok {
		return contracts.CreateProjectResponse{}, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	workspaceKey, err := a.createProjectWorkspace(request.ProjectName)
	if err != nil {
		return contracts.CreateProjectResponse{}, err
	}
	result, err := creator.CreateProject(ctx, orchestrate.CreateProjectRequest{
		WorkspaceKey: workspaceKey,
		Goal:         request.Goal,
	})
	if err != nil {
		_ = os.Remove(workspaceKey)
		return contracts.CreateProjectResponse{}, publicBindingError(err)
	}
	return contracts.CreateProjectResponse{
		SessionID:    result.Session.ID.String(),
		AgentID:      result.Agent.ID.String(),
		Goal:         result.Session.Goal,
		WorkspaceKey: result.Session.WorkspaceKey,
	}, nil
}

func (a *App) CreateSession(
	request contracts.CreateSessionRequest,
) (response contracts.CreateSessionResponse, err error) {
	done := a.beginBinding("CreateSession")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.CreateSessionResponse{}, err
	}
	creator, ok := service.(sessionCreatorService)
	if !ok {
		return contracts.CreateSessionResponse{}, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	result, err := creator.CreateSessionForWorkspace(ctx, request.WorkspaceKey, request.Goal)
	if err != nil {
		return contracts.CreateSessionResponse{}, publicBindingError(err)
	}
	return contracts.CreateSessionResponse{
		SessionID:    result.Session.ID.String(),
		AgentID:      result.Agent.ID.String(),
		Goal:         result.Session.Goal,
		WorkspaceKey: result.Session.WorkspaceKey,
	}, nil
}

// createProjectWorkspace reserves a private project directory under the
// current user's Praxis root. The frontend never chooses a filesystem path.
func (a *App) createProjectWorkspace(projectName string) (string, error) {
	projectName = strings.TrimSpace(projectName)
	if !validProjectName(projectName) {
		return "", bindingError(contracts.ErrorCodeProjectWorkspaceInvalid)
	}
	a.mu.RLock()
	root := a.projectRoot
	a.mu.RUnlock()
	if root == "" {
		resolved, err := storage.ResolveDataRoot("")
		if err != nil {
			return "", bindingError(contracts.ErrorCodeInternal)
		}
		root = resolved.Projects
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", bindingError(contracts.ErrorCodeInternal)
	}
	workspaceKey := filepath.Join(root, projectName)
	if err := os.Mkdir(workspaceKey, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", bindingError(contracts.ErrorCodeWorkspaceConflict)
		}
		return "", bindingError(contracts.ErrorCodeInternal)
	}
	return workspaceKey, nil
}

func validProjectName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		filepath.Clean(name) == name && !filepath.IsAbs(name) &&
		!strings.ContainsAny(name, "/\\\x00\r\n")
}

func (a *App) GetSession(sessionID string) (response contracts.SessionSnapshot, err error) {
	done := a.beginBinding("GetSession")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.SessionSnapshot{}, err
	}
	projection, err := service.ProjectSession(ctx, domain.SessionID(sessionID), 100)
	if err != nil {
		return contracts.SessionSnapshot{}, publicBindingError(err)
	}
	result := contracts.SessionSnapshot{
		ID:           projection.Session.ID.String(),
		Goal:         projection.Session.Goal,
		WorkspaceKey: projection.Session.WorkspaceKey,
		CreatedAt:    projection.Session.CreatedAt,
		UpdatedAt:    projection.Session.UpdatedAt,
		Groups:       make([]contracts.GroupSnapshot, 0, len(projection.Groups)),
		Agents:       make([]contracts.AgentSnapshot, 0, len(projection.Agents)),
	}
	for _, group := range projection.Groups {
		result.Groups = append(result.Groups, contracts.GroupSnapshot{
			ID:             group.ID.String(),
			PrimaryAgentID: group.PrimaryAgentID.String(),
			MaxConcurrent:  group.MaxConcurrent,
		})
	}
	for _, agent := range projection.Agents {
		result.Agents = append(result.Agents, contracts.AgentSnapshot{
			ID:               agent.ID.String(),
			SessionID:        agent.SessionID.String(),
			GroupID:          agent.GroupID.String(),
			Profile:          string(agent.Profile),
			State:            string(agent.State),
			CurrentExecution: agent.CurrentExecutionID.String(),
			Executions:       make([]contracts.ExecutionSnapshot, 0),
		})
	}
	return result, nil
}

func (a *App) GetAgent(agentID string) (response contracts.AgentSnapshot, err error) {
	done := a.beginBinding("GetAgent")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.AgentSnapshot{}, err
	}
	projection, err := service.ProjectAgent(ctx, domain.AgentID(agentID), 100)
	if err != nil {
		return contracts.AgentSnapshot{}, publicBindingError(err)
	}
	result := contracts.AgentSnapshot{
		ID:                projection.Agent.ID.String(),
		SessionID:         projection.Agent.SessionID.String(),
		GroupID:           projection.Agent.GroupID.String(),
		Profile:           string(projection.Agent.Profile),
		State:             string(projection.Agent.State),
		CurrentExecution:  projection.Agent.CurrentExecutionID.String(),
		ExecutionIDs:      make([]string, 0, len(projection.Executions)),
		Executions:        make([]contracts.ExecutionSnapshot, 0, len(projection.Executions)),
		WaitConditionIDs:  make([]string, 0, len(projection.Waits)),
		DeliveryIDs:       make([]string, 0, len(projection.Deliveries)),
		ControlRequestIDs: make([]string, 0, len(projection.Controls)),
	}
	for _, execution := range projection.Executions {
		result.ExecutionIDs = append(result.ExecutionIDs, execution.ID.String())
		result.Executions = append(result.Executions, contracts.ExecutionSnapshot{
			ID:          execution.ID.String(),
			Reason:      string(execution.Reason),
			Status:      string(execution.Status),
			Outcome:     string(execution.Outcome),
			FailureCode: string(execution.FailureCode),
			CreatedAt:   execution.CreatedAt,
			StartedAt:   execution.StartedAt,
			SettledAt:   execution.SettledAt,
		})
	}
	for _, wait := range projection.Waits {
		result.WaitConditionIDs = append(result.WaitConditionIDs, wait.ID.String())
	}
	for _, delivery := range projection.Deliveries {
		result.DeliveryIDs = append(result.DeliveryIDs, delivery.ID.String())
	}
	for _, control := range projection.Controls {
		result.ControlRequestIDs = append(result.ControlRequestIDs, control.ID.String())
	}
	return result, nil
}

func (a *App) ListAgentMessages(agentID string) (response []contracts.AgentMessage, err error) {
	done := a.beginBinding("ListAgentMessages")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return nil, err
	}
	messages, err := service.ListAgentMessages(ctx, domain.AgentID(agentID), 200)
	if err != nil {
		return nil, publicBindingError(err)
	}
	result := make([]contracts.AgentMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, contracts.AgentMessage{
			Sequence:    message.Sequence,
			At:          message.At,
			ExecutionID: message.ExecutionID.String(),
			Role:        message.Role,
			Content:     message.Content,
		})
	}
	return result, nil
}

func (a *App) ListAgentHistory(agentID string) (response []contracts.AgentHistoryItem, err error) {
	done := a.beginBinding("ListAgentHistory")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return nil, err
	}
	messages, err := service.ListAgentMessages(ctx, domain.AgentID(agentID), 200)
	if err != nil {
		return nil, publicBindingError(err)
	}
	projection, err := service.ProjectAgent(ctx, domain.AgentID(agentID), 200)
	if err != nil {
		return nil, publicBindingError(err)
	}
	result := make([]contracts.AgentHistoryItem, 0, len(messages)+len(projection.Executions))
	for _, message := range messages {
		value := contracts.AgentMessage{
			Sequence:    message.Sequence,
			At:          message.At,
			ExecutionID: message.ExecutionID.String(),
			Role:        message.Role,
			Content:     message.Content,
		}
		result = append(result, contracts.AgentHistoryItem{
			Kind:     "message",
			At:       message.At,
			Sequence: message.Sequence,
			Message:  &value,
		})
	}
	for _, execution := range projection.Executions {
		if execution.Status != domain.ExecutionSettled || execution.Outcome != domain.ExecutionFailed {
			continue
		}
		at := execution.SettledAt
		if at.IsZero() {
			at = execution.CreatedAt
		}
		value := contracts.ExecutionSnapshot{
			ID:          execution.ID.String(),
			Reason:      string(execution.Reason),
			Status:      string(execution.Status),
			Outcome:     string(execution.Outcome),
			FailureCode: string(execution.FailureCode),
			CreatedAt:   execution.CreatedAt,
			StartedAt:   execution.StartedAt,
			SettledAt:   execution.SettledAt,
		}
		result = append(result, contracts.AgentHistoryItem{
			Kind:      "execution",
			At:        at,
			Execution: &value,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].At.Equal(result[j].At) {
			return result[i].Sequence < result[j].Sequence
		}
		return result[i].At.Before(result[j].At)
	})
	return result, nil
}

func (a *App) SendInput(request contracts.SendInputRequest) (response contracts.SendInputResponse, err error) {
	done := a.beginBinding("SendInput")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.SendInputResponse{}, err
	}
	runtime, err := runtimeSnapshot(request.SandboxMode, request.ApprovalMode, request.Revision)
	if err != nil {
		return contracts.SendInputResponse{}, publicBindingError(err)
	}
	result, err := service.SendInput(ctx, orchestrate.SendInputRequest{
		AgentID: domain.AgentID(request.AgentID), RequestID: domain.RequestID(request.RequestID),
		Content: request.Content, ModelID: request.ModelID, Reasoning: request.Reasoning,
		RuntimeSnapshot: runtime,
	})
	if err != nil {
		return contracts.SendInputResponse{}, publicBindingError(err)
	}
	return contracts.SendInputResponse{
		ExecutionID: result.Execution.ID.String(), ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

func (a *App) Resume(request contracts.ResumeRequest) (response contracts.SendInputResponse, err error) {
	done := a.beginBinding("Resume")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.SendInputResponse{}, err
	}
	runtime, err := runtimeSnapshot(request.SandboxMode, request.ApprovalMode, request.Revision)
	if err != nil {
		return contracts.SendInputResponse{}, publicBindingError(err)
	}
	result, err := service.Resume(ctx, orchestrate.ResumeRequest{
		AgentID: domain.AgentID(request.AgentID), RequestID: domain.RequestID(request.RequestID),
		Content: request.Content, RuntimeSnapshot: runtime,
	})
	if err != nil {
		return contracts.SendInputResponse{}, publicBindingError(err)
	}
	return contracts.SendInputResponse{
		ExecutionID: result.Execution.ID.String(), ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

func (a *App) RequestControl(request contracts.ControlRequest) (response contracts.ControlResponse, err error) {
	done := a.beginBinding("RequestControl")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.ControlResponse{}, err
	}
	result, err := service.RequestControl(ctx, orchestrate.ControlRequest{
		ID: domain.AgentControlRequestID(request.ID), AgentID: domain.AgentID(request.AgentID),
		Kind: domain.AgentControlKind(request.Kind),
	})
	if err != nil {
		return contracts.ControlResponse{}, publicBindingError(err)
	}
	return contracts.ControlResponse{
		RequestID: result.Request.ID.String(), AgentID: result.Request.AgentID.String(),
		TargetExecutionID: result.Request.TargetExecutionID.String(), Kind: string(result.Request.Kind),
		Status: string(result.Request.Status), ExistingRequest: result.ExistingRequest,
		CancellationError: result.CancellationError,
	}, nil
}

func (a *App) QueueWork(request contracts.QueueWorkRequest) (response contracts.QueueWorkResponse, err error) {
	done := a.beginBinding("QueueWork")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.QueueWorkResponse{}, err
	}
	runtime, err := runtimeSnapshot(request.SandboxMode, request.ApprovalMode, request.Revision)
	if err != nil {
		return contracts.QueueWorkResponse{}, publicBindingError(err)
	}
	result, err := service.EnqueueWork(ctx, orchestrate.QueueWorkRequest{
		ID: domain.WorkItemID(request.ID), AgentID: domain.AgentID(request.AgentID),
		Prompt: request.Prompt, RuntimeSnapshot: runtime,
	})
	if err != nil {
		return contracts.QueueWorkResponse{}, publicBindingError(err)
	}
	return contracts.QueueWorkResponse{
		WorkID: result.Work.ID.String(), ExecutionID: result.Work.ExecutionID.String(),
		Status: string(result.Work.Status), ExistingWork: result.ExistingWork,
		ActivationError: result.ActivationError,
	}, nil
}

func (a *App) bindingContext() (context.Context, CoreService, error) {
	a.mu.RLock()
	ctx, service := a.ctx, a.service
	a.mu.RUnlock()
	if ctx == nil || service == nil || !service.Ready() {
		a.logDebug("binding rejected: core service is not ready")
		return nil, nil, bindingError(contracts.ErrorCodeNotReady)
	}
	return context.WithoutCancel(ctx), service, nil
}

func (a *App) beginBinding(name string) func(error) {
	started := time.Now()
	if commandBinding(name) {
		a.logInfo("binding=%s started", name)
	} else {
		a.logDebug("binding=%s started", name)
	}
	return func(err error) {
		if err != nil {
			a.logError("binding=%s failed: %v", name, err)
			return
		}
		if commandBinding(name) {
			a.logInfo("binding=%s completed duration_ms=%d", name, time.Since(started).Milliseconds())
			return
		}
		a.logDebug("binding=%s completed duration_ms=%d", name, time.Since(started).Milliseconds())
	}
}

func commandBinding(name string) bool {
	switch name {
	case "CreateProject", "CreateSession", "SendInput", "Resume", "RequestControl", "QueueWork":
		return true
	default:
		return false
	}
}

func (a *App) logInfo(format string, args ...any) {
	a.mu.RLock()
	logger := a.logger
	a.mu.RUnlock()
	logger.Infof(format, args...)
}

func (a *App) logDebug(format string, args ...any) {
	a.mu.RLock()
	logger := a.logger
	a.mu.RUnlock()
	logger.Debugf(format, args...)
}

func (a *App) logError(format string, args ...any) {
	a.mu.RLock()
	logger := a.logger
	a.mu.RUnlock()
	logger.Errorf(format, args...)
}

func runtimeSnapshot(sandbox, approval, revision string) (domain.RuntimeExecutionSnapshot, error) {
	return domain.NewRuntimeExecutionSnapshot(
		domain.SandboxMode(sandbox), domain.ApprovalMode(approval), revision,
	)
}

func publicBindingError(err error) error {
	if err == nil {
		return nil
	}
	var command *orchestrate.CommandError
	if errors.As(err, &command) {
		return publicCommandError(command)
	}
	if executionError, ok := publicExecutionError(err); ok {
		return executionError
	}
	switch {
	case errors.Is(err, context.Canceled):
		return bindingError(contracts.ErrorCodeRequestCanceled)
	case errors.Is(err, context.DeadlineExceeded):
		return bindingError(contracts.ErrorCodeRequestTimeout)
	case errors.Is(err, domain.ErrInvalidValue):
		return bindingError(contracts.ErrorCodeValidation)
	case errors.Is(err, domain.ErrInvalidTransition):
		return bindingError(contracts.ErrorCodeInvalidTransition)
	case errors.Is(err, domain.ErrLeaseConflict):
		return bindingError(contracts.ErrorCodeWorkspaceConflict)
	case errors.Is(err, domain.ErrAlreadySettled):
		return bindingError(contracts.ErrorCodeAlreadySettled)
	case errors.Is(err, domain.ErrWorkQueueEmpty):
		return bindingError(contracts.ErrorCodeWorkQueueEmpty)
	case errors.Is(err, domain.ErrWorkItemActive):
		return bindingError(contracts.ErrorCodeWorkItemActive)
	case errors.Is(err, domain.ErrAlreadyDelivered):
		return bindingError(contracts.ErrorCodeAlreadyDelivered)
	case errors.Is(err, domain.ErrRequestNotFound):
		return bindingError(contracts.ErrorCodeRequestNotFound)
	case errors.Is(err, domain.ErrRequestConflict):
		return bindingError(contracts.ErrorCodeRequestConflict)
	case errors.Is(err, domain.ErrNotFound):
		return bindingError(contracts.ErrorCodeNotFound)
	}
	return bindingError(contracts.ErrorCodeInternal)
}

func bindingError(code contracts.ErrorCode) error {
	return errors.New(code.String())
}

func publicCommandError(command *orchestrate.CommandError) error {
	if command == nil {
		return bindingError(contracts.ErrorCodeInternal)
	}
	switch command.Code {
	case orchestrate.CommandErrorAgentExecuting:
		return bindingError(contracts.ErrorCodeAgentExecuting)
	case orchestrate.CommandErrorAgentUnavailable:
		return bindingError(contracts.ErrorCodeAgentUnavailable)
	case orchestrate.CommandErrorInvalidRequest:
		return bindingError(contracts.ErrorCodeInvalidRequest)
	case orchestrate.CommandErrorNotReady:
		return bindingError(contracts.ErrorCodeNotReady)
	case orchestrate.CommandErrorProjectWorkspaceInvalid:
		return bindingError(contracts.ErrorCodeProjectWorkspaceInvalid)
	default:
		return bindingError(contracts.ErrorCodeInternal)
	}
}

func publicExecutionError(err error) (error, bool) {
	switch {
	case agentruntime.IsCode(err, agentruntime.ErrorBusy):
		return bindingError(contracts.ErrorCodeExecutionBusy), true
	case agentruntime.IsCode(err, agentruntime.ErrorContract):
		return bindingError(contracts.ErrorCodeExecutionContract), true
	case agentruntime.IsCode(err, agentruntime.ErrorPolicyBlocked):
		return bindingError(contracts.ErrorCodeExecutionPolicyBlocked), true
	case agentruntime.IsCode(err, agentruntime.ErrorApprovalRequired):
		return bindingError(contracts.ErrorCodeExecutionApproval), true
	case agentruntime.IsCode(err, agentruntime.ErrorStorage):
		return bindingError(contracts.ErrorCodeExecutionStorage), true
	case agentruntime.IsCode(err, agentruntime.ErrorProvider):
		return bindingError(contracts.ErrorCodeExecutionProvider), true
	case agentruntime.IsCode(err, agentruntime.ErrorTool):
		return bindingError(contracts.ErrorCodeExecutionTool), true
	case agentruntime.IsCode(err, agentruntime.ErrorResourceLimit):
		return bindingError(contracts.ErrorCodeExecutionResourceLimit), true
	case agentruntime.IsCode(err, agentruntime.ErrorClosed):
		return bindingError(contracts.ErrorCodeExecutionClosed), true
	case agentruntime.IsCode(err, agentruntime.ErrorInterrupted):
		return bindingError(contracts.ErrorCodeExecutionInterrupted), true
	default:
		return nil, false
	}
}
