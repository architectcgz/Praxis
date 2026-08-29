package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"praxis/internal/agentruntime"
	"praxis/internal/core/domain"
	"praxis/internal/core/orchestrate"
	"praxis/internal/core/session"
	"praxis/internal/logging"
	"praxis/internal/providers/registry"
	"praxis/internal/storage"
	"praxis/internal/storage/agentlog"
	"praxis/internal/storage/sqlite"
)

// Application is the production composition of target storage, orchestration,
// runtime activation, delivery, and startup recovery. It embeds only the core
// command/query surface used by the Wails binding.
type Application struct {
	*orchestrate.AgentOrchestrator
	root       storage.DataRoot
	store      *sqlite.Store
	models     *registry.Registry
	registry   *orchestrate.AgentRuntimeRegistry
	scheduler  *orchestrate.ExecutionScheduler
	runtimeLog *runtimeLog
	output     *agentOutputPublisher
}

// Open builds a target application and completes recovery before returning a
// ready command owner. The runner is the only provider/tool integration point.
func Open(
	ctx context.Context,
	root storage.DataRoot,
	runner agentruntime.TargetExecutionRunner,
) (*Application, error) {
	if ctx == nil {
		return nil, errors.New("application composition context is required")
	}
	if err := root.Initialize(ctx); err != nil {
		return nil, err
	}
	if _, err := os.Stat(root.Database); err == nil {
		inventory, err := sqlite.InspectExisting(ctx, root.Database)
		if err != nil {
			return nil, err
		}
		if inventory.HasLegacyData() {
			return nil, errors.New(
				"legacy data root requires an explicit stopped backup and conversion",
			)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect data root database: %w", err)
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
	legacy := store.Repositories()
	target := store.TargetRepositories()
	output := newAgentOutputPublisher()
	if runner == nil {
		runner = &providerRunner{
			store: store, root: root, registry: modelRegistry, logger: diagnostics.Logger(),
			outputObserver: output.Publish,
		}
	}
	factory := targetRuntimeFactory{
		root:        root,
		runner:      runner,
		header:      newSessionHeaderResolver(store),
		logger:      diagnostics.Log,
		eventLogger: diagnostics.Event,
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
		Transactions:   store,
		Sessions:       target.Sessions,
		Groups:         target.Groups,
		Agents:         target.Agents,
		Executions:     target.Executions,
		QueuedWork:     target.QueuedWork,
		TaskPackets:    legacy.TaskPackets,
		Manifests:      legacy.ContextManifests,
		Grants:         legacy.CapabilityGrants,
		Waits:          target.Waits,
		Controls:       target.Controls,
		Deliveries:     target.Deliveries,
		Activator:      scheduler,
		Canceller:      registry,
		Models:         modelRegistry,
		InitiallyReady: false,
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
		Executions:   target.Executions,
		Controls:     target.Controls,
		Deliveries:   target.Deliveries,
		Orchestrator: orchestrator,
		Scheduler:    scheduler,
		Delivery:     delivery,
		Sessions:     sessions,
		Snapshot:     recoverySnapshot,
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
		root:              root,
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
	agentID domain.AgentID,
	limit int,
) ([]session.AgentSessionMessage, error) {
	if ctx == nil {
		return nil, errors.New("agent message context is required")
	}
	if agentID == "" {
		return nil, errors.New("agent message agent id is required")
	}
	projection, err := a.ProjectAgent(ctx, agentID, 1)
	if err != nil {
		return nil, err
	}
	store, err := agentlog.Open(a.root, projection.Agent.SessionID, agentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close(context.Background()) }()
	return store.ListMessages(ctx, limit)
}

type targetRuntimeFactory struct {
	root        storage.DataRoot
	runner      agentruntime.TargetExecutionRunner
	header      agentruntime.TargetSessionHeaderResolver
	logger      agentruntime.TargetExecutionLogger
	eventLogger agentruntime.TargetExecutionEventLogger
}

func (f targetRuntimeFactory) New(
	ctx context.Context,
	agentID domain.AgentID,
) (orchestrate.ManagedAgentRuntime, error) {
	openSessions := func(
		sessionID domain.SessionID,
		targetAgentID domain.AgentID,
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
		SettlementTimeout: 30 * time.Second,
	})
}

func newAgentSessionResolver(root storage.DataRoot) orchestrate.AgentSessionResolver {
	return func(sessionID domain.SessionID, agentID domain.AgentID) (session.TranscriptReceiptStore, error) {
		return agentlog.Open(root, sessionID, agentID)
	}
}

func newSessionHeaderResolver(store *sqlite.Store) agentruntime.TargetSessionHeaderResolver {
	return func(ctx context.Context, execution domain.AgentExecution) (session.AgentSessionHeader, error) {
		agent, err := store.GetAgent(ctx, execution.AgentID)
		if err != nil {
			return session.AgentSessionHeader{}, err
		}
		return newSessionHeader(ctx, store, agent), nil
	}
}

func newDeliveryHeaderResolver(store *sqlite.Store) orchestrate.DeliverySessionHeaderResolver {
	return func(ctx context.Context, delivery domain.ContextDelivery) (session.AgentSessionHeader, error) {
		agent, err := store.GetAgent(ctx, delivery.TargetAgentID)
		if err != nil {
			return session.AgentSessionHeader{}, err
		}
		return newSessionHeader(ctx, store, agent), nil
	}
}

func newSessionHeader(ctx context.Context, store *sqlite.Store, agent domain.Agent) session.AgentSessionHeader {
	sessionRecord, err := store.GetSession(ctx, agent.SessionID)
	workspace := ""
	if err == nil {
		workspace = sessionRecord.WorkspaceKey
	}
	return session.AgentSessionHeader{
		SessionID:        agent.SessionID,
		AgentID:          agent.ID,
		Profile:          agent.Profile,
		WorkspaceKey:     workspace,
		InjectionNonce:   agent.SessionID.String() + ":" + agent.ID.String(),
		MinReaderVersion: 2,
		WrittenBy:        "praxis/target",
	}
}

func recoverySnapshot(context.Context, domain.Agent) (domain.RuntimeExecutionSnapshot, error) {
	return domain.NewRuntimeExecutionSnapshot(
		domain.SandboxReadOnly,
		domain.ApprovalAlwaysAsk,
		"recovery-v1",
	)
}
