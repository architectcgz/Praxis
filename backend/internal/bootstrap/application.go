package bootstrap

import (
	"context"
	"errors"
	"praxis/internal/clock"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/core/tool"
	"praxis/internal/dataroot"
	fs "praxis/internal/infra/filesystem"
	"praxis/internal/logging"
	agentservice "praxis/internal/service/agent"
	approvalservice "praxis/internal/service/approval"
	"praxis/internal/service/coordinator"
	projectservice "praxis/internal/service/project"
	sessionservice "praxis/internal/service/session"
	"praxis/wails/bindings"
)

// Application 拥有进程资源；业务状态与执行生命周期分别由业务能力和 Coordinator 管理。
type Application struct {
	storageResources
	frontend    bindings.Services
	sessions    *sessionservice.Service
	coordinator *coordinator.Coordinator
}

func (a *Application) Services() bindings.Services {
	if a == nil {
		return bindings.Services{}
	}
	return a.frontend
}

// Options 在初始化阶段注入执行器、工具和 Hook；运行期间不修改注册表。
type Options struct {
	Runner *loop.Runner
	Tools  []tool.Tool
	Hooks  []any
}

// Open 只做高层装配；会话 Context 按需恢复，启动不继续旧 Run。
func Open(ctx context.Context, root dataroot.DataRoot, options Options) (_ *Application, err error) {
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
		// 关闭日志前保存启动失败原因，不能只依赖桌面宿主的控制台输出。
		a.logger.Errorf("application open failed: %v", err)
		// 装配失败先关闭执行入口，再关闭存储与日志资源。
		if a.coordinator != nil {
			_ = a.coordinator.Close(context.Background())
		}
		_ = a.storage.Close(context.Background())
		_ = a.logger.Close()
	}()
	config, err := loadConfiguration(root, options.Tools)
	if err != nil {
		return nil, err
	}
	emitter := coreevent.NewEmitter()
	hooks, err := hook.NewDispatcher(options.Hooks...)
	if err != nil {
		return nil, err
	}
	a.sessions = buildSessions(a.storage, hooks, config, root)
	coordinator := buildCoordinator(resources, a.sessions, hooks, emitter, options.Runner)
	a.coordinator = coordinator
	agents := agentservice.NewService(agentservice.Dependencies{
		Entries:     a.sessions,
		ActiveRun:   coordinator.ActiveRun,
		SessionRuns: coordinator.SessionRuns,
		RunRecords:  a.sessions,
		Sessions:    a.storage.Sessions,
		Definitions: a.sessions.Definition,
	})
	models := buildModelService(config, a.logger)
	projects := projectservice.NewService(projectservice.Dependencies{
		Projects:   a.storage.Projects,
		Sessions:   a.storage.Sessions,
		Directory:  fs.Filesystem{},
		TextReader: fs.Filesystem{},
		Clock:      clock.UTCClock{},
	})
	approvals := approvalservice.NewService(approvalservice.Dependencies{
		Sessions: a.sessions,
		Records:  a.storage.Sessions,
		Clock:    clock.UTCClock{},
	})
	a.frontend = bindings.Services{
		Projects:       projects,
		Approvals:      approvals,
		Sessions:       a.sessions,
		Agents:         agents,
		Commands:       a.coordinator,
		ModelConfig:    models,
		Emitter:        emitter,
		Usages:         a.sessions,
		SessionLogPath: a.storage.Sessions.SessionLogPath,
	}
	a.logger.Infof("application open completed ready=true")
	ready = true
	return a, nil
}

func (a *Application) ApplicationLogger() *logging.Logger {
	factory := logging.NewFactory()
	if a == nil || a.logger == nil {
		return factory.Nop()
	}
	return factory.Ensure(a.logger)
}

// Close 只有在全部执行退出后才关闭存储；超时保留资源供后续关闭重试。
func (a *Application) Close(ctx context.Context) error {
	logger := a.ApplicationLogger()
	logger.Infof("application close started")
	if err := a.coordinator.Close(ctx); err != nil {
		logger.Errorf("execution close incomplete: %v", err)
		return err
	}
	sessionErr := a.sessions.CleanupEmptySessions(ctx)
	if err := ctx.Err(); err != nil {
		// 清理超时时保留可写存储，后续关闭仍能接纳尚未完成的会话删除。
		return errors.Join(sessionErr, err)
	}
	storageErr := a.storage.Close(ctx)
	if sessionErr != nil || storageErr != nil {
		logger.Errorf("application close encountered errors sessions=%v storage=%v", sessionErr, storageErr)
	} else {
		logger.Infof("application close completed")
	}
	if ctx.Err() != nil {
		return errors.Join(sessionErr, storageErr)
	}
	return errors.Join(sessionErr, storageErr, logger.Close())
}
