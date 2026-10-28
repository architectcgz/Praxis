package bootstrap

import (
	"net/http"
	"praxis/internal/clock"
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

func buildCoordinator(resources storageResources, repos storage.Repositories, config *configuration, sessions *sessionservice.Service, hooks *hook.Dispatcher, emitter *coreevent.Emitter, configMu *sync.RWMutex, customRunner *loop.Runner) *execution.Coordinator {
	tasks := executionservice.NewService(executionservice.Dependencies{
		HookDispatcher: hooks,
		Logger:         resources.logger,
		Emitter:        emitter,
		Sessions:       repos.Sessions,
		Records:        repos.Executions,
		UsageRecords:   repos.ModelUsage,
		History:        sessions,
		Definitions:    config.agents,
		Clock:          clock.UTCClock{},
		IDs:            identity.SecureIDGenerator{},
	})
	provider := providers.New(providers.Dependencies{
		Credentials: config.models,
		HTTPClient:  http.DefaultClient,
	})

	// 先登记用量保存，再接入前端订阅，确保通知时用量已完成记录。
	emitter.Subscribe(func(value coreevent.Event) {
		if payload, ok := value.Payload.(coreevent.ModelUsage); ok {
			if value.Scope.AgentID != identity.AgentID(value.Scope.SessionID) {
				return
			}
			tasks.RecordUsage(providerapi.UsageRecord{
				SessionID:   value.Scope.SessionID.String(),
				AgentID:     value.Scope.AgentID.String(),
				ExecutionID: value.Scope.ExecutionID.String(),
				TurnID:      value.Scope.TurnID,
				Usage:       payload.Usage,
			})
		}
	})

	reviewer := &executionmodel.Reviewer{
		Progress:       tasks,
		Provider:       provider,
		HookDispatcher: hooks,
		Emitter:        emitter,
	}
	operations := toolservice.NewService(toolservice.Dependencies{
		ApprovalConfig: config,
		Sessions:       repos.Sessions,
	})
	executor := executionmodel.NewExecutor(executionmodel.ExecutorDependencies{
		Catalog:        config.tools,
		Operations:     operations,
		Reviewer:       reviewer,
		HookDispatcher: hooks,
		Emitter:        emitter,
		Clock:          clock.UTCClock{},
		Logf:           resources.logger.Errorf,
	})

	runnerDeps := loop.Dependencies{
		Provider:       provider,
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
	// 执行状态由业务服务按次绑定；复制共享依赖，不改写外部 Runner。
	runnerDeps.Emitter = emitter
	runnerDeps.HookDispatcher = hooks
	runnerDeps.SummaryFormat = defaultSummaryFormat()
	runner := loop.NewRunner(runnerDeps)
	return execution.NewCoordinator(execution.Config{
		Loop:              &runner,
		Operations:        tasks,
		Sessions:          sessions,
		HookDispatcher:    hooks,
		Logger:            resources.logger,
		ConfigMu:          configMu,
		Tools:             executor,
		SessionRecords:    repos.Sessions,
		Workspaces:        repos.Workspaces,
		TransitionTimeout: 30 * time.Second,
	})
}
