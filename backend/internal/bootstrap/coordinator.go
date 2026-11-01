package bootstrap

import (
	"os"
	"path/filepath"
	"praxis/internal/clock"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	tooloutput "praxis/internal/infra/tools/output"
	executionservice "praxis/internal/service/agent/execution"
	execution "praxis/internal/service/coordinator"
	sessionservice "praxis/internal/service/session"
	"time"
)

func buildCoordinator(resources storageResources, sessions *sessionservice.Service, hooks *hook.Dispatcher, emitter *coreevent.Emitter, customRunner *loop.Runner) *execution.Coordinator {
	tasks := executionservice.NewService(executionservice.Dependencies{
		HookDispatcher: hooks,
		Logger:         resources.logger,
		Emitter:        emitter,
		Sessions:       resources.storage.Sessions,
		Records:        sessions,
		Clock:          clock.UTCClock{},
		IDs:            identity.SecureIDGenerator{},
	})
	reviewer := &executionmodel.Reviewer{
		Progress:       tasks,
		HookDispatcher: hooks,
	}
	executor := executionmodel.NewExecutor(executionmodel.ExecutorDependencies{
		Reviewer:       reviewer,
		HookDispatcher: hooks,
		Emitter:        emitter,
		Clock:          clock.UTCClock{},
		Logf:           resources.logger.Errorf,
		Outputs:        tooloutput.Files{Root: filepath.Join(os.TempDir(), "praxis", "tool-output")},
	})

	runnerDeps := loop.Dependencies{
		HookDispatcher: hooks,
		Emitter:        emitter,
		Logf:           resources.logger.Infof,
	}
	if customRunner != nil {
		runnerDeps = customRunner.Dependencies
	}
	// 日志等共享能力在装配入口补齐，Provider 和工具目录在执行开始时绑定会话快照。
	if runnerDeps.Logf == nil {
		runnerDeps.Logf = func(string, ...any) {}
	}
	// 执行状态由业务服务按次绑定；复制共享依赖，不改写外部 Runner。
	runnerDeps.Emitter = emitter
	runnerDeps.HookDispatcher = hooks
	runner := loop.NewRunner(runnerDeps)
	coordinator := execution.NewCoordinator(execution.Config{
		Loop:              &runner,
		Operations:        tasks,
		Sessions:          sessions,
		HookDispatcher:    hooks,
		Logger:            resources.logger,
		Tools:             executor,
		SessionRecords:    resources.storage.Sessions,
		Projects:          resources.storage.Projects,
		TransitionTimeout: 30 * time.Second,
	})
	return coordinator
}
