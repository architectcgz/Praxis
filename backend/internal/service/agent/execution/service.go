package execution

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	"praxis/internal/identity"
	"praxis/internal/logging"
	"praxis/internal/repository"
)

// AgentDefinitionProvider 提供已校验的 Agent 定义。
type AgentDefinitionProvider interface {
	Definition(identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
	ResolveModelReference(identity.AgentDefinitionID) (agentmodel.ModelReference, error)
}

// ModelConfigResolver 从模型目录解析选定模型或 Agent 默认模型配置。
type ModelConfigResolver interface {
	ResolveModelConfig(string, string, string) (modelapi.Config, error)
}

type Dependencies struct {
	Controls       repository.AgentControl
	Transactions   repository.Transaction
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	EventObserver  func(TerminalEvent)
	Workspaces     repository.Workspace
	Sessions       repository.Session
	Agents         repository.Agent
	Records        repository.AgentExecution
	Queue          repository.Task
	ToolExecutions repository.ToolExecution
	Executions     ActiveTasks
	Messages       repository.MessageStreams
	PrimaryAgent   PrimaryAgentProvider
	Definitions    AgentDefinitionProvider
	Models         ModelConfigResolver
	Contexts       repository.SessionContext
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 负责 Execution 的创建、队列准备、上下文冻结、恢复、进度和结算，不持有单次执行的临时状态。
type Service struct {
	controls       repository.AgentControl
	tx             repository.Transaction
	hookDispatcher *hook.Dispatcher
	logger         *logging.Logger
	eventObserver  func(TerminalEvent)
	workspaces     repository.Workspace
	sessions       repository.Session
	agents         repository.Agent
	records        repository.AgentExecution
	queue          repository.Task
	toolExecutions repository.ToolExecution
	executions     ActiveTasks
	messages       repository.MessageStreams
	primary        PrimaryAgentProvider
	models         ModelConfigResolver
	definitions    AgentDefinitionProvider
	contexts       repository.SessionContext
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
		"transactions":    deps.Transactions,
		"controls":        deps.Controls,
		"workspaces":      deps.Workspaces,
		"sessions":        deps.Sessions,
		"agents":          deps.Agents,
		"tasks":           deps.Records,
		"tool executions": deps.ToolExecutions,
		"messages":        deps.Messages,
		"primary agent":   deps.PrimaryAgent,
		"definitions":     deps.Definitions,
		"models":          deps.Models,
	} {
		if value == nil {
			return nil, fmt.Errorf("task service %s is required", name)
		}
	}
	return &Service{
		controls:       deps.Controls,
		tx:             deps.Transactions,
		hookDispatcher: deps.HookDispatcher,
		logger:         logging.NewFactory().Ensure(deps.Logger),
		eventObserver:  deps.EventObserver,
		workspaces:     deps.Workspaces,
		sessions:       deps.Sessions,
		agents:         deps.Agents,
		records:        deps.Records,
		queue:          deps.Queue,
		toolExecutions: deps.ToolExecutions,
		executions:     deps.Executions,
		messages:       deps.Messages,
		primary:        deps.PrimaryAgent,
		models:         deps.Models,
		definitions:    deps.Definitions,
		contexts:       deps.Contexts,
		clock:          clock.ClockOrDefault(deps.Clock),
		ids:            identity.IDsOrDefault(deps.IDs),
	}, nil
}

// PrimaryAgentProvider 解析直接输入所属会话的 main agent。
type PrimaryAgentProvider interface {
	GetOrCreatePrimaryAgent(context.Context, identity.SessionID, identity.RequestID) (agentmodel.Agent, error)
}
