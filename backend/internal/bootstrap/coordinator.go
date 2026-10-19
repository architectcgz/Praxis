package bootstrap

import (
	execution "praxis/internal/service/agent/coordinator"

	toolservice "praxis/internal/service/toolexecution"
	"sync"

	"praxis/internal/clock"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/core/model"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"praxis/internal/logging"
	inputservice "praxis/internal/service/agent/input"
	"praxis/internal/transaction"
)

// ModelSource 提供模型选择和冻结配置对应的请求流，不暴露 Provider 实现。
type ModelSource interface {
	model.ModelBuilder
	inputservice.ModelConfigResolver
}

// Dependencies 是进程级执行协调器的装配依赖；Task 用例和队列由 task/service 提供。
type coordinatorDependencies struct {
	Transactions   transaction.Runner
	Tasks          execution.ExecutionOperations
	Queue          execution.TaskQueue
	Recovery       execution.RecoverySource
	ActiveTasks    *execution.ActiveTasks
	ToolExecutions toolexecution.Store
	Messages       messagemodel.Reader
	Writers        toolexecution.WriterProvider
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
	IDs            identity.IDGenerator
}

func assembleCoordinator(deps coordinatorDependencies) (*execution.Coordinator, error) {
	runner := deps.Runner
	if runner == nil {
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
		reviewer := execution.RiskReviewer{
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
		executor, err := toolservice.NewExecutor(toolservice.Dependencies{
			Transactions:   deps.Transactions,
			HookDispatcher: deps.HookDispatcher,
			Rules:          deps.Rules,
			Review:         reviewer.Review,
			Tasks:          deps.Tasks,
			Executions:     deps.ToolExecutions,
			Catalog:        deps.Tools,
			Clock:          deps.Clock,
		})
		if err != nil {
			return nil, err
		}
		built, err := loop.NewRunner(loop.Dependencies{
			ModelBuilder:   deps.Models,
			Executions:     deps.ToolExecutions,
			SummaryFormat:  deps.SummaryFormat,
			ToolExecutor:   executor,
			Tools:          deps.Tools,
			HookDispatcher: deps.HookDispatcher,
			EventObserver:  observe,
			Logf:           logging.NewFactory().Ensure(deps.Logger).Infof,
			Clock:          deps.Clock,
			IDs:            deps.IDs,
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
		Approvals: execution.ToolApprovals{
			DecideOperation: deps.Rules.Decide,
			Transactions:    deps.Transactions,
			Tasks:           deps.Tasks,
			Executions:      deps.ToolExecutions,
		},
	})
}
