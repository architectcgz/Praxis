package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"praxis/internal/clock"
	"praxis/internal/core/coordinator"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/core/tool"
	"praxis/internal/dataroot"
	"praxis/internal/identity"
	fs "praxis/internal/infra/filesystem"
	"praxis/internal/infra/storage"
	"praxis/internal/logging"
	approvalservice "praxis/internal/service/approval"
	projectservice "praxis/internal/service/project"
	sessionservice "praxis/internal/service/session"
	"praxis/wails/bindings"
)

type storageResources struct {
	storage *storage.Store
	logger  *logging.Logger
}

func openStorage(ctx context.Context, root dataroot.DataRoot) (storageResources, error) {
	logger, err := logging.NewFactory().Application(filepath.Join(root.Root, "praxis.log"))
	if err != nil {
		return storageResources{}, fmt.Errorf("open runtime log: %w", err)
	}
	logger.Infof("application open started root=%s", root.Root)
	state, err := storage.Open(ctx, root.Root)
	if err != nil {
		logger.Errorf("open storage failed: %v", err)
		_ = logger.Close()
		return storageResources{}, fmt.Errorf("open storage: %w", err)
	}
	return storageResources{
		storage: state,
		logger:  logger,
	}, nil
}

func buildSessions(store *storage.Store, hooks *hook.Dispatcher, core *coordinator.Coordinator) *sessionservice.Service {
	return sessionservice.NewService(sessionservice.Dependencies{
		Core:           core,
		HookDispatcher: hooks,
		Projects:       store.Projects,
		Sessions:       store.Sessions,
		Clock:          clock.UTCClock{},
		IDs:            identity.SecureIDGenerator{},
	})
}

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
	coordinator := buildCoordinator(resources, func(ctx context.Context) (*sessionmodel.Configuration, error) {
		return config.loadSessionConfiguration(ctx, root)
	}, hooks, emitter, options.Runner)
	a.sessions = buildSessions(a.storage, hooks, coordinator)
	a.coordinator = coordinator
	projects := projectservice.NewService(projectservice.Dependencies{
		Projects:  a.storage.Projects,
		Sessions:  a.storage.Sessions,
		Directory: fs.Filesystem{},
		Clock:     clock.UTCClock{},
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
		Commands:       a.coordinator,
		Emitter:        emitter,
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
