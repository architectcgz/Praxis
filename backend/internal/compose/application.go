package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	applicationagent "praxis/internal/application/agent"
	agentruntime "praxis/internal/application/agent_runtime"
	executioncontrol "praxis/internal/application/execution/control"
	executiondelivery "praxis/internal/application/execution/delivery"
	executionqueue "praxis/internal/application/execution/queue"
	executionsettlement "praxis/internal/application/execution/settlement"
	executionstart "praxis/internal/application/execution/start"
	applicationproject "praxis/internal/application/project"
	applicationsession "praxis/internal/application/session"
	corecommand "praxis/internal/core/command"
	domainagent "praxis/internal/core/domain/agent"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainproject "praxis/internal/core/domain/project"
	domainsecurity "praxis/internal/core/domain/security"
	domainsession "praxis/internal/core/domain/session"
	domainworkflow "praxis/internal/core/domain/workflow"
	domainworkspace "praxis/internal/core/domain/workspace"

	"praxis/internal/core/projection"
	"praxis/internal/core/session"
	"praxis/internal/logging"
	"praxis/internal/orchestration"
	"praxis/internal/providers/registry"
	"praxis/internal/storage/agentlog"
	"praxis/internal/storage/agentpolicy"
	"praxis/internal/storage/dataroot"
	"praxis/internal/storage/sqlite"
)

// Application is the production composition of target storage, application
// services, orchestration, runtime activation, delivery, and startup recovery.
type Application struct {
	readiness   *orchestration.ReadinessGate
	projections *projection.Service
	projects    *applicationproject.Service
	sessions    *applicationsession.Service
	controls    *executioncontrol.Service
	deliveries  *executiondelivery.Service
	agents      *applicationagent.Service
	queues      *executionqueue.Service
	settlements *executionsettlement.Service
	starts      *executionstart.Service
	store       *sqlite.Store
	models      *registry.Registry
	registry    *agentruntime.Registry
	scheduler   *orchestration.Scheduler
	runtimeLog  *runtimeLog
	output      *agentOutputPublisher
}

func (a *Application) ListProjects(ctx context.Context, limit int) ([]domainproject.Project, error) {
	return a.projections.ListProjects(ctx, limit)
}

func (a *Application) ListWorkspaces(ctx context.Context, projectID domainfoundation.ProjectID, limit int) ([]domainworkspace.Workspace, error) {
	return a.projections.ListWorkspaces(ctx, projectID, limit)
}

func (a *Application) ListSessionsByProject(
	ctx context.Context,
	projectID domainfoundation.ProjectID,
	limit int,
) ([]domainsession.Session, error) {
	return a.projections.ListSessionsByProject(ctx, projectID, limit)
}

func (a *Application) ListSessions(ctx context.Context, limit int) ([]domainsession.Session, error) {
	return a.projections.ListSessions(ctx, limit)
}

func (a *Application) ProjectSession(
	ctx context.Context,
	sessionID domainfoundation.SessionID,
	limit int,
) (projection.SessionProjection, error) {
	return a.projections.ProjectSession(ctx, sessionID, limit)
}

func (a *Application) ProjectAgent(
	ctx context.Context,
	agentID domainfoundation.AgentID,
	limit int,
) (projection.AgentProjection, error) {
	return a.projections.ProjectAgent(ctx, agentID, limit)
}

func (a *Application) ListSessionEvents(
	ctx context.Context,
	sessionID domainfoundation.SessionID,
	after time.Time,
	limit int,
) ([]domainfoundation.DomainEvent, error) {
	return a.projections.ListSessionEvents(ctx, sessionID, after, limit)
}

func (a *Application) ListAgentEvents(
	ctx context.Context,
	agentID domainfoundation.AgentID,
	after time.Time,
	limit int,
) ([]domainfoundation.DomainEvent, error) {
	return a.projections.ListAgentEvents(ctx, agentID, after, limit)
}

func (a *Application) ListExecutionEvents(
	ctx context.Context,
	executionID domainfoundation.AgentExecutionID,
	after time.Time,
	limit int,
) ([]domainfoundation.DomainEvent, error) {
	return a.projections.ListExecutionEvents(ctx, executionID, after, limit)
}

func (a *Application) CreateProject(ctx context.Context, name, path string, requestID domainfoundation.RequestID) (result applicationproject.CreateProjectResult, err error) {
	name = strings.TrimSpace(name)
	path = filepath.Clean(strings.TrimSpace(path))
	if name == "" || strings.ContainsAny(name, "\\/:*?\"<>|\x00\r\n") || name == "." || name == ".." || !filepath.IsAbs(path) {
		return result, corecommand.NewError(corecommand.ErrorProjectWorkspaceInvalid)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return result, err
	}
	result, err = a.projects.CreateProject(ctx, applicationproject.CreateProjectParams{
		RequestID: requestID, Name: name, Path: path,
	})
	return result, nil
}

func (a *Application) RequestControl(
	ctx context.Context,
	params executioncontrol.RequestParams,
) (executioncontrol.RequestResult, error) {
	return a.controls.RequestControl(ctx, params)
}

func (a *Application) SendInput(ctx context.Context, params executionstart.SendInputParams) (executionstart.Result, error) {
	return a.starts.SendInput(ctx, params)
}

func (a *Application) Resume(ctx context.Context, params executionstart.ResumeParams) (executionstart.Result, error) {
	return a.starts.Resume(ctx, params)
}

func (a *Application) EnqueueWork(ctx context.Context, params executionqueue.EnqueueParams) (executionqueue.EnqueueResult, error) {
	return a.queues.EnqueueWork(ctx, params)
}

func (a *Application) UpdateAgentPolicy(ctx context.Context, params applicationagent.UpdatePolicyParams) (applicationagent.UpdatePolicyResult, error) {
	return a.agents.UpdatePolicy(ctx, params)
}

func (a *Application) CreateSessionForProject(
	ctx context.Context,
	requestID domainfoundation.RequestID,
	projectID domainfoundation.ProjectID,
	workspaceID domainfoundation.WorkspaceID,
	goal string,
) (applicationsession.CreateResult, error) {
	return a.sessions.CreateSessionForProject(ctx, requestID, projectID, workspaceID, goal)
}

// Open builds a target application and completes recovery before returning a
// ready command owner. The runner is the only provider/tool integration point.
func Open(
	ctx context.Context,
	root dataroot.DataRoot,
	runner agentruntime.TargetExecutionRunner,
) (*Application, error) {
	if ctx == nil {
		return nil, errors.New("application composition context is required")
	}
	if err := root.Initialize(ctx); err != nil {
		return nil, err
	}
	store, err := sqlite.Open(ctx, root.Database)
	if err != nil {
		return nil, err
	}
	closeStore := func() { _ = store.Close(context.Background()) }
	if err := store.VerifyTargetIntegrity(ctx); err != nil {
		closeStore()
		return nil, fmt.Errorf("verify target schema: %w", err)
	}
	diagnostics, err := openRuntimeLog(root)
	if err != nil {
		closeStore()
		return nil, fmt.Errorf("open runtime log: %w", err)
	}
	diagnostics.logger.Infof("composition open started root=%s database=%s", root.Root, root.Database)
	modelRegistry, err := registry.Load(root.ModelsConfig, root.SecretsConfig, nil)
	if err != nil {
		diagnostics.logger.Errorf("load model registry failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	policyStore, err := agentpolicy.NewStore(root.AgentPolicyFile)
	if err != nil {
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	systemPolicy, err := policyStore.Current(ctx)
	if err != nil {
		_ = diagnostics.Close()
		closeStore()
		return nil, fmt.Errorf("load agent policy: %w", err)
	}
	target := store.TargetRepositories()
	output := newAgentOutputPublisher()
	if runner == nil {
		runner, err = agentruntime.NewExecutionEngine(agentruntime.ExecutionEngineConfig{
			Models: providerModelResolver{registry: modelRegistry}, OutputObserver: output.Publish,
			Logf: diagnostics.Logger().Infof,
		})
		if err != nil {
			_ = diagnostics.Close()
			closeStore()
			return nil, err
		}
	}
	factory := targetRuntimeFactory{
		root:        root,
		runner:      runner,
		header:      newSessionHeaderResolver(store),
		logger:      diagnostics.Log,
		eventLogger: diagnostics.Event,
		output:      output.Publish,
	}
	projections, err := projection.NewService(projection.Config{
		Projects:   target.Projects,
		Workspaces: target.Workspaces,
		Sessions:   target.Sessions,
		Contexts:   target.Contexts,
		Agents:     target.Agents,
		Executions: target.Executions,
		Waits:      target.Waits,
		Controls:   target.Controls,
		Deliveries: target.Deliveries,
		Events:     target.Events,
		Messages:   newAgentMessageQuery(root),
	})
	if err != nil {
		diagnostics.logger.Errorf("create projection service failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	registry, err := agentruntime.NewRegistry(factory)
	if err != nil {
		diagnostics.logger.Errorf("create runtime registry failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	scheduler, err := orchestration.NewScheduler(orchestration.SchedulerConfig{
		Executions: target.Executions,
		Runtime:    registry,
	})
	if err != nil {
		diagnostics.logger.Errorf("create execution scheduler failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	readiness := orchestration.NewReadinessGate(false)
	sessionService, err := applicationsession.NewService(applicationsession.Config{
		Transactions:    store,
		Projects:        target.Projects,
		Workspaces:      target.Workspaces,
		Sessions:        target.Sessions,
		Contexts:        target.Contexts,
		Policies:        target.Policies,
		Agents:          target.Agents,
		CommandReceipts: target.Commands,
		Events:          target.Events,
		Readiness:       readiness,
		PolicyFactory:   policyFactory(systemPolicy),
	})
	if err != nil {
		diagnostics.logger.Errorf("create session service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	settlementService, err := executionsettlement.NewService(executionsettlement.Config{
		Transactions: store,
		Agents:       target.Agents,
		Executions:   target.Executions,
		QueuedWork:   target.QueuedWork,
		Controls:     target.Controls,
		Events:       target.Events,
	})
	if err != nil {
		diagnostics.logger.Errorf("create execution settlement service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	deliveryInputs := &executionInputFactory{}
	deliveryService, err := executiondelivery.NewService(executiondelivery.Config{
		Transactions:    store,
		Agents:          target.Agents,
		Executions:      target.Executions,
		Waits:           target.Waits,
		Deliveries:      target.Deliveries,
		CommandReceipts: target.Commands,
		Events:          target.Events,
		Inputs:          deliveryInputs,
		Activator:       scheduler,
		Lifecycle:       settlementService,
	})
	if err != nil {
		diagnostics.logger.Errorf("create execution delivery service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	startService, err := executionstart.NewService(executionstart.Config{
		Transactions: store, Workspaces: target.Workspaces, Sessions: target.Sessions,
		Contexts: target.Contexts, Policies: target.Policies, Agents: target.Agents,
		Executions: target.Executions, Deliveries: target.Deliveries, Events: target.Events,
		Readiness: readiness, PrimaryAgent: sessionService, Activator: scheduler,
		Lifecycle: settlementService, Models: modelRegistry,
	})
	if err != nil {
		diagnostics.logger.Errorf("create execution start service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	queueService, err := executionqueue.NewService(executionqueue.Config{
		Transactions: store,
		Agents:       target.Agents,
		Executions:   target.Executions,
		QueuedWork:   target.QueuedWork,
		Deliveries:   target.Deliveries,
		Receipts:     target.Commands,
		Events:       target.Events,
		Readiness:    readiness,
		Inputs:       startService,
		Activator:    scheduler,
		Lifecycle:    settlementService,
	})
	if err != nil {
		diagnostics.logger.Errorf("create execution queue service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	if err := settlementService.SetQueueStarter(queuedWorkStarter{queue: queueService}); err != nil {
		diagnostics.logger.Errorf("configure execution settlement queue starter failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	agentService, err := applicationagent.NewService(applicationagent.Config{
		Transactions:    store,
		Agents:          target.Agents,
		Policies:        target.Policies,
		CommandReceipts: target.Commands,
		Events:          target.Events,
		Readiness:       readiness,
	})
	if err != nil {
		diagnostics.logger.Errorf("create agent policy service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	projectService, err := applicationproject.NewService(applicationproject.Config{
		Transactions:    store,
		Projects:        target.Projects,
		Workspaces:      target.Workspaces,
		CommandReceipts: target.Commands,
		Events:          target.Events,
		Readiness:       readiness,
	})
	if err != nil {
		diagnostics.logger.Errorf("create project service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	controlService, err := executioncontrol.NewService(executioncontrol.Config{
		Transactions:    store,
		Agents:          target.Agents,
		Executions:      target.Executions,
		Controls:        target.Controls,
		CommandReceipts: target.Commands,
		Events:          target.Events,
		Readiness:       readiness,
		Canceller:       registry,
	})
	if err != nil {
		diagnostics.logger.Errorf("create control service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	commands := orchestrationCommandAdapter{readiness: readiness, controls: controlService, deliveries: deliveryService, queues: queueService, settlements: settlementService}
	sessions := newAgentSessionResolver(root)
	delivery, err := orchestration.NewDeliveryCoordinator(orchestration.DeliveryCoordinatorConfig{
		Commands:        commands,
		Sessions:        sessions,
		SessionHeader:   newDeliveryHeaderResolver(store),
		ResolveArtifact: store.ResolveContextArtifact,
	})
	if err != nil {
		diagnostics.logger.Errorf("create delivery coordinator failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	recovery, err := orchestration.NewRecoveryCoordinator(orchestration.RecoveryCoordinatorConfig{
		Agents:     target.Agents,
		Contexts:   target.Contexts,
		Executions: target.Executions,
		Waits:      target.Waits,
		Controls:   target.Controls,
		Deliveries: target.Deliveries,
		Commands:   commands,
		Lifecycle:  settlementService,
		Scheduler:  scheduler,
		Delivery:   delivery,
		Sessions:   sessions,
	})
	if err != nil {
		diagnostics.logger.Errorf("create recovery coordinator failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	deliveryInputs.start = startService
	if _, err := recovery.Recover(ctx); err != nil {
		diagnostics.logger.Errorf("startup recovery failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, fmt.Errorf("startup recovery: %w", err)
	}
	diagnostics.logger.Infof("startup recovery completed")
	diagnostics.logger.Infof("composition open completed ready=true")
	return &Application{
		readiness:   readiness,
		projections: projections,
		projects:    projectService,
		sessions:    sessionService,
		controls:    controlService,
		deliveries:  deliveryService,
		agents:      agentService,
		queues:      queueService,
		settlements: settlementService,
		starts:      startService,
		store:       store,
		models:      modelRegistry,
		registry:    registry,
		scheduler:   scheduler,
		runtimeLog:  diagnostics,
		output:      output,
	}, nil
}

// RuntimeLogger exposes the shared diagnostics sink to the Wails binding
// layer without exposing composition internals or storage adapters.
func (a *Application) RuntimeLogger() *logging.Logger {
	factory := logging.NewFactory()
	if a == nil || a.runtimeLog == nil {
		return factory.Nop()
	}
	return factory.Ensure(a.runtimeLog.Logger())
}

// SubscribeAgentOutput registers a listener for transient provider output.
// Consumers must use the durable transcript after a settled notification.
func (a *Application) SubscribeAgentOutput(observer agentruntime.AgentOutputObserver) func() {
	if a == nil || a.output == nil {
		return func() {}
	}
	return a.output.Subscribe(observer)
}

// ListModels returns configured model capabilities for the desktop binding.
func (a *Application) ListModels() []registry.ModelOption {
	if a.models == nil {
		return []registry.ModelOption{}
	}
	return a.models.ListModels()
}

// ModelConfig returns an editable copy of the full model configuration for
// the settings UI to render.
func (a *Application) ModelConfig() registry.FileConfig {
	if a.models == nil {
		return registry.FileConfig{}
	}
	return a.models.Config()
}

// SaveModelConfig validates and persists a new model configuration. A saved
// configuration takes effect immediately for the running orchestration layer.
func (a *Application) SaveModelConfig(config registry.FileConfig) error {
	if a.models == nil {
		return errors.New("model registry is not available")
	}
	if err := a.models.ApplyConfig(config); err != nil {
		return err
	}
	a.RuntimeLogger().Infof(
		"model config saved providers=%d models=%d", len(config.Providers), len(config.Models),
	)
	return nil
}

// SetProviderKey stores or clears the API key for one configured provider.
func (a *Application) SetProviderKey(providerID, value string) error {
	if a.models == nil {
		return errors.New("model registry is not available")
	}
	if err := a.models.SetProviderKey(providerID, value); err != nil {
		return err
	}
	a.RuntimeLogger().Infof("provider API key updated provider=%s cleared=%t", providerID, value == "")
	return nil
}

// HasProviderKey reports secret presence without exposing the configured key.
func (a *Application) HasProviderKey(providerID string) bool {
	if a.models == nil {
		return false
	}
	return a.models.HasProviderKey(providerID)
}

// DiscoverProviderModels retrieves model IDs from the configured provider
// endpoint while keeping the resolved API key inside the registry.
func (a *Application) DiscoverProviderModels(ctx context.Context, providerID string) ([]string, error) {
	if a.models == nil {
		return nil, errors.New("model registry is not available")
	}
	return a.models.DiscoverProviderModels(ctx, providerID)
}

// Close prevents new commands before stopping runtime actors and storage.
func (a *Application) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("application close context is required")
	}
	a.SetReady(false)
	logger := a.RuntimeLogger()
	logger.Infof("composition close started")
	runtimeErr := a.registry.Close(ctx)
	storeErr := a.store.Close(ctx)
	if runtimeErr != nil || storeErr != nil {
		logger.Errorf("composition close encountered errors runtime=%v store=%v", runtimeErr, storeErr)
	}
	if runtimeErr == nil && storeErr == nil {
		logger.Infof("composition close completed")
	}
	logErr := a.runtimeLog.Close()
	if runtimeErr != nil && storeErr != nil && logErr != nil {
		return fmt.Errorf("close runtimes: %v; close store: %v; close runtime log: %w", runtimeErr, storeErr, logErr)
	}
	if runtimeErr != nil && storeErr != nil {
		return fmt.Errorf("close runtimes: %v; close store: %w", runtimeErr, storeErr)
	}
	if runtimeErr != nil && logErr != nil {
		return fmt.Errorf("close runtimes: %v; close runtime log: %w", runtimeErr, logErr)
	}
	if storeErr != nil && logErr != nil {
		return fmt.Errorf("close store: %v; close runtime log: %w", storeErr, logErr)
	}
	if runtimeErr != nil {
		return runtimeErr
	}
	if storeErr != nil {
		return storeErr
	}
	return logErr
}

func (a *Application) ListAgentMessages(
	ctx context.Context,
	agentID domainfoundation.AgentID,
	limit int,
) ([]session.AgentSessionMessage, error) {
	return a.projections.ListAgentMessages(ctx, agentID, limit)
}

type targetRuntimeFactory struct {
	root        dataroot.DataRoot
	runner      agentruntime.TargetExecutionRunner
	header      agentruntime.TargetSessionHeaderResolver
	logger      agentruntime.TargetExecutionLogger
	eventLogger agentruntime.TargetExecutionEventLogger
	output      agentruntime.AgentOutputObserver
}

// orchestrationCommandAdapter adapts application command results to the
// narrow orchestration contracts at the composition root.
type orchestrationCommandAdapter struct {
	readiness   *orchestration.ReadinessGate
	controls    *executioncontrol.Service
	deliveries  *executiondelivery.Service
	queues      *executionqueue.Service
	settlements *executionsettlement.Service
}

func (a orchestrationCommandAdapter) SetReady(ready bool) {
	a.readiness.SetReady(ready)
}

func (a orchestrationCommandAdapter) ClaimContextDelivery(
	ctx context.Context,
	deliveryID domainfoundation.DeliveryID,
) (orchestration.ContextDeliveryClaim, error) {
	claim, err := a.deliveries.ClaimContextDelivery(ctx, deliveryID)
	if err != nil {
		return orchestration.ContextDeliveryClaim{}, err
	}
	return orchestration.ContextDeliveryClaim{
		Delivery: claim.Delivery,
		Claimed:  claim.Claimed,
	}, nil
}

func (a orchestrationCommandAdapter) CompleteContextDelivery(
	ctx context.Context,
	request orchestration.ContextDeliveryCompletionRequest,
) (orchestration.ContextDeliveryCompletion, error) {
	completion, err := a.deliveries.CompleteContextDelivery(ctx, executiondelivery.CompleteParams{
		DeliveryID:       request.DeliveryID,
		ArtifactEntryRef: request.ArtifactEntryRef,
		RequestID:        request.RequestID,
	})
	if err != nil {
		return orchestration.ContextDeliveryCompletion{}, err
	}
	return orchestration.ContextDeliveryCompletion{
		Delivery:         completion.Delivery,
		Execution:        completion.Execution,
		ExistingDelivery: completion.ExistingDelivery,
		ActivationError:  completion.ActivationError,
	}, nil
}

func (a orchestrationCommandAdapter) SettleRuntimeExecution(
	ctx context.Context,
	executionID domainfoundation.AgentExecutionID,
	outcome domainexecution.ExecutionOutcome,
	failureCode domainexecution.ExecutionFailureCode,
) error {
	return a.settlements.SettleRuntimeExecution(ctx, executionID, outcome, failureCode)
}

func (a orchestrationCommandAdapter) ApplyControlRequest(
	ctx context.Context,
	requestID domainfoundation.AgentControlRequestID,
) error {
	return a.controls.ApplyControlRequest(ctx, requestID)
}

func (a orchestrationCommandAdapter) StartNextQueuedWork(
	ctx context.Context,
	agentID domainfoundation.AgentID,
) (bool, error) {
	result, err := a.queues.StartNextQueuedWork(ctx, agentID)
	if err != nil {
		return false, err
	}
	return result.Started, nil
}

func (a orchestrationCommandAdapter) IsAgentUnavailable(err error) bool {
	return corecommand.HasError(err, corecommand.ErrorAgentUnavailable)
}

var (
	_ orchestration.DeliveryCommands = orchestrationCommandAdapter{}
	_ orchestration.RecoveryCommands = orchestrationCommandAdapter{}
)

func (f targetRuntimeFactory) New(
	ctx context.Context,
	agentID domainfoundation.AgentID,
) (agentruntime.ManagedRuntime, error) {
	openSessions := func(
		sessionID domainfoundation.SessionID,
		targetAgentID domainfoundation.AgentID,
	) (session.TranscriptReceiptStore, error) {
		return agentlog.Open(f.root, sessionID, targetAgentID)
	}
	return agentruntime.NewTargetRuntime(agentruntime.TargetRuntimeConfig{
		AgentID:           agentID,
		Sessions:          openSessions,
		Header:            f.header,
		Runner:            f.runner,
		Logger:            f.logger,
		EventLogger:       f.eventLogger,
		OutputObserver:    f.output,
		SettlementTimeout: 30 * time.Second,
	})
}

func newAgentSessionResolver(root dataroot.DataRoot) orchestration.AgentSessionResolver {
	return func(sessionID domainfoundation.SessionID, agentID domainfoundation.AgentID) (session.TranscriptReceiptStore, error) {
		return agentlog.Open(root, sessionID, agentID)
	}
}

func newAgentMessageQuery(root dataroot.DataRoot) projection.AgentMessageQuery {
	return func(ctx context.Context, sessionID domainfoundation.SessionID, agentID domainfoundation.AgentID, limit int) ([]session.AgentSessionMessage, error) {
		store, err := agentlog.Open(root, sessionID, agentID)
		if err != nil {
			return nil, err
		}
		defer func() { _ = store.Close(context.Background()) }()
		return store.ListMessages(ctx, limit)
	}
}

func policyFactory(snapshot domainsecurity.AgentPolicySnapshot) applicationsession.AgentSecurityPolicyFactory {
	return func(workspace domainworkspace.Workspace, profile domainsecurity.AgentProfile) (domainsecurity.AgentSecurityPolicy, error) {
		template, ok := snapshot.TemplateFor(profile)
		if !ok {
			return domainsecurity.AgentSecurityPolicy{}, fmt.Errorf("agent policy has no template for profile %s", profile)
		}
		capabilities := domainsecurity.CapabilityPolicy{AllowedTools: template.AllowedTools}
		switch template.WorkspaceAccess {
		case domainsecurity.WorkspaceAccessRead:
			capabilities.ReadScopes = []string{workspace.Path}
		case domainsecurity.WorkspaceAccessReadWrite:
			capabilities.ReadScopes = []string{workspace.Path}
			capabilities.WriteScopes = []string{workspace.Path}
		}
		return domainsecurity.NewAgentSecurityPolicy(1, capabilities,
			domainsecurity.SandboxPolicy{Mode: snapshot.SandboxMode},
			domainsecurity.ApprovalPolicy{Mode: snapshot.ApprovalMode})
	}
}

func newSessionHeaderResolver(store *sqlite.Store) agentruntime.TargetSessionHeaderResolver {
	return func(ctx context.Context, execution domainexecution.AgentExecution) (session.AgentSessionHeader, error) {
		agent, err := store.GetAgent(ctx, execution.AgentID)
		if err != nil {
			return session.AgentSessionHeader{}, err
		}
		return newSessionHeader(ctx, store, agent), nil
	}
}

func newDeliveryHeaderResolver(store *sqlite.Store) orchestration.DeliverySessionHeaderResolver {
	return func(ctx context.Context, delivery domainworkflow.ContextDelivery) (session.AgentSessionHeader, error) {
		agent, err := store.GetAgent(ctx, delivery.TargetAgentID)
		if err != nil {
			return session.AgentSessionHeader{}, err
		}
		return newSessionHeader(ctx, store, agent), nil
	}
}

func newSessionHeader(ctx context.Context, store *sqlite.Store, agent domainagent.Agent) session.AgentSessionHeader {
	sessionRecord, err := store.GetSession(ctx, agent.SessionID)
	workspaceID := domainfoundation.WorkspaceID("")
	if err == nil {
		workspaceID = sessionRecord.WorkspaceID
	}
	return session.AgentSessionHeader{
		SessionID:        agent.SessionID,
		AgentID:          agent.ID,
		Profile:          agent.Profile,
		WorkspaceID:      workspaceID,
		InjectionNonce:   agent.SessionID.String() + ":" + agent.ID.String(),
		MinReaderVersion: 2,
		WrittenBy:        "praxis/target",
	}
}

type executionInputFactory struct {
	start *executionstart.Service
}

func (f executionInputFactory) MaterializeExecutionInput(
	ctx context.Context,
	agent domainagent.Agent,
	providerID string,
	modelID string,
	reasoning string,
) (domainexecution.ExecutionInputSnapshot, error) {
	if f.start == nil {
		return domainexecution.ExecutionInputSnapshot{}, errors.New("execution start service is not configured")
	}
	return f.start.MaterializeExecutionInput(ctx, agent, providerID, modelID, reasoning)
}

type queuedWorkStarter struct {
	queue *executionqueue.Service
}

func (s queuedWorkStarter) StartNextQueuedWork(ctx context.Context, agentID domainfoundation.AgentID) (bool, error) {
	result, err := s.queue.StartNextQueuedWork(ctx, agentID)
	return result.Started, err
}

func (a *Application) Ready() bool {
	return a.readiness.Ready()
}

func (a *Application) SetReady(ready bool) {
	a.readiness.SetReady(ready)
}
