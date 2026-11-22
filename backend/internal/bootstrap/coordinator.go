package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"praxis/internal/engine/approval"
	"praxis/internal/engine/coordinator"
	"praxis/internal/engine/hook"
	"praxis/internal/engine/loop"
	"praxis/internal/engine/modelcall"
	"praxis/internal/engine/run"
	"praxis/internal/engine/session"
	"praxis/internal/engine/toolexecution"
	"praxis/internal/event"
	"praxis/internal/tool/output"
	"time"
)

func buildCoordinator(resources storageResources, loadConfig func(context.Context) (*session.Configuration, error), hooks *hook.Dispatcher, emitter *event.Emitter, customRunner *loop.Runner) *coordinator.Coordinator {
	runDeps := run.Dependencies{
		HookDispatcher: hooks,
		Logger:         resources.logger,
		Emitter:        emitter,
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
		Logf:           resources.logger.Errorf,
	})
	executor := toolexecution.NewExecutor(toolexecution.ExecutorDependencies{
		HookDispatcher: hooks,
		Emitter:        emitter,
		Outputs:        output.Files{Root: filepath.Join(os.TempDir(), "praxis", "tool-output")},
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
		Emitter:           emitter,
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
