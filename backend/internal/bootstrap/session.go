package bootstrap

import (
	"praxis/internal/clock"
	"praxis/internal/core/hook"
	"praxis/internal/identity"
	"praxis/internal/infra/storage"
	sessionservice "praxis/internal/service/session"
	"sync"
)

func buildSessions(store *storage.Store, hooks *hook.Dispatcher, configMu *sync.RWMutex, models sessionservice.ModelConfigResolver) *sessionservice.Service {
	return sessionservice.NewService(sessionservice.Dependencies{
		ConfigMu:       configMu,
		ModelConfigs:   models,
		HookDispatcher: hooks,
		Projects:       store.Projects,
		Sessions:       store.Sessions,
		Log:            store.Logs,
		Clock:          clock.UTCClock{},
		IDs:            identity.SecureIDGenerator{},
	})
}
