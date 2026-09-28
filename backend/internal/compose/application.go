package compose

import (
	agentmodel "praxis/internal/agent"
	agentassembly "praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"

	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	agentruntime "praxis/internal/runtime/agent"
	appservices "praxis/internal/service"
	applicationagent "praxis/internal/service/agent"
	executioncontrol "praxis/internal/service/execution/control"
	executionqueue "praxis/internal/service/execution/queue"
	executionsettlement "praxis/internal/service/execution/settlement"
	executionstart "praxis/internal/service/execution/start"
	toolinvocation "praxis/internal/service/execution/tool_invocation"
	applicationproject "praxis/internal/service/project"
	applicationsession "praxis/internal/service/session"

	agentregistry "praxis/internal/infra/agent_registry"
	"praxis/internal/infra/agentlog"
	"praxis/internal/infra/dataroot"
	"praxis/internal/infra/document"
	modelregistry "praxis/internal/infra/model_registry"
	"praxis/internal/infra/providers/anthropicmessages"
	openaichat "praxis/internal/infra/providers/openai_chat"
	openairesponses "praxis/internal/infra/providers/openai_responses"
	"praxis/internal/infra/sqlite"
	"praxis/internal/infra/storage"
	"praxis/internal/infra/toolconfig"
	"praxis/internal/logging"
	"praxis/internal/orchestration"
	"praxis/internal/runtime"
	securitymodel "praxis/internal/security"
	"praxis/internal/tools"
	toolcontracts "praxis/internal/tools/contracts"
	"praxis/wails/bindings"
)

// Application 是目标架构的组合根：创建具体实现、注入 service 与
// orchestration，并持有进程级生命周期资源。前端端口由
// praxis/internal/service 实现，这里只负责装配与释放。
type Application struct {
	frontend *appservices.Services
	store    *sqlite.Store
	registry *agentruntime.Registry
	logger   *logging.Logger
}

// Services 组装 wails 层注入所需的前端服务集。把 *appservices.Services 赋给
// bindings 的接口字段，编译期即可确认它满足全部端口。
func (a *Application) Services() bindings.Services {
	if a == nil || a.frontend == nil {
		return bindings.Services{}
	}
	impl := a.frontend
	return bindings.Services{
		Projects:    impl,
		Sessions:    impl,
		Agents:      impl,
		Commands:    impl,
		Models:      impl,
		ModelConfig: impl,
		Events:      impl,
	}
}

// Open 构建应用并返回一个就绪的命令持有者。runner 是唯一的 provider/tool 接入点。
func Open(
	ctx context.Context,
	root dataroot.DataRoot,
	runner agentruntime.ExecutionRunner,
	registeredTools ...toolcontracts.Tool,
) (*Application, error) {
	if ctx == nil {
		return nil, errors.New("application composition context is required")
	}
	if err := root.Initialize(ctx); err != nil {
		return nil, err
	}
	diagnostics, err := logging.NewFactory().Runtime(filepath.Join(root.Runtime, "praxis.log"))
	if err != nil {
		return nil, fmt.Errorf("open runtime log: %w", err)
	}
	store, err := sqlite.Open(ctx, root.Database)
	if err != nil {
		diagnostics.Errorf("open sqlite failed: %v", err)
		_ = diagnostics.Close()
		return nil, err
	}
	closeStore := func() { _ = store.Close(context.Background()) }
	diagnostics.Infof("composition open started root=%s database=%s", root.Root, root.Database)
	modelRegistry, err := modelregistry.Load(root.ModelProvidersConfig, root.ModelCredentialsFile, nil, newModelStream)
	if err != nil {
		diagnostics.Errorf("load model registry failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	modelConfig := modelregistry.NewApplicationAdapter(modelRegistry)
	toolConfig, err := toolconfig.Load(root.ToolConfig)
	if err != nil {
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	toolPermissions, err := securitymodel.NewToolPermissionPolicy(toolConfig.Tools)
	if err != nil {
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	toolRegistry := tools.NewToolRegistry()
	for _, tool := range registeredTools {
		if err := toolRegistry.Register(tool); err != nil {
			_ = diagnostics.Close()
			closeStore()
			return nil, err
		}
	}
	definitions := toolRegistry.List()
	registeredToolNames := make([]contracts.ToolName, 0, len(definitions))
	for _, definition := range definitions {
		registeredToolNames = append(registeredToolNames, definition.Name)
	}
	if err := toolPermissions.ValidateRegistered(registeredToolNames); err != nil {
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	agentRegistry, err := agentregistry.Load(root.AgentDefinitions, func(providerID, modelID string) error {
		if !modelregistry.ContainsModel(modelRegistry.Config(), providerID, modelID) {
			return fmt.Errorf("model %q for provider %q is not configured", modelID, providerID)
		}
		return nil
	})
	if err != nil {
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	documents, err := document.New(root.Documents)
	if err != nil {
		closeStore()
		return nil, fmt.Errorf("open document store: %w", err)
	}
	repos, err := storage.NewRepositories(store, documents)
	if err != nil {
		closeStore()
		return nil, err
	}
	events := appservices.NewEventPublisher()
	if runner == nil {
		runner, err = agentassembly.NewRunner(agentassembly.RunnerConfig{
			ModelBuilder: modelRegistry,
			ToolInvocations: toolinvocation.Config{
				Transactions:      store,
				Executions:        repos.Executions,
				SecuritySnapshots: repos.SecuritySnapshots,
				Invocations:       repos.ToolInvocations,
				Catalog:           toolRegistry,
			},
			EventObserver: events.Publish,
			Logf:          diagnostics.Infof,
		})
		if err != nil {
			_ = diagnostics.Close()
			closeStore()
			return nil, err
		}
	}
	factory := runtimeFactory{
		root:   root,
		runner: runner,
		header: newSessionHeaderResolver(store),
		logger: func(execution executionmodel.AgentExecution, stage string, err error) {
			if err != nil {
				diagnostics.Errorf("execution id=%s stage=%s failed: %v", execution.ID, stage, err)
			}
		},
		eventLogger: func(execution executionmodel.AgentExecution, stage string) {
			diagnostics.Infof("execution id=%s stage=%s", execution.ID, stage)
		},
		eventObserver: events.Publish,
	}
	agentService, err := applicationagent.NewService(applicationagent.Config{
		Transactions: store,
		Agents:       repos.Agents,
		Policies:     repos.Policies,
		Executions:   repos.Executions,
		Waits:        repos.Waits,
		Controls:     repos.Controls,
		Messages:     newAgentMessageReader(root),
	})
	if err != nil {
		diagnostics.Errorf("create agent service failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	registry, err := agentruntime.NewRegistry(factory)
	if err != nil {
		diagnostics.Errorf("create runtime registry failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	scheduler, err := orchestration.NewScheduler(orchestration.SchedulerConfig{
		Runtime: registry,
	})
	if err != nil {
		diagnostics.Errorf("create execution scheduler failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	sessionService, err := applicationsession.NewService(applicationsession.Config{
		Transactions:  store,
		Projects:      repos.Projects,
		Workspaces:    repos.Workspaces,
		Sessions:      repos.Sessions,
		Contexts:      repos.Contexts,
		Policies:      repos.Policies,
		Agents:        repos.Agents,
		Definitions:   agentRegistry.Definition,
		PolicyFactory: agentRegistry.SecurityPolicy,
		Transcripts:   agentTranscriptLoader{root: root},
	})
	if err != nil {
		diagnostics.Errorf("create session service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	settlementService, err := executionsettlement.NewService(executionsettlement.Config{
		Transactions: store,
		Sessions:     repos.Sessions,
		Agents:       repos.Agents,
		Executions:   repos.Executions,
		QueuedWork:   repos.QueuedWork,
		Controls:     repos.Controls,
		Transcripts:  agentTranscriptLoader{root: root},
		Logger:       diagnostics,
	})
	if err != nil {
		diagnostics.Errorf("create execution settlement service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	startService, err := executionstart.NewService(executionstart.Config{
		Transactions: store, Workspaces: repos.Workspaces, Sessions: repos.Sessions,
		Policies: repos.Policies, Agents: repos.Agents,
		Executions:      repos.Executions,
		ToolPermissions: toolPermissions,
		RegisteredTools: registeredToolNames,
		PrimaryAgent:    sessionService, Activator: scheduler,
		Lifecycle:       settlementService,
		Definitions:     executionInputProvider{agents: agentRegistry, models: modelRegistry},
		Models:          executionInputProvider{agents: agentRegistry, models: modelRegistry},
		ContextProvider: newExecutionContextProvider(repos.Contexts, agentTranscriptLoader{root: root}),
	})
	if err != nil {
		diagnostics.Errorf("create execution start service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	queueService, err := executionqueue.NewService(executionqueue.Config{
		Transactions: store,
		Agents:       repos.Agents,
		Executions:   repos.Executions,
		QueuedWork:   repos.QueuedWork,
		Inputs:       startService,
		Activator:    scheduler,
		Lifecycle:    settlementService,
	})
	if err != nil {
		diagnostics.Errorf("create execution queue service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	if err := settlementService.SetQueueStarter(queuedWorkStarter{queue: queueService}); err != nil {
		diagnostics.Errorf("configure execution settlement queue starter failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	projectService, err := applicationproject.NewService(applicationproject.Config{
		Transactions: store,
		Projects:     repos.Projects,
		Workspaces:   repos.Workspaces,
	})
	if err != nil {
		diagnostics.Errorf("create project service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	controlService, err := executioncontrol.NewService(executioncontrol.Config{
		Transactions: store,
		Agents:       repos.Agents,
		Controls:     repos.Controls,
		Canceller:    registry,
		Settler:      settlementService,
	})
	if err != nil {
		diagnostics.Errorf("create control service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	diagnostics.Infof("composition open completed ready=true")
	frontend := appservices.New(appservices.Config{
		Agents:      agentService,
		Projects:    projectService,
		Sessions:    sessionService,
		Controls:    controlService,
		Queues:      queueService,
		Settlements: settlementService,
		Starts:      startService,
		Models:      modelConfig,
		AgentConfig: agentRegistry,
		Events:      events,
		Logger:      diagnostics,
	})
	return &Application{
		frontend: frontend,
		store:    store,
		registry: registry,
		logger:   diagnostics,
	}, nil
}

func newModelStream(
	format modelregistry.ModelAPIFormat,
	provider modelregistry.ProviderConfig,
	modelConfig modelregistry.ModelConfig,
	apiKey string,
	client *http.Client,
) (runtime.ModelStream, error) {
	switch format {
	case modelregistry.APIFormatAnthropicMessages:
		return anthropicmessages.New(anthropicmessages.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			HTTPClient:    client,
			ContextWindow: modelConfig.ContextWindow,
		})
	case modelregistry.APIFormatOpenAIChatCompletions:
		return openaichat.New(openaichat.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			HTTPClient:    client,
			ContextWindow: modelConfig.ContextWindow,
		})
	case modelregistry.APIFormatOpenAIResponses:
		return openairesponses.New(openairesponses.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			HTTPClient:    client,
			ContextWindow: modelConfig.ContextWindow,
		})
	default:
		return nil, fmt.Errorf("unsupported model API format %q", format)
	}
}

// RuntimeLogger 暴露共享诊断日志给 Wails binding，但不暴露组合根内部实现。
func (a *Application) RuntimeLogger() *logging.Logger {
	factory := logging.NewFactory()
	if a == nil || a.logger == nil {
		return factory.Nop()
	}
	return factory.Ensure(a.logger)
}

// Close prevents new commands before stopping runtime actors and storage.
func (a *Application) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("application close context is required")
	}
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
	logErr := logger.Close()
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

type runtimeFactory struct {
	root          dataroot.DataRoot
	runner        agentruntime.ExecutionRunner
	header        agentruntime.SessionHeaderResolver
	logger        agentruntime.ExecutionLogger
	eventLogger   agentruntime.ExecutionEventLogger
	eventObserver agentruntime.AgentEventObserver
}

func (f runtimeFactory) New(
	ctx context.Context,
	agentID contracts.AgentID,
) (agentruntime.ManagedRuntime, error) {
	openSessions := func(
		sessionID contracts.SessionID,
		targetAgentID contracts.AgentID,
	) (runtime.TranscriptStore, error) {
		return agentlog.Open(f.root, sessionID, targetAgentID)
	}
	return agentruntime.NewRuntime(agentruntime.RuntimeConfig{
		AgentID:           agentID,
		Sessions:          openSessions,
		Header:            f.header,
		Runner:            f.runner,
		Logger:            f.logger,
		EventLogger:       f.eventLogger,
		EventObserver:     f.eventObserver,
		SettlementTimeout: 30 * time.Second,
	})
}

func newAgentMessageReader(root dataroot.DataRoot) func(context.Context, contracts.SessionID, contracts.AgentID, int) ([]runtime.AgentSessionMessage, error) {
	return func(ctx context.Context, sessionID contracts.SessionID, agentID contracts.AgentID, limit int) ([]runtime.AgentSessionMessage, error) {
		store, err := agentlog.Open(root, sessionID, agentID)
		if err != nil {
			return nil, err
		}
		defer func() { _ = store.Close(context.Background()) }()
		return store.ListMessages(ctx, limit)
	}
}

type executionInputProvider struct {
	agents *agentregistry.Registry
	models *modelregistry.Registry
}

func (r executionInputProvider) Definition(definitionID contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	return r.agents.Definition(definitionID)
}

func (r executionInputProvider) FreezeExecutionModel(providerID, modelID, reasoningLevel string) (contracts.ExecutionModelSnapshot, error) {
	return r.models.FreezeExecutionModel(providerID, modelID, reasoningLevel)
}

func (r executionInputProvider) FreezeDefaultExecutionModel(definitionID contracts.AgentDefinitionID) (contracts.ExecutionModelSnapshot, error) {
	reference, err := r.agents.ResolveModelReference(definitionID)
	if err != nil {
		return contracts.ExecutionModelSnapshot{}, err
	}
	return r.models.FreezeExecutionModel(reference.ProviderID, reference.ModelID, "")
}

func newSessionHeaderResolver(store *sqlite.Store) agentruntime.SessionHeaderResolver {
	return func(ctx context.Context, execution executionmodel.AgentExecution) (runtime.AgentSessionHeader, error) {
		agent, err := store.GetAgent(ctx, execution.AgentID)
		if err != nil {
			return runtime.AgentSessionHeader{}, err
		}
		return newSessionHeader(ctx, store, agent), nil
	}
}

func newSessionHeader(ctx context.Context, store *sqlite.Store, agent agentmodel.Agent) runtime.AgentSessionHeader {
	sessionRecord, err := store.GetSession(ctx, agent.SessionID)
	workspaceID := contracts.WorkspaceID("")
	if err == nil {
		workspaceID = sessionRecord.WorkspaceID
	}
	return runtime.AgentSessionHeader{
		SessionID:        agent.SessionID,
		AgentID:          agent.ID,
		DefinitionID:     agent.DefinitionID,
		Profile:          agent.Profile,
		WorkspaceID:      workspaceID,
		InjectionNonce:   agent.SessionID.String() + ":" + agent.ID.String(),
		MinReaderVersion: 4,
		WrittenBy:        "praxis",
	}
}

type queuedWorkStarter struct {
	queue *executionqueue.Service
}

func (s queuedWorkStarter) StartNextQueuedWork(ctx context.Context, agentID contracts.AgentID) (bool, error) {
	result, err := s.queue.StartNextQueuedWork(ctx, agentID)
	return result.Started, err
}

type agentTranscriptLoader struct {
	root dataroot.DataRoot
}

func (c agentTranscriptLoader) LoadTranscript(
	ctx context.Context,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
) (runtime.AgentTranscript, error) {
	store, err := agentlog.Open(c.root, sessionID, agentID)
	if err != nil {
		return runtime.AgentTranscript{}, err
	}
	defer func() { _ = store.Close(context.Background()) }()
	return store.LoadTranscript(ctx)
}
