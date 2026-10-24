package bootstrap

import (
	"context"
	"praxis/internal/core/agent/execution/timing"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/core/security"
	taskmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/internal/infra/providers"
	"praxis/internal/infra/storage"
	execution "praxis/internal/service/agent/coordinator"
	executionservice "praxis/internal/service/agent/execution"
	sessionservice "praxis/internal/service/session"
	toolservice "praxis/internal/service/toolexecution"
	"sync"
)

func buildExecution(resources storageResources, repos storage.Repositories, config *configuration, sessions *sessionservice.Service, hooks *hook.Dispatcher, emitter *coreevent.Emitter, configMu *sync.RWMutex, runner *loop.Runner) (*execution.Coordinator, *executionservice.Service, error) {
	active := &taskmodel.ActiveTasks{}
	tasks, err := executionservice.NewService(executionservice.Dependencies{
		Controls:       repos.Controls,
		HookDispatcher: hooks,
		Logger:         resources.logger,
		Emitter:        emitter,
		Sessions:       repos.Sessions,
		Agents:         repos.Agents,
		Records:        repos.Executions,
		Executions:     active,
		Entries:        repos.Entries,
		Definitions:    config.agents,
	})
	if err != nil {
		return nil, nil, err
	}

	provider, err := providers.New(providers.Dependencies{Credentials: config.models})
	if err != nil {
		return nil, nil, err
	}

	coordinator, err := assembleCoordinator(coordinatorDependencies{
		TimingRecorder: timing.New(tasks.RecordTiming),
		Tasks:          tasks,
		Progress:       tasks,
		Records:        repos.Executions,
		SessionTasks:   sessions,
		MainAgent:      sessions,
		SessionModels:  sessions,
		Recovery:       repos.Executions,
		ActiveTasks:    active,
		ToolExecutions: repos.ToolExecutions,
		ToolCalls:      repos.ToolCalls,
		Agents:         repos.Agents,
		Entries:        repos.Entries,
		Sessions:       repos.Sessions,
		Workspaces:     repos.Workspaces,
		Provider:       provider,
		Tools:          config.tools,
		Rules: toolservice.ApprovalRules{
			ConfigMu: configMu,
			Catalog:  config.tools,
			DefaultMode: func(ctx context.Context, sessionID identity.SessionID) (security.ApprovalMode, error) {
				session, err := repos.Sessions.Get(ctx, sessionID)
				if err != nil {
					return "", err
				}
				project, err := repos.Projects.Get(ctx, session.ProjectID)
				if err != nil {
					return "", err
				}
				return security.EffectiveApprovalMode(config.settings.ApprovalMode, project.ApprovalMode), nil
			},
		},
		HookDispatcher: hooks,
		Logger:         resources.logger,
		ConfigMu:       configMu,
		Emitter:        emitter,
		ObserveUsage:   usageObserver(repos.ModelUsage, resources.logger),
		SummaryFormat:  defaultSummaryFormat(),
		Runner:         runner,
	})
	return coordinator, tasks, err
}
