package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"praxis/internal/agentruntime"
	domainagent "praxis/internal/core/domain/agent"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainproject "praxis/internal/core/domain/project"
	domainsecurity "praxis/internal/core/domain/security"
	domainsession "praxis/internal/core/domain/session"
	domainworkflow "praxis/internal/core/domain/workflow"
	domainworkspace "praxis/internal/core/domain/workspace"

	"praxis/internal/core/orchestrate"
	"praxis/internal/core/session"
	"praxis/internal/logging"
	"praxis/internal/providers/registry"
	"praxis/internal/storage/agentlog"
	"praxis/internal/storage/agentpolicy"
	"praxis/internal/storage/dataroot"
	"praxis/internal/storage/sqlite"
)

// Application is the production composition of target storage, orchestration,
// runtime activation, delivery, and startup recovery. It embeds only the core
// command/query surface used by the Wails binding.
type Application struct {
	*orchestrate.AgentOrchestrator
	store      *sqlite.Store
	models     *registry.Registry
	registry   *orchestrate.AgentRuntimeRegistry
	scheduler  *orchestrate.ExecutionScheduler
	runtimeLog *runtimeLog
	output     *agentOutputPublisher
}

func (a *Application) ListProjects(ctx context.Context, limit int) ([]domainproject.Project, error) {
	return a.AgentOrchestrator.ListProjects(ctx, limit)
}

func (a *Application) ListWorkspaces(ctx context.Context, projectID domainfoundation.ProjectID, limit int) ([]domainworkspace.Workspace, error) {
	return a.AgentOrchestrator.ListWorkspaces(ctx, projectID, limit)
}

func (a *Application) ListSessionsByProject(
	ctx context.Context,
	projectID domainfoundation.ProjectID,
	limit int,
) ([]domainsession.Session, error) {
	return a.AgentOrchestrator.ListSessionsByProject(ctx, projectID, limit)
}

func (a *Application) CreateProject(ctx context.Context, name, path string, requestID domainfoundation.RequestID) (result orchestrate.CreateProjectResult, err error) {
	name = strings.TrimSpace(name)
	path = filepath.Clean(strings.TrimSpace(path))
	if name == "" || strings.ContainsAny(name, "\\/:*?\"<>|\x00\r\n") || name == "." || name == ".." || !filepath.IsAbs(path) {
		return result, &orchestrate.CommandError{Code: orchestrate.CommandErrorProjectWorkspaceInvalid}
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return result, err
	}
	result, err = a.AgentOrchestrator.CreateProject(ctx, orchestrate.CreateProjectRequest{
		RequestID: requestID, Name: name, Path: path,
	})
	return result, nil
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
	registry, err := orchestrate.NewAgentRuntimeRegistry(factory)
	if err != nil {
		diagnostics.logger.Errorf("create runtime registry failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	scheduler, err := orchestrate.NewExecutionScheduler(orchestrate.ExecutionSchedulerConfig{
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
	orchestrator, err := orchestrate.NewAgentOrchestrator(orchestrate.AgentOrchestratorConfig{
		Transactions:    store,
		Projects:        target.Projects,
		Workspaces:      target.Workspaces,
		Sessions:        target.Sessions,
		Contexts:        target.Contexts,
		Policies:        target.Policies,
		Agents:          target.Agents,
		Executions:      target.Executions,
		QueuedWork:      target.QueuedWork,
		Waits:           target.Waits,
		Controls:        target.Controls,
		Deliveries:      target.Deliveries,
		CommandReceipts: target.Commands,
		Events:          target.Events,
		Messages:        newAgentMessageQuery(root),
		PolicyFactory:   policyFactory(systemPolicy),
		Activator:       scheduler,
		Canceller:       registry,
		Models:          modelRegistry,
		InitiallyReady:  false,
	})
	if err != nil {
		diagnostics.logger.Errorf("create agent orchestrator failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	sessions := newAgentSessionResolver(root)
	delivery, err := orchestrate.NewDeliveryCoordinator(orchestrate.DeliveryCoordinatorConfig{
		Orchestrator:    orchestrator,
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
	recovery, err := orchestrate.NewRecoveryCoordinator(orchestrate.RecoveryCoordinatorConfig{
		Agents:       target.Agents,
		Contexts:     target.Contexts,
		Executions:   target.Executions,
		Waits:        target.Waits,
		Controls:     target.Controls,
		Deliveries:   target.Deliveries,
		Orchestrator: orchestrator,
		Scheduler:    scheduler,
		Delivery:     delivery,
		Sessions:     sessions,
	})
	if err != nil {
		diagnostics.logger.Errorf("create recovery coordinator failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
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
		AgentOrchestrator: orchestrator,
		store:             store,
		models:            modelRegistry,
		registry:          registry,
		scheduler:         scheduler,
		runtimeLog:        diagnostics,
		output:            output,
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
	return a.AgentOrchestrator.ListAgentMessages(ctx, agentID, limit)
}

type targetRuntimeFactory struct {
	root        dataroot.DataRoot
	runner      agentruntime.TargetExecutionRunner
	header      agentruntime.TargetSessionHeaderResolver
	logger      agentruntime.TargetExecutionLogger
	eventLogger agentruntime.TargetExecutionEventLogger
	output      agentruntime.AgentOutputObserver
}

func (f targetRuntimeFactory) New(
	ctx context.Context,
	agentID domainfoundation.AgentID,
) (orchestrate.ManagedAgentRuntime, error) {
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

func newAgentSessionResolver(root dataroot.DataRoot) orchestrate.AgentSessionResolver {
	return func(sessionID domainfoundation.SessionID, agentID domainfoundation.AgentID) (session.TranscriptReceiptStore, error) {
		return agentlog.Open(root, sessionID, agentID)
	}
}

func newAgentMessageQuery(root dataroot.DataRoot) orchestrate.AgentMessageQuery {
	return func(ctx context.Context, sessionID domainfoundation.SessionID, agentID domainfoundation.AgentID, limit int) ([]session.AgentSessionMessage, error) {
		store, err := agentlog.Open(root, sessionID, agentID)
		if err != nil {
			return nil, err
		}
		defer func() { _ = store.Close(context.Background()) }()
		return store.ListMessages(ctx, limit)
	}
}

func policyFactory(snapshot domainsecurity.AgentPolicySnapshot) orchestrate.AgentSecurityPolicyFactory {
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

func newDeliveryHeaderResolver(store *sqlite.Store) orchestrate.DeliverySessionHeaderResolver {
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
