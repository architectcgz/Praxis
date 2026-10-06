package compose

import (
	agentruntime "praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	turnmodel "praxis/internal/core/turn"

	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	appservices "praxis/internal/service"
	applicationagent "praxis/internal/service/agent"
	applicationproject "praxis/internal/service/project"
	applicationsession "praxis/internal/service/session"
	turncontrol "praxis/internal/service/turn/control"
	turnlifecycle "praxis/internal/service/turn/lifecycle"
	turnqueue "praxis/internal/service/turn/queue"
	turnstart "praxis/internal/service/turn/start"
	toolinvocation "praxis/internal/service/turn/tool_invocation"

	securitymodel "praxis/internal/core/security"
	agentregistry "praxis/internal/infra/agent_registry"
	"praxis/internal/infra/dataroot"
	"praxis/internal/infra/document"
	"praxis/internal/infra/jsonl"
	modelregistry "praxis/internal/infra/model_registry"
	"praxis/internal/infra/toolconfig"
	"praxis/internal/logging"
	"praxis/internal/timing"
	"praxis/internal/tools"
	toolcontracts "praxis/internal/tools/contracts"
	"praxis/wails/bindings"
)

// Application 是目标架构的组合根：创建具体实现、注入 service 与
// runtime，并持有进程级生命周期资源。前端端口由
// praxis/internal/service 实现，这里只负责装配与释放。
type Application struct {
	frontend *appservices.Services
	sessions *applicationsession.Service
	store    *jsonl.Store
	registry *agentruntime.Registry
	logger   *logging.Logger
	timings  *timing.Recorder
	usages   jsonl.ModelUsageStore
}

// Services 组装 wails 层注入所需的前端服务集。把 *appservices.Services 赋给
// bindings 的接口字段，编译期即可确认它满足全部端口。
func (a *Application) Services() bindings.Services {
	if a == nil || a.frontend == nil {
		return bindings.Services{}
	}
	impl := a.frontend
	return bindings.Services{
		Projects:       impl,
		Sessions:       impl,
		Agents:         impl,
		Commands:       impl,
		Models:         impl,
		ModelConfig:    impl,
		Events:         impl,
		Timings:        a.timings,
		Usages:         a.usages,
		SessionLogPath: a.store.SessionLogPath,
	}
}

// Open 构建应用并返回一个就绪的命令持有者。runner 是唯一的 provider/tool 接入点。
func Open(
	ctx context.Context,
	root dataroot.DataRoot,
	runner agentruntime.TurnRunner,
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
	store, err := jsonl.Open(ctx, root.Runtime)
	if err != nil {
		diagnostics.Errorf("open JSONL failed: %v", err)
		_ = diagnostics.Close()
		return nil, err
	}
	closeStore := func() { _ = store.Close(context.Background()) }
	diagnostics.Infof("composition open started root=%s logs=%s", root.Root, root.Runtime)
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
	repos := store.Repositories()
	messages := applicationsession.NewMessageStore(
		store, repos.Agents, repos.SessionMessages, repos.AgentMessages,
	)
	events := appservices.NewEventPublisher()
	usages := jsonl.ModelUsageStore{Store: store}
	observe := func(event agentruntime.AgentEvent) {
		if event.Kind == agentruntime.AgentEventModelUsage && event.Usage != nil {
			// 用量写入不继承业务取消，保存失败只影响统计，不改变模型响应。
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := usages.Save(ctx, agentruntime.ModelUsageRecord{
				SessionID: event.SessionID.String(),
				AgentID:   event.AgentID.String(),
				TurnID:    event.TurnID.String(),
				Step:      event.Step,
				Usage:     *event.Usage,
			})
			cancel()
			if err != nil {
				diagnostics.Errorf("token usage save failed turn=%s step=%d: %v", event.TurnID, event.Step, err)
			}
		}
		events.Publish(event)
	}
	timingStore := jsonl.TimingStore{Store: store}
	if err := timingStore.RecoverInterrupted(ctx); err != nil {
		_ = diagnostics.Close()
		closeStore()
		return nil, fmt.Errorf("recover operation timings: %w", err)
	}
	timings, err := timing.New(timingStore, func(record timing.Record) {
		events.Publish(agentruntime.AgentEvent{
			Kind:    agentruntime.AgentEventTiming,
			AgentID: contracts.AgentID(record.AgentID),
			TurnID:  contracts.TurnID(record.TurnID),
			Timing:  &record,
		})
	}, diagnostics.Errorf)
	if err != nil {
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	if runner == nil {
		runner, err = newTurnRunner(modelRegistry, toolinvocation.Config{
			Transactions:      store,
			Turns:             repos.Turns,
			SecuritySnapshots: repos.SecuritySnapshots,
			Invocations:       repos.ToolInvocations,
			Catalog:           toolRegistry,
		}, timings, observe, diagnostics.Infof)
		if err != nil {
			_ = diagnostics.Close()
			closeStore()
			return nil, err
		}
	}
	factory := runtimeFactory{
		runner:   timedRunner{next: runner, recorder: timings},
		messages: messages.Resolve,
		logger: func(turn turnmodel.Turn, stage string, err error) {
			if err != nil {
				diagnostics.Errorf("turn id=%s stage=%s failed: %v", turn.ID, stage, err)
			}
		},
		eventLogger: func(turn turnmodel.Turn, stage string) {
			diagnostics.Infof("turn id=%s stage=%s", turn.ID, stage)
		},
	}
	agentService, err := applicationagent.NewService(applicationagent.Config{
		Agents:   repos.Agents,
		Turns:    repos.Turns,
		Waits:    repos.Waits,
		Controls: repos.Controls,
		Messages: messages.ListAgent,
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
	sessionService, err := applicationsession.NewService(applicationsession.Config{
		Transactions:    store,
		Projects:        repos.Projects,
		Workspaces:      repos.Workspaces,
		Sessions:        repos.Sessions,
		Contexts:        repos.Contexts,
		Policies:        repos.Policies,
		Agents:          repos.Agents,
		Turns:           repos.Turns,
		ToolInvocations: repos.ToolInvocations,
		QueuedWork:      repos.QueuedWork,
		Controls:        repos.Controls,
		SessionMessages: repos.SessionMessages,
		AgentMessages:   repos.AgentMessages,
		Definitions:     agentRegistry.Definition,
		PolicyFactory:   agentRegistry.SecurityPolicy,
		Messages:        messages,
		UsageRecords:    usages.ListSession,
		RemoveSessionData: func(ctx context.Context, sessionID contracts.SessionID, documentRefs []string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			var cleanupErr error
			for _, ref := range documentRefs {
				cleanupErr = errors.Join(cleanupErr, documents.Remove(ctx, ref))
			}
			return cleanupErr
		},
	})
	if err != nil {
		diagnostics.Errorf("create session service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	if err := sessionService.RecoverStaleTurns(ctx); err != nil {
		diagnostics.Errorf("recover stale turns failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, fmt.Errorf("recover stale turns: %w", err)
	}
	lifecycleService, err := turnlifecycle.NewService(turnlifecycle.Config{
		Transactions:  store,
		Sessions:      repos.Sessions,
		Agents:        repos.Agents,
		Turns:         repos.Turns,
		QueuedWork:    repos.QueuedWork,
		Controls:      repos.Controls,
		Messages:      messages,
		Logger:        diagnostics,
		EventObserver: events.Publish,
	})
	if err != nil {
		diagnostics.Errorf("create turn lifecycle service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	startService, err := turnstart.NewService(turnstart.Config{
		Transactions: store, Workspaces: repos.Workspaces, Sessions: repos.Sessions,
		Policies: repos.Policies, Agents: repos.Agents,
		Turns:           repos.Turns,
		SessionMessages: repos.SessionMessages,
		AgentMessages:   repos.AgentMessages,
		ToolPermissions: toolPermissions,
		RegisteredTools: registeredToolNames,
		PrimaryAgent:    sessionService, Activator: registry,
		Lifecycle:           lifecycleService,
		Definitions:         turnInputProvider{agents: agentRegistry, models: modelRegistry},
		AgentDefinitionsDir: root.AgentDefinitions,
		Models:              turnInputProvider{agents: agentRegistry, models: modelRegistry},
		ContextProvider:     sessionService,
	})
	if err != nil {
		diagnostics.Errorf("create turn start service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	queueService, err := turnqueue.NewService(turnqueue.Config{
		Transactions:    store,
		Agents:          repos.Agents,
		Turns:           repos.Turns,
		QueuedWork:      repos.QueuedWork,
		SessionMessages: repos.SessionMessages,
		AgentMessages:   repos.AgentMessages,
		Inputs:          startService,
		Activator:       registry,
		Lifecycle:       lifecycleService,
	})
	if err != nil {
		diagnostics.Errorf("create turn queue service failed: %v", err)
		_ = diagnostics.Close()
		_ = registry.Close(context.Background())
		closeStore()
		return nil, err
	}
	if err := lifecycleService.SetQueueStarter(queuedWorkStarter{queue: queueService}); err != nil {
		diagnostics.Errorf("configure turn lifecycle queue starter failed: %v", err)
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
	controlService, err := turncontrol.NewService(turncontrol.Config{
		Transactions: store,
		Agents:       repos.Agents,
		Turns:        repos.Turns,
		Controls:     repos.Controls,
		Canceller:    registry,
		Ender:        lifecycleService,
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
		Starts:      startService,
		Models:      modelConfig,
		AgentConfig: agentRegistry,
		ReloadConfig: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			candidateModels, err := modelregistry.Load(root.ModelProvidersConfig, root.ModelCredentialsFile, nil, newModelStream)
			if err != nil {
				return err
			}
			candidateConfig := candidateModels.Config()
			candidateAgents, err := agentregistry.Load(root.AgentDefinitions, func(providerID, modelID string) error {
				if !modelregistry.ContainsModel(candidateConfig, providerID, modelID) {
					return fmt.Errorf("model %q for provider %q is not configured", modelID, providerID)
				}
				return nil
			})
			if err != nil {
				return err
			}
			candidateTools, err := toolconfig.Load(root.ToolConfig)
			if err != nil {
				return err
			}
			candidatePermissions, err := securitymodel.NewToolPermissionPolicy(candidateTools.Tools)
			if err != nil {
				return err
			}
			if err := candidatePermissions.ValidateRegistered(registeredToolNames); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			// 与 Turn 准入共用事务边界，避免后台队列读到混合版本的模型、定义和权限。
			return store.InTx(ctx, func(context.Context) error {
				modelRegistry.ReplaceFrom(candidateModels)
				agentRegistry.ReplaceFrom(candidateAgents)
				startService.UpdateToolPermissions(candidatePermissions)
				return nil
			})
		},
		Events: events,
		Logger: diagnostics,
	})
	return &Application{
		frontend: frontend,
		sessions: sessionService,
		store:    store,
		registry: registry,
		logger:   diagnostics,
		timings:  timings,
		usages:   usages,
	}, nil
}

// RuntimeLogger 暴露共享诊断日志给 Wails binding，但不暴露组合根内部实现。
func (a *Application) RuntimeLogger() *logging.Logger {
	factory := logging.NewFactory()
	if a == nil || a.logger == nil {
		return factory.Nop()
	}
	return factory.Ensure(a.logger)
}

// Close 停止 runtime，清理本次运行新建的空会话，再关闭存储和日志；返回汇总错误。
func (a *Application) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("application close context is required")
	}
	logger := a.RuntimeLogger()
	logger.Infof("composition close started")
	runtimeErr := a.registry.Close(ctx)
	sessionErr := a.sessions.CleanupEmptySessions(ctx)
	storeErr := a.store.Close(ctx)
	if runtimeErr != nil || sessionErr != nil || storeErr != nil {
		logger.Errorf(
			"composition close encountered errors runtime=%v sessions=%v store=%v",
			runtimeErr, sessionErr, storeErr,
		)
	}
	if runtimeErr == nil && sessionErr == nil && storeErr == nil {
		logger.Infof("composition close completed")
	}
	logErr := logger.Close()
	return errors.Join(runtimeErr, sessionErr, storeErr, logErr)
}

type turnInputProvider struct {
	agents *agentregistry.Registry
	models *modelregistry.Registry
}

func (r turnInputProvider) Definition(definitionID contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	return r.agents.Definition(definitionID)
}

func (r turnInputProvider) FreezeTurnModel(providerID, modelID, reasoningLevel string) (contracts.ModelSnapshot, error) {
	return r.models.FreezeTurnModel(providerID, modelID, reasoningLevel)
}

func (r turnInputProvider) FreezeDefaultTurnModel(definitionID contracts.AgentDefinitionID) (contracts.ModelSnapshot, error) {
	reference, err := r.agents.ResolveModelReference(definitionID)
	if err != nil {
		return contracts.ModelSnapshot{}, err
	}
	return r.models.FreezeTurnModel(reference.ProviderID, reference.ModelID, "")
}

type queuedWorkStarter struct {
	queue *turnqueue.Service
}

func (s queuedWorkStarter) StartNextQueuedWork(ctx context.Context, agentID contracts.AgentID) (bool, error) {
	result, err := s.queue.StartNextQueuedWork(ctx, agentID)
	return result.Started, err
}
