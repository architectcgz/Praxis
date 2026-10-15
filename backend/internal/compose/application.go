package compose

import (
	"praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	"praxis/internal/hooks"

	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"time"

	appservices "praxis/internal/service"
	applicationagent "praxis/internal/service/agent"
	applicationproject "praxis/internal/service/project"
	applicationruntime "praxis/internal/service/runtime"
	runtimequeue "praxis/internal/service/runtime/queue"
	applicationtask "praxis/internal/service/runtime/task"
	applicationsession "praxis/internal/service/session"
	toolexecution "praxis/internal/tool_execution"

	agentregistry "praxis/internal/infra/agent_registry"
	"praxis/internal/infra/dataroot"
	"praxis/internal/infra/document"
	"praxis/internal/infra/jsonl"
	modelregistry "praxis/internal/infra/model_registry"
	"praxis/internal/infra/toolconfig"
	"praxis/internal/infra/workspacefs"
	"praxis/internal/logging"
	"praxis/internal/loop"
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
		Timings:        impl,
		Usages:         impl,
		SessionLogPath: a.store.SessionLogPath,
	}
}

// Open 构建应用并返回就绪的服务集；runLoop 为空时装配默认的 loop.Run 执行函数。
func Open(
	ctx context.Context,
	root dataroot.DataRoot,
	runLoop agentruntime.LoopFunc,
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
	toolConfig, err := toolconfig.Load(root.ToolConfig)
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
	if err := toolConfig.ValidateRegistered(registeredToolNames); err != nil {
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	toolCatalog := toolconfig.NewCatalog(toolConfig, toolRegistry)
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
	workspaceFilesystem := workspacefs.Filesystem{}
	messages := repos.Messages
	events := appservices.NewEventPublisher()
	usages := jsonl.ModelUsageStore{Store: store}
	observe := func(event agentruntime.AgentEvent) {
		if event.Kind == agentruntime.AgentEventModelUsage && event.Usage != nil {
			// 用量写入不继承业务取消，保存失败只影响统计，不改变模型响应。
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := usages.Save(ctx, model.ModelUsageRecord{
				SessionID: event.SessionID.String(),
				AgentID:   event.AgentID.String(),
				TaskID:    event.TaskID.String(),
				TurnID:    event.TurnID,
				Usage:     *event.Usage,
			})
			cancel()
			if err != nil {
				diagnostics.Errorf("token usage save failed task=%s turn=%s: %v", event.TaskID, event.TurnID, err)
			}
		}
		events.Publish(desktopAgentEvent(event))
	}
	timingStore := jsonl.TimingStore{Store: store}
	if err := timingStore.RecoverInterrupted(ctx); err != nil {
		_ = diagnostics.Close()
		closeStore()
		return nil, fmt.Errorf("recover operation timings: %w", err)
	}
	timings, err := timing.New(timingStore, func(record timing.Record) {
		value := desktopTiming(record)
		events.Publish(appservices.AgentEvent{
			Kind:    string(agentruntime.AgentEventTiming),
			AgentID: contracts.AgentID(record.AgentID),
			TaskID:  contracts.TaskID(record.TaskID),
			Timing:  &value,
		})
	}, diagnostics.Errorf)
	if err != nil {
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	hookDispatcher := hooks.NewDispatcher(timing.Hook{Recorder: timings})
	store.ObserveCommitted(hookDispatcher, diagnostics.Errorf)
	executions := &agentmodel.Executions{}
	agentService, err := applicationagent.NewService(applicationagent.Config{
		Transactions: store,
		Agents:       repos.Agents,
		Tasks:        repos.Tasks,
		Controls:     repos.Controls,
		Executions:   executions,
		Messages:     messages,
	})
	if err != nil {
		diagnostics.Errorf("create agent service failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	sessionService, err := applicationsession.NewService(applicationsession.Config{
		Transactions:   store,
		HookDispatcher: hookDispatcher,
		Projects:       repos.Projects,
		Workspaces:     repos.Workspaces,
		Sessions:       repos.Sessions,
		Contexts:       repos.Contexts,
		Agents:         repos.Agents,
		Tasks:          repos.Tasks,
		Definitions:    agentRegistry.Definition,
		Messages:       messages,
		UsageRecords:   usages.ListSession,
		TextReader:     workspaceFilesystem,
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
		closeStore()
		return nil, err
	}
	taskContextBuilder, err := applicationtask.NewTaskContextBuilder(applicationtask.ContextBuilderConfig{
		Transactions: store,
		Sessions:     repos.Sessions,
		Tasks:        repos.Tasks,
		Contexts:     repos.Contexts,
		Messages:     messages,
	})
	if err != nil {
		diagnostics.Errorf("create task context builder failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	taskService, err := applicationtask.NewService(applicationtask.Config{
		Controls:       repos.Controls,
		Transactions:   store,
		HookDispatcher: hookDispatcher,
		Logger:         diagnostics,
		EventObserver: func(event applicationtask.TerminalEvent) {
			events.Publish(terminalAgentEvent(event))
		},
		Workspaces:     repos.Workspaces,
		Sessions:       repos.Sessions,
		Agents:         repos.Agents,
		Tasks:          repos.Tasks,
		Executions:     executions,
		Messages:       repos.Messages,
		PrimaryAgent:   sessionService,
		Definitions:    taskInputProvider{agents: agentRegistry, models: modelRegistry},
		Models:         taskInputProvider{agents: agentRegistry, models: modelRegistry},
		ContextBuilder: taskContextBuilder,
	})
	if err != nil {
		diagnostics.Errorf("create task service failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	if runLoop == nil {
		toolExecutor, err := toolexecution.NewExecutor(toolexecution.Config{
			Transactions:   store,
			HookDispatcher: hookDispatcher,
			CommandPolicy:  toolCatalog,
			Tasks:          repos.Tasks,
			Invocations:    repos.ToolInvocations,
			Catalog:        toolCatalog,
		})
		if err != nil {
			_ = diagnostics.Close()
			closeStore()
			return nil, err
		}
		runLoop, err = newLoop(loop.Config{
			ModelBuilder:   modelRegistry,
			ContextBuilder: taskContextBuilder,
			Progress:       taskService,
			ToolExecutor:   toolExecutor,
			Tools:          toolCatalog,
			HookDispatcher: hookDispatcher,
			EventObserver:  observe,
			Logf:           diagnostics.Infof,
		})
		if err != nil {
			_ = diagnostics.Close()
			closeStore()
			return nil, err
		}
	}
	queueService, err := runtimequeue.NewService(runtimequeue.Config{
		Transactions: store,
		Agents:       repos.Agents,
		Tasks:        repos.Tasks,
		Messages:     repos.Messages,
	})
	if err != nil {
		diagnostics.Errorf("create task queue service failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	projectService, err := applicationproject.NewService(applicationproject.Config{
		Transactions: store,
		Projects:     repos.Projects,
		Workspaces:   repos.Workspaces,
		Directory:    workspaceFilesystem,
		Logger:       diagnostics,
	})
	if err != nil {
		diagnostics.Errorf("create project service failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	factory := runtimeFactory{
		hooks:      hookDispatcher,
		executions: executions,
		runLoop:    runLoop,
		messageRecorders: func(ctx context.Context, sessionID contracts.SessionID, agentID contracts.AgentID) (agentruntime.MessageRecorder, error) {
			return messages.Resolve(ctx, sessionID, agentID)
		},
		taskPreparer: taskService,
		transitions:  taskService,
		tasks:        repos.Tasks,
		logger:       diagnostics,
	}
	registry, err := agentruntime.NewRegistry(factory)
	if err != nil {
		diagnostics.Errorf("create runtime registry failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	runtimeService, err := applicationruntime.NewService(applicationruntime.Config{
		Tasks:     taskService,
		Queue:     queueService,
		Activator: registry,
		Approvals: applicationruntime.ToolApprovals{
			CommandPolicy: toolCatalog,
			Transactions:  store,
			Tasks:         repos.Tasks,
			Invocations:   repos.ToolInvocations,
		},
	})
	if err != nil {
		_ = registry.Close(context.Background())
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	activeTasks, err := repos.Tasks.ListActive(ctx)
	if err == nil {
		for _, task := range activeTasks {
			if err = registry.Activate(ctx, task); err != nil {
				break
			}
		}
	}
	if err != nil {
		_ = registry.Close(context.Background())
		_ = diagnostics.Close()
		closeStore()
		return nil, fmt.Errorf("resume active tasks: %w", err)
	}
	sessions, err := repos.Sessions.List(ctx, math.MaxInt)
	if err == nil {
		for _, session := range sessions {
			agents, listErr := repos.Agents.ListBySession(ctx, session.ID, math.MaxInt)
			if listErr != nil {
				err = listErr
				break
			}
			for _, agent := range agents {
				if agent.State != agentmodel.AgentIdle && agent.State != agentmodel.AgentWaiting {
					continue
				}
				if _, err = registry.StartNext(ctx, agent.ID); err != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		_ = registry.Close(context.Background())
		_ = diagnostics.Close()
		closeStore()
		return nil, fmt.Errorf("resume pending tasks: %w", err)
	}

	diagnostics.Infof("composition open completed ready=true")
	frontend := appservices.New(appservices.Config{
		Agents:      agentService,
		Projects:    projectService,
		Sessions:    sessionService,
		Runtime:     runtimeService,
		Models:      modelRegistry,
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
			if err := candidateTools.ValidateRegistered(registeredToolNames); err != nil {
				return err
			}
			candidateCatalog := toolconfig.NewCatalog(candidateTools, toolRegistry)
			if err := ctx.Err(); err != nil {
				return err
			}
			// 与 Task 准入共用事务边界，避免后台队列读到混合版本的模型、定义和权限。
			return store.InTx(ctx, func(context.Context) error {
				modelRegistry.ReplaceFrom(candidateModels)
				agentRegistry.ReplaceFrom(candidateAgents)
				toolCatalog.ReplaceFrom(candidateCatalog)
				return nil
			})
		},
		Events:           events,
		Logger:           diagnostics,
		ListAgentTimings: timings.ListAgent,
		ListSessionUsage: usages.ListSession,
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

type taskInputProvider struct {
	agents *agentregistry.Registry
	models *modelregistry.Registry
}

func (r taskInputProvider) Definition(definitionID contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	return r.agents.Definition(definitionID)
}

func (r taskInputProvider) FreezeTaskModel(providerID, modelID, reasoningLevel string) (model.ModelSnapshot, error) {
	return r.models.FreezeTaskModel(providerID, modelID, reasoningLevel)
}

func (r taskInputProvider) FreezeDefaultTaskModel(definitionID contracts.AgentDefinitionID) (model.ModelSnapshot, error) {
	reference, err := r.agents.ResolveModelReference(definitionID)
	if err != nil {
		return model.ModelSnapshot{}, err
	}
	return r.models.FreezeTaskModel(reference.ProviderID, reference.ModelID, "")
}
