package run

import (
	"praxis/internal/clock"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"

	"praxis/internal/identity"
	"praxis/internal/logging"
	"praxis/internal/repository"
)

type Dependencies struct {
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	Emitter        *coreevent.Emitter
	Sessions       repository.Session
	Clock          clock.Clock
	IDs            identity.IDGenerator
}
