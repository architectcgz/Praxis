package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"praxis/internal/config"
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
)

func buildCoordinator(resources storageResources, loadConfig func(context.Context) (*session.Configuration, error), hooks *hook.Dispatcher, emitter *event.Emitter, customRunner *loop.Runner, runtime config.Runtime) *coordinator.Coordinator {
	runDeps := run.Dependencies{
		HookDispatcher: hooks,
		Logger:         resources.logger,
		Emitter:        emitter,
		PrepareTimeout: runtime.Lifecycle.Preparation,
		CommitTimeout:  runtime.Lifecycle.Commit,
		SettleTimeout:  runtime.Lifecycle.Settlement,
	}
	modelCaller := modelcall.NewCaller(modelcall.Dependencies{
		HookDispatcher: hooks,
		Emitter:        emitter,
		Logf:           resources.logger.Infof,
		MaxAttempts:    runtime.Model.MaxAttempts,
		RequestTimeout: runtime.Model.RequestTimeout,
		CommitTimeout:  runtime.Lifecycle.Commit,
	})
	approvals := approval.NewGate(approval.GateDependencies{
		ModelCaller:    modelCaller,
		HookDispatcher: hooks,
		Emitter:        emitter,
		Logf:           resources.logger.Errorf,
		CommitTimeout:  runtime.Lifecycle.Commit,
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
	runnerDeps.CommitTimeout = runtime.Lifecycle.Commit
	runnerDeps.ReadOnlyParallelism = runtime.Execution.ReadOnlyParallelism
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
		TransitionTimeout: runtime.Lifecycle.Transition,
		ShutdownTimeout:   runtime.Lifecycle.Shutdown,
		ActiveSubagents:   runtime.Execution.ActiveSubagents,
	})
}
