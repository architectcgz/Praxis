package compose

import (
	"praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"

	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	appservices "praxis/internal/service"
	applicationagent "praxis/internal/service/agent"
	applicationproject "praxis/internal/service/project"
	applicationruntime "praxis/internal/service/runtime"
	runtimequeue "praxis/internal/service/runtime/queue"
	applicationtask "praxis/internal/service/runtime/task"
	tasklifecycle "praxis/internal/service/runtime/task/lifecycle"
	taskstart "praxis/internal/service/runtime/task/start"
	toolinvocation "praxis/internal/service/runtime/task/tool_invocation"
	taskturn "praxis/internal/service/runtime/task/turn"
	applicationsession "praxis/internal/service/session"

	securitymodel "praxis/internal/core/security"
	agentregistry "praxis/internal/infra/agent_registry"
	"praxis/internal/infra/dataroot"
	"praxis/internal/infra/document"
	"praxis/internal/infra/jsonl"
	modelregistry "praxis/internal/infra/model_registry"
	"praxis/internal/infra/toolconfig"
	"praxis/internal/infra/workspacefs"
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
	if runLoop == nil {
		runLoop, err = newLoop(modelRegistry, toolinvocation.Config{
			Transactions:      store,
			Tasks:             repos.Tasks,
			SecuritySnapshots: repos.SecuritySnapshots,
			Invocations:       repos.ToolInvocations,
			Catalog:           toolRegistry,
		}, taskturn.Config{
			Transactions: store,
			Turns:        repos.Turns,
		}, timings, observe, diagnostics.Infof)
		if err != nil {
			_ = diagnostics.Close()
			closeStore()
			return nil, err
		}
	}
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
		Transactions:    store,
		Projects:        repos.Projects,
		Workspaces:      repos.Workspaces,
		Sessions:        repos.Sessions,
		Contexts:        repos.Contexts,
		Policies:        repos.Policies,
		Agents:          repos.Agents,
		Tasks:           repos.Tasks,
		ToolInvocations: repos.ToolInvocations,
		Controls:        repos.Controls,
		Turns:           repos.Turns,
		Definitions:     agentRegistry.Definition,
		PolicyFactory:   agentRegistry.SecurityPolicy,
		Messages:        messages,
		UsageRecords:    usages.ListSession,
		TextReader:      workspaceFilesystem,
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
	if err := sessionService.RecoverStaleTasks(ctx); err != nil {
		diagnostics.Errorf("recover stale tasks failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, fmt.Errorf("recover stale tasks: %w", err)
	}
	lifecycleService, err := tasklifecycle.NewService(tasklifecycle.Config{
		Transactions: store,
		Sessions:     repos.Sessions,
		Agents:       repos.Agents,
		Tasks:        repos.Tasks,
		Controls:     repos.Controls,
		Messages:     messages,
		Logger:       diagnostics,
		EventObserver: func(event tasklifecycle.TerminalEvent) {
			events.Publish(terminalAgentEvent(event))
		},
	})
	if err != nil {
		diagnostics.Errorf("create task lifecycle service failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	startService, err := taskstart.NewService(taskstart.Config{
		Transactions:        store,
		Workspaces:          repos.Workspaces,
		Sessions:            repos.Sessions,
		Policies:            repos.Policies,
		Agents:              repos.Agents,
		Tasks:               repos.Tasks,
		Executions:          executions,
		Messages:            repos.Messages,
		ToolPermissions:     toolPermissions,
		RegisteredTools:     registeredToolNames,
		PrimaryAgent:        sessionService,
		Definitions:         taskInputProvider{agents: agentRegistry, models: modelRegistry},
		AgentDefinitionsDir: root.AgentDefinitions,
		Models:              taskInputProvider{agents: agentRegistry, models: modelRegistry},
		ContextProvider:     sessionService,
	})
	if err != nil {
		diagnostics.Errorf("create task start service failed: %v", err)
		_ = diagnostics.Close()
		closeStore()
		return nil, err
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
	taskService, err := applicationtask.NewService(startService, lifecycleService)
	if err != nil {
		_ = diagnostics.Close()
		closeStore()
		return nil, err
	}
	factory := runtimeFactory{
		executions: executions,
		runLoop:    timedLoop(runLoop, timings),
		messageRecorders: func(ctx context.Context, sessionID contracts.SessionID, agentID contracts.AgentID) (agentruntime.MessageRecorder, error) {
			return messages.Resolve(ctx, sessionID, agentID)
		},
		taskBuilder: startService,
		lifecycle:   taskService,
		tasks:       repos.Tasks,
		logger:      diagnostics,
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
	})
	if err != nil {
		_ = registry.Close(context.Background())
		_ = diagnostics.Close()
		closeStore()
		return nil, err
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
			// 与 Task 准入共用事务边界，避免后台队列读到混合版本的模型、定义和权限。
			return store.InTx(ctx, func(context.Context) error {
				modelRegistry.ReplaceFrom(candidateModels)
				agentRegistry.ReplaceFrom(candidateAgents)
				startService.UpdateToolPermissions(candidatePermissions)
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
