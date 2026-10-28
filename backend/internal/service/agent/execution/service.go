package execution

import (
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/internal/logging"
	"praxis/internal/repository"
)

// AgentDefinitionProvider 提供已校验的 Agent 定义。
type AgentDefinitionProvider interface {
	Definition(identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

type Dependencies struct {
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	Emitter        *coreevent.Emitter
	Sessions       repository.Session
	Records        repository.AgentExecution
	UsageRecords   repository.ModelUsage
	History        sessionmodel.History
	Definitions    AgentDefinitionProvider
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 负责执行准入、上下文投影、进度和结算；主会话历史由 Session 提供。
type Service struct {
	hookDispatcher *hook.Dispatcher
	logger         *logging.Logger
	emitter        *coreevent.Emitter
	sessions       repository.Session
	records        repository.AgentExecution
	usageRecords   repository.ModelUsage
	history        sessionmodel.History
	definitions    AgentDefinitionProvider
	clock          clock.Clock
	ids            identity.IDGenerator
}

// NewService 使用装配层提供的完整依赖创建 Execution 业务服务。
func NewService(deps Dependencies) *Service {
	return &Service{
		hookDispatcher: deps.HookDispatcher,
		logger:         deps.Logger,
		emitter:        deps.Emitter,
		sessions:       deps.Sessions,
		records:        deps.Records,
		usageRecords:   deps.UsageRecords,
		history:        deps.History,
		definitions:    deps.Definitions,
		clock:          deps.Clock,
		ids:            deps.IDs,
	}
}
