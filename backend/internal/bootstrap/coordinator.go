package bootstrap

import (
	"context"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/agent/execution/timing"
	contextcompress "praxis/internal/core/context/compress"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	toolinfra "praxis/internal/infra/tools"
	"praxis/internal/logging"
	"praxis/internal/repository"
	execution "praxis/internal/service/coordinator"
	toolservice "praxis/internal/service/toolexecution"
	"sync"
	"time"
)

// coordinatorDependencies 装配业务用例、Core 内存队列与执行能力。
type coordinatorDependencies struct {
	TimingRecorder *timing.Recorder
	Tasks          execution.ExecutionOperations
	Progress       loop.ExecutionProgress
	Records        repository.AgentExecution
	SessionTasks   execution.SessionTasks
	MainAgent      func(context.Context, identity.SessionID) (*agentmodel.Agent, error)
	SessionModels  execution.SessionModels
	Recovery       repository.AgentExecution
	ToolExecutions repository.ToolExecution
	Entries        repository.Entries
	ToolCalls      repository.ToolCallQueries
	Agent          func(identity.AgentID) (*agentmodel.Agent, error)
	Sessions       repository.Session
	Workspaces     repository.Workspace
	Provider       providerapi.Provider
	Tools          tool.ToolCatalog
	Rules          toolservice.ApprovalRules
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	ConfigMu       *sync.RWMutex
	Emitter        *coreevent.Emitter
	ObserveUsage   func(providerapi.UsageRecord)
	SummaryFormat  contextcompress.SummaryFormat
	Runner         *loop.Runner
	Clock          clock.Clock
}

func assembleCoordinator(deps coordinatorDependencies) *execution.Coordinator {
	if deps.ObserveUsage != nil {
		// 先登记用量保存，再接入前端订阅，确保通知时用量已完成记录。
		deps.Emitter.Subscribe(func(value coreevent.Event) {
			if value.Kind == coreevent.ModelUsage && value.Usage != nil {
				deps.ObserveUsage(providerapi.UsageRecord{
					SessionID:   value.SessionID.String(),
					AgentID:     value.AgentID.String(),
					ExecutionID: value.ExecutionID.String(),
					TurnID:      value.TurnID,
					Usage:       *value.Usage,
				})
			}
		})
	}
	reviewer := &executionmodel.Reviewer{
		Provider:       deps.Provider,
		TimingRecorder: deps.TimingRecorder,
		HookDispatcher: deps.HookDispatcher,
		Emitter:        deps.Emitter,
	}
	records := toolservice.NewService(toolservice.Dependencies{
		Rules:    deps.Rules,
		Tasks:    deps.Records,
		Events:   deps.ToolExecutions,
		Entries:  deps.Entries,
		Calls:    deps.ToolCalls,
		Sessions: deps.Sessions,
		Clock:    deps.Clock,
	})

	executor := executionmodel.NewExecutor(executionmodel.ExecutorDependencies{
		Catalog:        deps.Tools,
		Operations:     records,
		Reviewer:       reviewer,
		Prepare:        toolinfra.Prepare,
		Bind:           toolinfra.Bind,
		HookDispatcher: deps.HookDispatcher,
		TimingRecorder: deps.TimingRecorder,
		Emitter:        deps.Emitter,
		Clock:          deps.Clock,
		Logf:           deps.Logger.Errorf,
	})

	runnerDeps := loop.Dependencies{
		Provider:       deps.Provider,
		SummaryFormat:  deps.SummaryFormat,
		Tools:          deps.Tools,
		HookDispatcher: deps.HookDispatcher,
		Emitter:        deps.Emitter,
		Logf:           deps.Logger.Infof,
	}
	if deps.Runner != nil {
		runnerDeps = deps.Runner.Dependencies
	}
	// 可选执行器的默认依赖统一在装配入口确定，业务构造器不再补齐。
	if runnerDeps.Logf == nil {
		runnerDeps.Logf = func(string, ...any) {}
	}
	if runnerDeps.CompressionStrategy == nil {
		runnerDeps.CompressionStrategy = contextcompress.FullStrategy{}
	}
	// 执行进度始终绑定同一个业务服务；复制依赖，不改写外部共享 Runner。
	runnerDeps.Emitter = deps.Emitter
	runnerDeps.Progress = deps.Progress
	runnerDeps.TimingRecorder = deps.TimingRecorder
	runnerDeps.HookDispatcher = deps.HookDispatcher
	runner := loop.NewRunner(runnerDeps)
	return execution.NewCoordinator(execution.Config{
		Loop:              &runner,
		Operations:        deps.Tasks,
		SessionTasks:      deps.SessionTasks,
		HookDispatcher:    deps.HookDispatcher,
		Logger:            deps.Logger,
		ConfigMu:          deps.ConfigMu,
		Recovery:          deps.Recovery,
		Tools:             executor,
		ToolRecords:       records,
		Sessions:          deps.Sessions,
		Workspaces:        deps.Workspaces,
		MainAgent:         deps.MainAgent,
		Agent:             deps.Agent,
		SessionModels:     deps.SessionModels,
		TransitionTimeout: 30 * time.Second,
	})
}
