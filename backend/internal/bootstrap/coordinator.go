package bootstrap

import (
	"praxis/internal/clock"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/hook"
	"praxis/internal/core/model"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	toolinfra "praxis/internal/infra/tools"
	"praxis/internal/logging"
	"praxis/internal/loop"
	"praxis/internal/repository"
	execution "praxis/internal/service/agent/coordinator"
	executionservice "praxis/internal/service/agent/execution"
	toolservice "praxis/internal/service/toolexecution"
	"sync"
)

// ModelSource 提供模型选择和冻结配置对应的请求流，不暴露 Provider 实现。
type ModelSource interface {
	model.ModelBuilder
	executionservice.ModelConfigResolver
}

// Dependencies 是进程级执行协调器的装配依赖；Task 用例和队列由 task/service 提供。
type coordinatorDependencies struct {
	Transactions   repository.Transaction
	Tasks          execution.ExecutionOperations
	Records        repository.AgentExecution
	Queue          execution.TaskQueue
	Recovery       execution.RecoverySource
	ActiveTasks    *execution.ActiveTasks
	ToolExecutions repository.ToolExecution
	Messages       repository.MessageStreams
	Sessions       repository.Session
	Workspaces     repository.Workspace
	Writers        repository.MessageWriterProvider
	Models         ModelSource
	Tools          tool.ToolCatalog
	Rules          toolservice.ApprovalRules
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	ConfigMu       *sync.RWMutex
	Events         *execution.EventPublisher
	ObserveUsage   func(model.ModelUsageRecord)
	SummaryFormat  contextcompress.SummaryFormat
	Runner         *loop.Runner
	Clock          clock.Clock
}

func assembleCoordinator(deps coordinatorDependencies) (*execution.Coordinator, error) {
	runner := deps.Runner
	observe := func(event loop.Event) {
		if deps.ObserveUsage != nil && event.Kind == loop.EventModelUsage && event.Usage != nil {
			deps.ObserveUsage(model.ModelUsageRecord{
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
		Models:   deps.Models,
		Messages: deps.Messages,
		Hooks:    deps.HookDispatcher,
		ObserveUsage: func(record model.ModelUsageRecord) {
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
		Transactions:   deps.Transactions,
		HookDispatcher: deps.HookDispatcher,
		Rules:          deps.Rules,
		Review:         reviewer.Review,
		Tasks:          deps.Records,
		Events:         deps.ToolExecutions,
		Messages:       deps.Messages,
		Prepare:        toolinfra.Prepare,
		Bind:           toolinfra.Bind,
		Catalog:        deps.Tools,
		Clock:          deps.Clock,
	})
	if err != nil {
		return nil, err
	}

	if runner == nil {
		built, err := loop.NewRunner(loop.Dependencies{
			ModelBuilder:   deps.Models,
			SummaryFormat:  deps.SummaryFormat,
			Tools:          deps.Tools,
			HookDispatcher: deps.HookDispatcher,
			EventObserver:  observe,
			Logf:           logging.NewFactory().Ensure(deps.Logger).Infof,
		})
		if err != nil {
			return nil, err
		}
		runner = &built
	}
	return execution.NewCoordinator(execution.Config{
		Loop:           runner,
		ActiveTasks:    deps.ActiveTasks,
		Operations:     deps.Tasks,
		Queue:          deps.Queue,
		Messages:       deps.Writers,
		HookDispatcher: deps.HookDispatcher,
		Logger:         deps.Logger,
		ConfigMu:       deps.ConfigMu,
		Recovery:       deps.Recovery,
		Tools:          executor,
		Sessions:       deps.Sessions,
		Workspaces:     deps.Workspaces,
	})
}
