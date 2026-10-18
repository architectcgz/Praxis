package execution

import (
	"context"
	"errors"
	"fmt"
	"sync"

	taskservice "praxis/internal/agent/input"
	"praxis/internal/clock"
	contextcompress "praxis/internal/context/compress"
	"praxis/internal/execution/loop"
	"praxis/internal/hook"
	"praxis/internal/identity"
	"praxis/internal/logging"
	messagemodel "praxis/internal/message"
	"praxis/internal/model"
	queueservice "praxis/internal/task/service"
	"praxis/internal/tool"
	"praxis/internal/toolcall"
	"praxis/internal/transaction"
)

// ModelSource 提供模型选择和冻结配置对应的请求流，不暴露 Provider 实现。
type ModelSource interface {
	model.ModelBuilder
	taskservice.ModelConfigResolver
}

// Dependencies 是进程级执行协调器的装配依赖；Task 用例和队列由 task/service 提供。
type Dependencies struct {
	Transactions   transaction.Runner
	Tasks          TaskOperations
	Queue          TaskQueue
	Progress       loop.TaskProgress
	Recovery       RecoverySource
	ActiveTasks    *ActiveTasks
	ToolExecutions toolcall.Store
	Messages       messagemodel.Reader
	Writers        toolcall.WriterProvider
	Models         ModelSource
	Tools          tool.ToolCatalog
	Rules          toolcall.ApprovalRules
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	ConfigMu       *sync.RWMutex
	Events         *EventPublisher
	ObserveUsage   func(model.ModelUsageRecord)
	SummaryFormat  contextcompress.SummaryFormat
	Runner         *loop.Runner
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Runtime 统一管理进程内执行、审批、恢复和关闭；Task 事实由 task/service 管理。
type Runtime struct {
	configMu    *sync.RWMutex
	tasks       TaskOperations
	queue       TaskQueue
	coordinator *Coordinator
	active      *ActiveTasks
	approvals   ToolApprovals
	recovery    RecoverySource
}

type StartResult struct {
	taskservice.SendInputResult
	ActivationError string
}

// NewRuntime 只装配进程级执行协调器，不创建 Task 服务或队列。
func NewRuntime(deps Dependencies) (*Runtime, error) {
	if deps.ConfigMu == nil {
		deps.ConfigMu = &sync.RWMutex{}
	}
	if deps.Events == nil {
		deps.Events = NewEventPublisher()
	}
	for name, value := range map[string]any{
		"transactions":    deps.Transactions,
		"tasks":           deps.Tasks,
		"progress":        deps.Progress,
		"recovery":        deps.Recovery,
		"active tasks":    deps.ActiveTasks,
		"tool executions": deps.ToolExecutions,
		"messages":        deps.Messages,
		"message writers": deps.Writers,
		"models":          deps.Models,
		"tools":           deps.Tools,
	} {
		if value == nil {
			return nil, fmt.Errorf("execution %s is required", name)
		}
	}
	if deps.Rules.Catalog == nil || deps.Rules.DefaultMode == nil {
		return nil, errors.New("execution approval rules are required")
	}

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
			deps.Events.Publish(loopEvent(event))
		}
		reviewer := RiskReviewer{
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
		executor, err := toolcall.NewExecutor(toolcall.Dependencies{
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
			Progress:       deps.Progress,
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
	coordinator, err := newCoordinator(coordinatorConfig{
		Loop:           runner,
		ActiveTasks:    deps.ActiveTasks,
		Tasks:          deps.Tasks,
		Queue:          deps.Queue,
		Messages:       deps.Writers,
		HookDispatcher: deps.HookDispatcher,
		Logger:         deps.Logger,
	})
	if err != nil {
		return nil, err
	}
	return &Runtime{
		configMu:    deps.ConfigMu,
		tasks:       deps.Tasks,
		queue:       deps.Queue,
		coordinator: coordinator,
		active:      deps.ActiveTasks,
		approvals: ToolApprovals{
			DecideOperation: deps.Rules.Decide,
			Transactions:    deps.Transactions,
			Tasks:           deps.Tasks,
			Executions:      deps.ToolExecutions,
		},
		recovery: deps.Recovery,
	}, nil
}

func (s *Runtime) SendInput(ctx context.Context, input taskservice.SendInput) (StartResult, error) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	result, err := s.tasks.SendInput(ctx, input)
	if err != nil {
		return StartResult{}, err
	}
	response := StartResult{SendInputResult: result}
	if result.ExistingRequest {
		return response, nil
	}
	ctx = context.WithoutCancel(ctx)
	if err := s.coordinator.Activate(ctx, result.Task); err != nil {
		response.ActivationError = err.Error()
		if abortErr := s.tasks.Abort(ctx, result.Task.ID); abortErr != nil {
			response.ActivationError = errors.Join(err, abortErr).Error()
		}
	}
	return response, nil
}

func (s *Runtime) EnqueueTask(ctx context.Context, input queueservice.EnqueueInput) (queueservice.EnqueueResult, error) {
	return s.queue.EnqueueTask(ctx, input)
}

// Restore 必须在外部准入开放前调用，先协调上次进程的未知副作用，再恢复任务与队列。
func (s *Runtime) Restore(ctx context.Context) error {
	if err := s.tasks.ReconcileToolResults(ctx); err != nil {
		return fmt.Errorf("reconcile tool results: %w", err)
	}
	if err := s.coordinator.Restore(ctx, s.recovery); err != nil {
		return fmt.Errorf("restore execution: %w", err)
	}
	return nil
}

func (s *Runtime) Pause(agentID identity.AgentID, executionID identity.AgentExecutionID) bool {
	return s.active.Pause(agentID, executionID)
}

func (s *Runtime) Stop(agentID identity.AgentID, executionID identity.AgentExecutionID) bool {
	return s.active.Stop(agentID, executionID)
}

// Close 等待全部执行与结算退出；超时后调用方必须保留存储资源。
func (s *Runtime) Close(ctx context.Context) error { return s.coordinator.Close(ctx) }
