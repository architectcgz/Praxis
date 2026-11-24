package bootstrap

import (
	"context"
	"praxis/internal/config"
	appcontext "praxis/internal/context"
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
	subagenttool "praxis/internal/tool/subagent"
)

func buildCoordinator(resources storageResources, loadConfig func(context.Context) (*session.Configuration, error), hooks *hook.Dispatcher, emitter *event.Emitter, customRunner *loop.Runner, toolOutputDir string, runtime config.Runtime) *coordinator.Coordinator {
	runDeps := run.Dependencies{
		HookDispatcher: hooks,
		Logger:         resources.logger,
		Emitter:        emitter,
		PrepareTimeout: runtime.Lifecycle.Preparation,
		CommitTimeout:  runtime.Lifecycle.Commit,
		SettleTimeout:  runtime.Lifecycle.Settlement,
	}
	budgetPolicy := appcontext.BudgetPolicy{
		MinMarginTokens:      runtime.Context.MinMarginTokens,
		MarginPercent:        runtime.Context.MarginPercent,
		RequestReserveTokens: runtime.Context.RequestReserveTokens,
	}
	modelCaller := modelcall.NewCaller(modelcall.Dependencies{
		HookDispatcher: hooks,
		Emitter:        emitter,
		Logf:           resources.logger.Infof,
		MaxAttempts:    runtime.Model.MaxAttempts,
		RequestTimeout: runtime.Model.RequestTimeout,
		CommitTimeout:  runtime.Lifecycle.Commit,
		BudgetPolicy:   budgetPolicy,
	})
	approvals := approval.NewGate(approval.GateDependencies{
		ModelCaller:    modelCaller,
		HookDispatcher: hooks,
		Emitter:        emitter,
		Logf:           resources.logger.Errorf,
		CommitTimeout:  runtime.Lifecycle.Commit,
		ReviewPolicy: approval.ReviewPolicy{
			Timeout:           runtime.Review.Timeout,
			MaxTextBytes:      runtime.Review.MaxTextBytes,
			TargetReasonChars: runtime.Review.TargetReasonChars,
			MaxReasonBytes:    runtime.Review.MaxReasonBytes,
		},
	})
	executor := toolexecution.NewExecutor(toolexecution.ExecutorDependencies{
		HookDispatcher:    hooks,
		Emitter:           emitter,
		Outputs:           output.Files{Root: toolOutputDir},
		InlineResultBytes: runtime.Tools.InlineResultBytes,
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
	runnerDeps.CompressionPolicy = appcontext.CompressionPolicy{
		BudgetPolicy:         budgetPolicy,
		NearLimitPercent:     runtime.Context.NearLimitPercent,
		SummaryTargetPercent: runtime.Context.SummaryTargetPercent,
		MaxSummaryTokens:     runtime.Context.MaxSummaryTokens,
	}
	runnerDeps.SummaryTimeout = runtime.Context.SummaryTimeout
	runnerDeps.ToolResultOverhead = runtime.Context.ToolResultOverhead
	runnerDeps.KeepToolResultTokens = runtime.Context.KeepToolResultTokens
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
		SubagentLimits: subagenttool.TimeoutLimits{
			MinTimeoutSeconds: runtime.Tools.Subagent.MinTimeoutSeconds,
			MaxTimeoutSeconds: runtime.Tools.Subagent.MaxTimeoutSeconds,
		},
	})
}
