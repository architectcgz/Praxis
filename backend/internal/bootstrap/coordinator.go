package bootstrap

import (
	"context"
	"net/http"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	contextcompress "praxis/internal/core/context/compress"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	providerapi "praxis/internal/core/provider"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"praxis/internal/infra/providers"
	"praxis/internal/infra/storage"
	executionservice "praxis/internal/service/agent/execution"
	execution "praxis/internal/service/coordinator"
	sessionservice "praxis/internal/service/session"
	toolservice "praxis/internal/service/toolexecution"
	"sync"
	"time"
)

func buildCoordinator(resources storageResources, repos storage.Repositories, config *configuration, sessions *sessionservice.Service, mainAgent func(context.Context, identity.SessionID) (*agentmodel.Agent, error), hooks *hook.Dispatcher, emitter *coreevent.Emitter, configMu *sync.RWMutex, customRunner *loop.Runner) *execution.Coordinator {
	tasks := executionservice.NewService(executionservice.Dependencies{
		HookDispatcher: hooks,
		Logger:         resources.logger,
		Emitter:        emitter,
		Sessions:       repos.Sessions,
		Agent:          sessions.GetAgent,
		Records:        repos.Executions,
		Entries:        repos.Entries,
		Definitions:    config.agents,
		Clock:          clock.UTCClock{},
		IDs:            identity.SecureIDGenerator{},
	})
	provider := providers.New(providers.Dependencies{
		Credentials: config.models,
		HTTPClient:  http.DefaultClient,
	})

	observeUsage := usageObserver(repos.ModelUsage, resources.logger)
	// 先登记用量保存，再接入前端订阅，确保通知时用量已完成记录。
	emitter.Subscribe(func(value coreevent.Event) {
		if value.Kind == coreevent.ModelUsage && value.Usage != nil {
			owner, err := sessions.GetAgent(value.AgentID)
			if err != nil || !owner.IsMain() || value.Transient {
				return
			}
			observeUsage(providerapi.UsageRecord{
				SessionID:   value.SessionID.String(),
				AgentID:     value.AgentID.String(),
				ExecutionID: value.ExecutionID.String(),
				TurnID:      value.TurnID,
				Usage:       *value.Usage,
			})
		}
	})

	reviewer := &executionmodel.Reviewer{
		Progress:       tasks,
		Provider:       provider,
		HookDispatcher: hooks,
		Emitter:        emitter,
	}
	records := toolservice.NewService(toolservice.Dependencies{
		ApprovalConfig: config,
		Tasks:          repos.Executions,
		Entries:        repos.Entries,
		Calls:          repos.ToolCalls,
		Sessions:       repos.Sessions,
		Projects:       repos.Projects,
		Clock:          clock.UTCClock{},
	})
	executor := executionmodel.NewExecutor(executionmodel.ExecutorDependencies{
		Catalog:        config.tools,
		Operations:     records,
		Reviewer:       reviewer,
		HookDispatcher: hooks,
		Emitter:        emitter,
		Clock:          clock.UTCClock{},
		Logf:           resources.logger.Errorf,
	})

	runnerDeps := loop.Dependencies{
		Provider:       provider,
		SummaryFormat:  defaultSummaryFormat(),
		Tools:          config.tools,
		HookDispatcher: hooks,
		Emitter:        emitter,
		Logf:           resources.logger.Infof,
	}
	if customRunner != nil {
		runnerDeps = customRunner.Dependencies
	}
	// 可选执行器的默认依赖统一在装配入口确定，业务构造器不再补齐。
	if runnerDeps.Logf == nil {
		runnerDeps.Logf = func(string, ...any) {}
	}
	if runnerDeps.CompressionStrategy == nil {
		runnerDeps.CompressionStrategy = contextcompress.FullStrategy{}
	}
	// 执行进度始终绑定同一个业务服务；复制依赖，不改写外部共享 Runner。
	runnerDeps.Emitter = emitter
	runnerDeps.Progress = tasks
	runnerDeps.Entries = repos.Entries
	runnerDeps.HookDispatcher = hooks
	runner := loop.NewRunner(runnerDeps)
	return execution.NewCoordinator(execution.Config{
		Loop:              &runner,
		Operations:        tasks,
		Sessions:          sessions,
		HookDispatcher:    hooks,
		Logger:            resources.logger,
		ConfigMu:          configMu,
		Recovery:          repos.Executions,
		Tools:             executor,
		ToolRecords:       records,
		Entries:           repos.Entries,
		SessionRecords:    repos.Sessions,
		Workspaces:        repos.Workspaces,
		MainAgent:         mainAgent,
		TransitionTimeout: 30 * time.Second,
	})
}
