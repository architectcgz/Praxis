package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"praxis/internal/clock"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/approval"
	"praxis/internal/core/coordinator"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/core/modelcall"
	sessionmodel "praxis/internal/core/session"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	tooloutput "praxis/internal/infra/tools/output"
	"time"
)

func buildCoordinator(resources storageResources, loadConfig func(context.Context) (*sessionmodel.Configuration, error), hooks *hook.Dispatcher, emitter *coreevent.Emitter, customRunner *loop.Runner) *coordinator.Coordinator {
	runDeps := agentrun.Dependencies{
		HookDispatcher: hooks,
		Logger:         resources.logger,
		Emitter:        emitter,
		Clock:          clock.UTCClock{},
		IDs:            identity.SecureIDGenerator{},
	}
	modelCaller := modelcall.NewCaller(modelcall.Dependencies{
		HookDispatcher: hooks,
		Emitter:        emitter,
		Logf:           resources.logger.Infof,
	})
	approvals := approval.NewGate(approval.GateDependencies{
		ModelCaller:    modelCaller,
		HookDispatcher: hooks,
		Emitter:        emitter,
		Clock:          clock.UTCClock{},
		Logf:           resources.logger.Errorf,
	})
	executor := executionmodel.NewExecutor(executionmodel.ExecutorDependencies{
		HookDispatcher: hooks,
		Emitter:        emitter,
		Outputs:        tooloutput.Files{Root: filepath.Join(os.TempDir(), "praxis", "tool-output")},
	})

	runnerDeps := loop.Dependencies{
		ModelCaller:    modelCaller,
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
	// 执行状态由所属 Run 按次维护；复制共享依赖，不改写外部 Runner。
	runnerDeps.ModelCaller = modelCaller
	runnerDeps.Emitter = emitter
	runnerDeps.HookDispatcher = hooks
	runner := loop.NewRunner(runnerDeps)
	return coordinator.NewCoordinator(coordinator.Config{
		Loop:              &runner,
		RunDependencies:   runDeps,
		LoadConfig:        loadConfig,
		HookDispatcher:    hooks,
		Logger:            resources.logger,
		Tools:             executor,
		Approvals:         approvals,
		SessionRecords:    resources.storage.Sessions,
		SessionLoader:     resources.storage.Sessions,
		Projects:          resources.storage.Projects,
		TransitionTimeout: 30 * time.Second,
	})
}
