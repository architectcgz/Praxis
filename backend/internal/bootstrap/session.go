package bootstrap

import (
	"praxis/internal/clock"
	"praxis/internal/core/coordinator"
	"praxis/internal/core/hook"
	"praxis/internal/identity"
	"praxis/internal/infra/storage"
	sessionservice "praxis/internal/service/session"
)

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
