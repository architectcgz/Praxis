package bootstrap

import (
	"context"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/hook"
	"praxis/internal/identity"
	"praxis/internal/infra/agentregistry"
	"praxis/internal/infra/storage"
	sessionservice "praxis/internal/service/session"
	"sync"
)

func buildSessions(resources storageResources, repos storage.Repositories, agents *agentregistry.Registry, hooks *hook.Dispatcher, configMu *sync.RWMutex, models sessionservice.ModelConfigResolver, mainAgent func(context.Context, identity.SessionID) (*agentmodel.Agent, error)) *sessionservice.Service {
	return sessionservice.NewService(sessionservice.Dependencies{
		ConfigMu:       configMu,
		ModelConfigs:   models,
		HookDispatcher: hooks,
		Projects:       repos.Projects,
		Workspaces:     repos.Workspaces,
		Sessions:       repos.Sessions,
		Entries:        repos.Entries,
		Executions:     repos.Executions,
		MainAgent:      mainAgent,
		Definitions:    agents.Definition,
		UsageRecords:   repos.ModelUsage,
		Clock:          clock.UTCClock{},
		IDs:            identity.SecureIDGenerator{},
	})
}
