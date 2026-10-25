package bootstrap

import (
	"context"
	"net/http"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/core/security"
	"praxis/internal/identity"
	"praxis/internal/infra/providers"
	"praxis/internal/infra/storage"
	executionservice "praxis/internal/service/agent/execution"
	execution "praxis/internal/service/coordinator"
	sessionservice "praxis/internal/service/session"
	toolservice "praxis/internal/service/toolexecution"
	"sync"
)

func buildExecution(resources storageResources, repos storage.Repositories, config *configuration, sessions *sessionservice.Service, mainAgent func(context.Context, identity.SessionID) (*agentmodel.Agent, error), hooks *hook.Dispatcher, emitter *coreevent.Emitter, configMu *sync.RWMutex, runner *loop.Runner) *execution.Coordinator {
	tasks := executionservice.NewService(executionservice.Dependencies{
		HookDispatcher: hooks,
		Logger:         resources.logger,
		Emitter:        emitter,
		Sessions:       repos.Sessions,
		Agent:          sessions.GetAgent,
		Records:        repos.Executions,
		Entries:        repos.Entries,
		Definitions:    config.agents,
		Clock:          clock.UTCClock{},
		IDs:            identity.SecureIDGenerator{},
	})

	provider := providers.New(providers.Dependencies{
		Credentials: config.models,
		HTTPClient:  http.DefaultClient,
	})

	coordinator := assembleCoordinator(coordinatorDependencies{
		Tasks:         tasks,
		Progress:      tasks,
		Records:       repos.Executions,
		SessionTasks:  sessions,
		MainAgent:     mainAgent,
		SessionModels: sessions,
		Recovery:      repos.Executions,
		ToolCalls:     repos.ToolCalls,
		Agent:         sessions.GetAgent,
		Entries:       repos.Entries,
		Sessions:      repos.Sessions,
		Workspaces:    repos.Workspaces,
		Provider:      provider,
		Tools:         config.tools,
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
		Clock:          clock.UTCClock{},
	})
	return coordinator
}
