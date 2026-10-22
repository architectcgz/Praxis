package bootstrap

import (
	"praxis/internal/clock"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	taskmodel "praxis/internal/core/session"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	toolinfra "praxis/internal/infra/tools"
	"praxis/internal/logging"
	"praxis/internal/loop"
	"praxis/internal/repository"
	execution "praxis/internal/service/agent/coordinator"
	toolservice "praxis/internal/service/toolexecution"
	"sync"
)

// coordinatorDependencies 装配业务用例、Core 内存队列与执行能力。
type coordinatorDependencies struct {
	Tasks          execution.ExecutionOperations
	Requests       loop.RequestSequencer
	ProgressHook   hook.ModelHook
	Records        repository.AgentExecution
	SessionTasks   execution.SessionTasks
	PrimaryAgent   execution.PrimaryAgentProvider
	SessionModels  execution.SessionModels
	Recovery       repository.AgentExecution
	ActiveTasks    *taskmodel.ActiveTasks
	ToolExecutions repository.ToolExecution
	Entries        repository.Entries
	ToolCalls      repository.ToolCallQueries
	Agents         repository.Agent
	Sessions       repository.Session
	Workspaces     repository.Workspace
	Provider       providerapi.Provider
	Tools          tool.ToolCatalog
	Rules          toolservice.ApprovalRules
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	ConfigMu       *sync.RWMutex
	Events         *execution.EventPublisher
	ObserveUsage   func(providerapi.UsageRecord)
	SummaryFormat  contextcompress.SummaryFormat
	Runner         *loop.Runner
	Clock          clock.Clock
}

func assembleCoordinator(deps coordinatorDependencies) (*execution.Coordinator, error) {
	observe := func(event loop.Event) {
		if deps.ObserveUsage != nil && event.Kind == loop.EventModelUsage && event.Usage != nil {
			deps.ObserveUsage(providerapi.UsageRecord{
				SessionID:   event.SessionID.String(),
				AgentID:     event.AgentID.String(),
				ExecutionID: event.ExecutionID.String(),
				TurnID:      event.TurnID,
				Usage:       *event.Usage,
			})
		}
		deps.Events.Publish(execution.LoopEvent(event))
	}
	reviewer := toolservice.RiskReviewer{
		Provider: deps.Provider,
		Entries:  deps.Entries,
		Agents:   deps.Agents,
		Hooks:    deps.HookDispatcher,
		ObserveUsage: func(record providerapi.UsageRecord) {
			observe(loop.Event{
				Kind:        loop.EventModelUsage,
				SessionID:   identity.SessionID(record.SessionID),
				AgentID:     identity.AgentID(record.AgentID),
				ExecutionID: identity.AgentExecutionID(record.ExecutionID),
				TurnID:      record.TurnID,
				Usage:       &record.Usage,
			})
		},
	}
	executor, err := toolservice.NewService(toolservice.Dependencies{
		HookDispatcher: deps.HookDispatcher,
		Rules:          deps.Rules,
		Review:         reviewer.Review,
		Tasks:          deps.Records,
		Events:         deps.ToolExecutions,
		Entries:        deps.Entries,
		Calls:          deps.ToolCalls,
		Sessions:       deps.Sessions,
		Agents:         deps.Agents,
		Prepare:        toolinfra.Prepare,
		Bind:           toolinfra.Bind,
		Catalog:        deps.Tools,
		Clock:          deps.Clock,
	})
	if err != nil {
		return nil, err
	}

	runnerDeps := loop.Dependencies{
		Provider:       deps.Provider,
		SummaryFormat:  deps.SummaryFormat,
		Tools:          deps.Tools,
		HookDispatcher: deps.HookDispatcher,
		EventObserver:  observe,
		Logf:           logging.NewFactory().Ensure(deps.Logger).Infof,
	}
	if deps.Runner != nil {
		runnerDeps = deps.Runner.Dependencies
	}
	// 执行序号和记录 Hook 始终绑定同一个业务服务；复制依赖，不改写外部共享 Runner。
	runnerDeps.Requests = deps.Requests
	if runnerDeps.HookDispatcher == nil {
		runnerDeps.HookDispatcher = deps.HookDispatcher
	}
	runnerDeps.HookDispatcher = runnerDeps.HookDispatcher.WithModelHooks(deps.ProgressHook)
	runner, err := loop.NewRunner(runnerDeps)
	if err != nil {
		return nil, err
	}
	return execution.NewCoordinator(execution.Config{
		Events:         deps.Events,
		Loop:           &runner,
		ActiveTasks:    deps.ActiveTasks,
		Operations:     deps.Tasks,
		SessionTasks:   deps.SessionTasks,
		HookDispatcher: deps.HookDispatcher,
		Logger:         deps.Logger,
		ConfigMu:       deps.ConfigMu,
		Recovery:       deps.Recovery,
		Tools:          executor,
		Sessions:       deps.Sessions,
		Workspaces:     deps.Workspaces,
		PrimaryAgent:   deps.PrimaryAgent,
		SessionModels:  deps.SessionModels,
	})
}
