package bootstrap

import (
	"praxis/internal/clock"
	"praxis/internal/core/hook"
	"praxis/internal/identity"
	"praxis/internal/infra/storage"
	sessionservice "praxis/internal/service/session"
	"sync"
)

func buildSessions(repos storage.Repositories, hooks *hook.Dispatcher, configMu *sync.RWMutex, models sessionservice.ModelConfigResolver) *sessionservice.Service {
	return sessionservice.NewService(sessionservice.Dependencies{
		ConfigMu:       configMu,
		ModelConfigs:   models,
		HookDispatcher: hooks,
		Projects:       repos.Projects,
		Workspaces:     repos.Workspaces,
		Sessions:       repos.Sessions,
		Entries:        repos.Entries,
		Executions:     repos.Executions,
		UsageRecords:   repos.ModelUsage,
		Clock:          clock.UTCClock{},
		IDs:            identity.SecureIDGenerator{},
	})
}
