package bootstrap

import (
	"context"
	"errors"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	"praxis/internal/dataroot"
	fs "praxis/internal/infra/filesystem"
	"praxis/internal/logging"
	agentservice "praxis/internal/service/agent"
	execution "praxis/internal/service/agent/coordinator"
	approvalservice "praxis/internal/service/approval"
	projectservice "praxis/internal/service/project"
	sessionservice "praxis/internal/service/session"
	workspaceservice "praxis/internal/service/workspace"
	"praxis/wails/bindings"
	"sync"
)

// Application 拥有进程资源；业务状态与执行生命周期分别由业务能力和 Coordinator 管理。
type Application struct {
	storageResources
	frontend    bindings.Services
	sessions    *sessionservice.Service
	coordinator *execution.Coordinator
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

// Open 只做高层装配；恢复完成前不把业务入口交给桌面宿主。
func Open(ctx context.Context, root dataroot.DataRoot, options Options) (*Application, error) {
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
		if a.coordinator != nil {
			_ = a.coordinator.Close(context.Background())
		}
		_ = a.store.Close(context.Background())
		_ = a.logger.Close()
	}()
	config, err := loadConfiguration(root, options.Tools)
	if err != nil {
		return nil, err
	}
	repos := a.store.Repositories()
	if err := agentservice.ValidateStoredDefinitions(ctx, config.agents.List(), repos.Sessions, repos.Agents); err != nil {
		return nil, err
	}
	emitter := coreevent.NewEmitter()
	hooks, err := hook.NewDispatcher(options.Hooks...)
	if err != nil {
		return nil, err
	}
	configMu := &sync.RWMutex{}
	a.sessions, err = buildSessions(resources, repos, config.agents, hooks, configMu, config.models)
	if err != nil {
		return nil, err
	}
	models := buildModelService(root, config, a.logger, configMu)
	coordinator, timings, err := buildExecution(resources, repos, config, a.sessions, hooks, emitter, configMu, options.Runner)
	if err != nil {
		return nil, err
	}
	a.coordinator = coordinator
	agents, err := agentservice.NewService(agentservice.Dependencies{
		Agents:           repos.Agents,
		ExecutionRecords: repos.Executions,
		Sessions:         repos.Sessions,
		Controls:         repos.Controls,
		Executions:       a.coordinator,
		Entries:          repos.Entries,
		Definitions:      config.agents.Definition,
	})
	if err != nil {
		return nil, err
	}
	projects, err := projectservice.NewService(projectservice.Dependencies{
		Projects:   repos.Projects,
		Workspaces: repos.Workspaces,
		Directory:  fs.Filesystem{},
	})
	if err != nil {
		return nil, err
	}
	approvals, err := approvalservice.NewService(approvalservice.Dependencies{
		ConfigMu: configMu,
		GlobalApprovalMode: func() security.ApprovalMode {
			configMu.RLock()
			defer configMu.RUnlock()
			return config.settings.ApprovalMode
		},
		Projects: repos.Projects,
	})
	if err != nil {
		return nil, err
	}
	workspaces, err := workspaceservice.NewService(workspaceservice.Dependencies{
		Sessions:   repos.Sessions,
		Workspaces: repos.Workspaces,
		TextReader: fs.Filesystem{},
	})
	if err != nil {
		return nil, err
	}
	a.frontend = bindings.Services{
		Projects:       projects,
		Approvals:      approvals,
		Sessions:       a.sessions,
		Workspaces:     workspaces,
		Agents:         agents,
		Commands:       a.coordinator,
		Models:         models,
		ModelConfig:    models,
		Emitter:        emitter,
		Timings:        timings,
		Usages:         a.sessions,
		SessionLogPath: a.store.SessionLogPath,
	}
	if err := a.coordinator.Restore(ctx); err != nil {
		return nil, err
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
	if ctx == nil {
		return errors.New("application close context is required")
	}
	logger := a.ApplicationLogger()
	logger.Infof("application close started")
	if err := a.coordinator.Close(ctx); err != nil {
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
