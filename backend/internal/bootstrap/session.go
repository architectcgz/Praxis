package bootstrap

import (
	"praxis/internal/core/hook"
	"praxis/internal/infra/agentregistry"
	"praxis/internal/infra/storage"
	sessionservice "praxis/internal/service/session"
	"sync"
)

func buildSessions(resources storageResources, repos storage.Repositories, agents *agentregistry.Registry, hooks *hook.Dispatcher, configMu *sync.RWMutex, models sessionservice.ModelConfigResolver) (*sessionservice.Service, error) {
	return sessionservice.NewService(sessionservice.Dependencies{
		ConfigMu:       configMu,
		ModelConfigs:   models,
		HookDispatcher: hooks,
		Projects:       repos.Projects,
		Workspaces:     repos.Workspaces,
		Sessions:       repos.Sessions,
		Entries:        repos.Entries,
		Contexts:       repos.Contexts,
		Agents:         repos.Agents,
		Tasks:          repos.Executions,
		Definitions:    agents.Definition,
		UsageRecords:   repos.ModelUsage,
	})
}
