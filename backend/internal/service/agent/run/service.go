package run

import (
	"praxis/internal/clock"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/internal/logging"
	"praxis/internal/repository"
)

type Dependencies struct {
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	Emitter        *coreevent.Emitter
	Sessions       repository.Session
	Records        sessionmodel.RunRecords
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 编排单次执行与 Loop、Session 的协作，负责准入、事实提交、进度和结算。
type Service struct {
	hookDispatcher *hook.Dispatcher
	logger         *logging.Logger
	emitter        *coreevent.Emitter
	sessions       repository.Session
	records        sessionmodel.RunRecords
	clock          clock.Clock
	ids            identity.IDGenerator
}

// NewService 使用装配层提供的完整依赖创建 Run 业务服务。
func NewService(deps Dependencies) *Service {
	return &Service{
		hookDispatcher: deps.HookDispatcher,
		logger:         deps.Logger,
		emitter:        deps.Emitter,
		sessions:       deps.Sessions,
		records:        deps.Records,
		clock:          deps.Clock,
		ids:            deps.IDs,
	}
}
