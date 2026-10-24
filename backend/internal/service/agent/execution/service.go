package execution

import (
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
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
	Agent          func(identity.AgentID) (*agentmodel.Agent, error)
	Records        repository.AgentExecution
	Entries        repository.Entries
	Definitions    AgentDefinitionProvider
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 负责 Execution 的创建、上下文冻结、进度和结算；调度与执行中的临时状态由 Coordinator 拥有。
type Service struct {
	hookDispatcher *hook.Dispatcher
	logger         *logging.Logger
	emitter        *coreevent.Emitter
	sessions       repository.Session
	agent          func(identity.AgentID) (*agentmodel.Agent, error)
	records        repository.AgentExecution
	entries        repository.Entries
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
		agent:          deps.Agent,
		records:        deps.Records,
		entries:        deps.Entries,
		definitions:    deps.Definitions,
		clock:          deps.Clock,
		ids:            deps.IDs,
	}
}
