package input

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/clock"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	"praxis/internal/identity"
	"praxis/internal/logging"
	agentmodel "praxis/internal/service/agent"
	sessionmodel "praxis/internal/service/session"
	queueservice "praxis/internal/service/task"

	"praxis/internal/core/toolcall"
	txrunner "praxis/internal/transaction"
	workspacemodel "praxis/internal/workspace"
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
	Controls       agentmodel.ControlStore
	Transactions   txrunner.Runner
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	EventObserver  func(TerminalEvent)
	Workspaces     workspacemodel.Store
	Sessions       SessionStore
	Agents         agentmodel.Store
	Records        ExecutionStore
	Queue          queueservice.Store
	ToolExecutions toolcall.Store
	Executions     ActiveTasks
	Messages       MessageStreams
	PrimaryAgent   PrimaryAgentProvider
	Definitions    AgentDefinitionProvider
	Models         ModelConfigResolver
	Contexts       sessionmodel.ContextStore
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 负责 Execution 的创建、队列准备、上下文冻结、恢复、进度和结算，不持有单次执行的临时状态。
type Service struct {
	controls       agentmodel.ControlStore
	tx             txrunner.Runner
	hookDispatcher *hook.Dispatcher
	logger         *logging.Logger
	eventObserver  func(TerminalEvent)
	workspaces     workspacemodel.Store
	sessions       SessionStore
	agents         agentmodel.Store
	records        ExecutionStore
	queue          queueservice.Store
	toolExecutions toolcall.Store
	executions     ActiveTasks
	messages       MessageStreams
	primary        PrimaryAgentProvider
	models         ModelConfigResolver
	definitions    AgentDefinitionProvider
	contexts       sessionmodel.ContextStore
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

// Get 读取 Execution 持久化事实，供工具调用和执行恢复复用。
func (s *Service) Get(ctx context.Context, id identity.AgentExecutionID) (agentmodel.Execution, error) {
	return s.records.Get(ctx, id)
}

// Save 保存已完成状态转换的 Execution。
func (s *Service) Save(ctx context.Context, value agentmodel.Execution) error {
	return s.records.Save(ctx, value)
}

// PrimaryAgentProvider 解析直接输入所属会话的 main agent。
type PrimaryAgentProvider interface {
	GetOrCreatePrimaryAgent(context.Context, identity.SessionID, identity.RequestID) (agentmodel.Agent, error)
}
