package bootstrap

import (
	"praxis/internal/core/hook"
	"praxis/internal/infra/agentregistry"
	"praxis/internal/infra/filesystem"
	"praxis/internal/infra/jsonl"
	sessionservice "praxis/internal/service/session"
	"sync"
)

func buildSessions(resources storageResources, repos jsonl.Repositories, agents *agentregistry.Registry, hooks *hook.Dispatcher, configMu *sync.RWMutex) (*sessionservice.Service, error) {
	return sessionservice.NewService(sessionservice.Dependencies{
		ConfigMu:       configMu,
		Transactions:   resources.store,
		HookDispatcher: hooks,
		Projects:       repos.Projects,
		Workspaces:     repos.Workspaces,
		Sessions:       repos.Sessions,
		Contexts:       repos.Contexts,
		Agents:         repos.Agents,
		Tasks:          repos.Executions,
		Definitions:    agents.Definition,
		UsageRecords:   repos.ModelUsage,
		TextReader:     fs.Filesystem{},
	})
}
