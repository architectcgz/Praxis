package bootstrap

import (
	"context"
	"praxis/internal/clock"
	"praxis/internal/core/hook"
	"praxis/internal/dataroot"
	"praxis/internal/identity"
	"praxis/internal/infra/storage"
	sessionservice "praxis/internal/service/session"
)

func buildSessions(store *storage.Store, hooks *hook.Dispatcher, config *configuration, root dataroot.DataRoot) *sessionservice.Service {
	return sessionservice.NewService(sessionservice.Dependencies{
		LoadConfig: func(ctx context.Context) (*sessionservice.Configuration, error) {
			return config.loadSessionConfiguration(ctx, root)
		},
		HookDispatcher: hooks,
		Projects:       store.Projects,
		Sessions:       store.Sessions,
		Log:            store.Logs,
		Clock:          clock.UTCClock{},
		IDs:            identity.SecureIDGenerator{},
	})
}
