package bootstrap

import (
	"os"
	"path/filepath"
	"praxis/internal/clock"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	providerapi "praxis/internal/core/provider"
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
		UsageRecords:   sessions,
		Clock:          clock.UTCClock{},
		IDs:            identity.SecureIDGenerator{},
	})
	reviewer := &executionmodel.Reviewer{
		Progress:       tasks,
		HookDispatcher: hooks,
		Emitter:        emitter,
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
	// 先登记用量保存，再接入前端订阅；只有当前主执行的用量写入会话。
	emitter.Subscribe(func(value coreevent.Event) {
		if payload, ok := value.Payload.(coreevent.ModelUsage); ok {
			run := coordinator.ActiveRun(identity.AgentID(value.Source.SessionID), value.Source.BranchID)
			if run == nil {
				return
			}
			record := run.Snapshot()
			if record.ID != value.Source.ExecutionID {
				return
			}
			if err := tasks.RecordUsage(providerapi.UsageRecord{
				SessionID:   record.SessionID.String(),
				AgentID:     record.AgentID.String(),
				ExecutionID: record.ID.String(),
				TurnID:      value.Source.TurnID,
				Usage:       payload.Usage,
			}); err != nil {
				emitter.Emit(coreevent.Event{
					Source:  value.Source,
					Payload: coreevent.ErrorOccurred{Message: "Failed to save model usage: " + err.Error()},
				})
			}
		}
	})
	return coordinator
}
