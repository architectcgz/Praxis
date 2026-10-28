package bootstrap

import (
	"context"
	"errors"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/core/tool"
	"praxis/internal/dataroot"
	"praxis/internal/identity"
	fs "praxis/internal/infra/filesystem"
	"praxis/internal/logging"
	agentservice "praxis/internal/service/agent"
	approvalservice "praxis/internal/service/approval"
	execution "praxis/internal/service/coordinator"
	projectservice "praxis/internal/service/project"
	sessionservice "praxis/internal/service/session"
	workspaceservice "praxis/internal/service/workspace"
	"praxis/wails/bindings"
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
		// 装配失败也要先停止可能已恢复的执行，再关闭其依赖的日志资源。
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
	repos := a.storage.Repositories()
	if err := agentservice.ValidateDefinitions(nil, config.agents.List()); err != nil {
		return nil, err
	}
	emitter := coreevent.NewEmitter()
	hooks, err := hook.NewDispatcher(options.Hooks...)
	if err != nil {
		return nil, err
	}
	configMu := &config.mu
	// 只在装配处绑定跨模块协作；业务入口在实例与执行恢复完成后才开放。
	var agents *agentservice.Service
	mainAgent := func(ctx context.Context, id identity.SessionID) (*agentmodel.Agent, error) {
		return agents.GetOrCreateMainAgent(ctx, id)
	}
	a.sessions = buildSessions(resources, repos, config.agents, hooks, configMu, config.models, mainAgent)
	agents = agentservice.NewService(agentservice.Dependencies{
		History: a.sessions,
		Agent:   a.sessions.GetAgent,
		ActiveExecution: func(id identity.AgentID) (agentexecution.ExecutionRecord, bool) {
			return a.coordinator.ActiveExecution(id)
		},
		RegisterAgent:    a.sessions.RegisterAgent,
		ExecutionRecords: repos.Executions,
		Sessions:         repos.Sessions,
		Definitions:      config.agents.Definition,
	})
	models := buildModelService(root, config, a.logger)
	coordinator := buildCoordinator(resources, repos, config, a.sessions, agents.GetOrCreateMainAgent, hooks, emitter, configMu, options.Runner)
	a.coordinator = coordinator
	projects := projectservice.NewService(projectservice.Dependencies{
		Projects:   repos.Projects,
		Workspaces: repos.Workspaces,
		Directory:  fs.Filesystem{},
		Clock:      clock.UTCClock{},
	})
	approvals := approvalservice.NewService(approvalservice.Dependencies{
		Config:   config,
		Projects: repos.Projects,
		Clock:    clock.UTCClock{},
	})
	workspaces := workspaceservice.NewService(workspaceservice.Dependencies{
		Projects:   repos.Projects,
		Sessions:   repos.Sessions,
		Workspaces: repos.Workspaces,
		TextReader: fs.Filesystem{},
	})
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
		Timings:        coordinator,
		Usages:         a.sessions,
		SessionLogPath: repos.Sessions.SessionLogPath,
	}
	if err := agents.RestoreAgents(ctx); err != nil {
		return nil, err
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
