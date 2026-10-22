package execution

import (
	"errors"
	"fmt"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/hook"
	taskmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/internal/logging"
	"praxis/internal/repository"
)

// AgentDefinitionProvider 提供已校验的 Agent 定义。
type AgentDefinitionProvider interface {
	Definition(identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

type Dependencies struct {
	Controls       repository.AgentControl
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	EventObserver  func(Event)
	Sessions       repository.Session
	Agents         repository.Agent
	Records        repository.AgentExecution
	Executions     *taskmodel.ActiveTasks
	Entries        repository.Entries
	Definitions    AgentDefinitionProvider
	Contexts       repository.SessionFacts
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 负责 Execution 的创建、队列准备、上下文冻结、恢复、进度和结算，不持有单次执行的临时状态。
type Service struct {
	controls       repository.AgentControl
	hookDispatcher *hook.Dispatcher
	logger         *logging.Logger
	eventObserver  func(Event)
	sessions       repository.Session
	agents         repository.Agent
	records        repository.AgentExecution
	executions     *taskmodel.ActiveTasks
	entries        repository.Entries
	definitions    AgentDefinitionProvider
	contexts       repository.SessionFacts
	clock          clock.Clock
	ids            identity.IDGenerator
}

// NewService 检查 Execution 用例依赖；输入规范化由公开用例的输入边界负责。
func NewService(deps Dependencies) (*Service, error) {
	if deps.Executions == nil {
		return nil, errors.New("task service executions are required")
	}
	if deps.Contexts == nil {
		return nil, errors.New("task service contexts are required")
	}
	for name, value := range map[string]any{
		"controls":    deps.Controls,
		"sessions":    deps.Sessions,
		"agents":      deps.Agents,
		"tasks":       deps.Records,
		"entries":     deps.Entries,
		"definitions": deps.Definitions,
	} {
		if value == nil {
			return nil, fmt.Errorf("task service %s is required", name)
		}
	}
	return &Service{
		controls:       deps.Controls,
		hookDispatcher: deps.HookDispatcher,
		logger:         logging.NewFactory().Ensure(deps.Logger),
		eventObserver:  deps.EventObserver,
		sessions:       deps.Sessions,
		agents:         deps.Agents,
		records:        deps.Records,
		executions:     deps.Executions,
		entries:        deps.Entries,
		definitions:    deps.Definitions,
		contexts:       deps.Contexts,
		clock:          clock.ClockOrDefault(deps.Clock),
		ids:            identity.IDsOrDefault(deps.IDs),
	}, nil
}
