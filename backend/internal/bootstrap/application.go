package bootstrap

import (
	"context"
	"errors"
	"sync"

	"praxis/internal/agent"
	"praxis/internal/dataroot"
	"praxis/internal/execution"
	"praxis/internal/execution/loop"
	"praxis/internal/hook"
	"praxis/internal/logging"
	"praxis/internal/project"
	"praxis/internal/session"
	"praxis/internal/timing"
	"praxis/internal/tool"
	"praxis/internal/workspace/fs"
	"praxis/wails/bindings"
)

// Application 拥有进程资源；业务状态与执行生命周期分别由业务能力和 Runtime 管理。
type Application struct {
	storageResources
	frontend bindings.Services
	sessions *session.Service
	runtime  *execution.Runtime
}

func (a *Application) Services() bindings.Services {
	if a == nil {
		return bindings.Services{}
	}
	return a.frontend
}

// Open 只做高层装配；恢复完成前不把业务入口交给桌面宿主。
func Open(ctx context.Context, root dataroot.DataRoot, runner *loop.Runner, extensions ...tool.Tool) (*Application, error) {
	if ctx == nil {
		return nil, errors.New("application composition context is required")
	}
	if err := root.Initialize(ctx); err != nil {
		return nil, err
	}
	if err := initializeDefaultFiles(ctx, root); err != nil {
		return nil, err
	}
	resources, err := openStorage(ctx, root)
	if err != nil {
		return nil, err
	}
	a := &Application{storageResources: resources}
	ready := false
	defer func() {
		if ready {
			return
		}
		// 装配失败也要先停止可能已恢复的执行，再关闭其依赖的日志资源。
		if a.runtime != nil {
			_ = a.runtime.Close(context.Background())
		}
		_ = a.store.Close(context.Background())
		_ = a.logger.Close()
	}()
	config, err := loadConfiguration(root, extensions)
	if err != nil {
		return nil, err
	}
	repos := a.store.Repositories()
	events := execution.NewEventPublisher()
	timings, err := buildTimings(ctx, a.store, events, a.logger)
	if err != nil {
		return nil, err
	}
	hooks := hook.NewDispatcher(timing.Hook{Recorder: timings})
	configMu := &sync.RWMutex{}
	a.sessions, err = buildSessions(resources, repos, config.agents, hooks, configMu)
	if err != nil {
		return nil, err
	}
	a.runtime, err = buildExecution(resources, repos, config, a.sessions, hooks, events, configMu, runner)
	if err != nil {
		return nil, err
	}
	agents, err := agent.NewService(agent.Dependencies{
		Transactions: a.store,
		Agents:       repos.Agents,
		Tasks:        repos.Executions,
		Controls:     repos.Controls,
		Executions:   a.runtime,
		Messages:     repos.Messages,
	})
	if err != nil {
		return nil, err
	}
	projects, err := project.NewService(project.Dependencies{
		GlobalApprovalMode: config.globalApprovalMode,
		Transactions:       a.store,
		Projects:           repos.Projects,
		Workspaces:         repos.Workspaces,
		Directory:          fs.Filesystem{},
		Logger:             a.logger,
	})
	if err != nil {
		return nil, err
	}
	models := buildModelService(root, config, a.store, a.logger, configMu)
	a.frontend = bindings.Services{
		Projects:       projects,
		Sessions:       a.sessions,
		Agents:         agents,
		Commands:       a.runtime,
		Models:         models,
		ModelConfig:    models,
		Events:         events,
		Timings:        timings,
		Usages:         a.sessions,
		SessionLogPath: a.store.SessionLogPath,
	}
	if err := a.runtime.Restore(ctx); err != nil {
		return nil, err
	}
	a.logger.Infof("application open completed ready=true")
	ready = true
	return a, nil
}

func (a *Application) RuntimeLogger() *logging.Logger {
	factory := logging.NewFactory()
	if a == nil || a.logger == nil {
		return factory.Nop()
	}
	return factory.Ensure(a.logger)
}

// Close 只有在全部执行退出后才关闭存储；超时保留资源供后续关闭重试。
func (a *Application) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("application close context is required")
	}
	logger := a.RuntimeLogger()
	logger.Infof("application close started")
	if err := a.runtime.Close(ctx); err != nil {
		logger.Errorf("execution close incomplete: %v", err)
		return err
	}
	sessionErr := a.sessions.CleanupEmptySessions(ctx)
	storeErr := a.store.Close(ctx)
	if sessionErr != nil || storeErr != nil {
		logger.Errorf("application close encountered errors sessions=%v store=%v", sessionErr, storeErr)
	} else {
		logger.Infof("application close completed")
	}
	return errors.Join(sessionErr, storeErr, logger.Close())
}
