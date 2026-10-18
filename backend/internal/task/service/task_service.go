package service

import (
	"context"
	"errors"
	"fmt"
	agentmodel "praxis/internal/agent"
	"praxis/internal/clock"
	"praxis/internal/hook"
	"praxis/internal/identity"
	"praxis/internal/logging"
	modelapi "praxis/internal/model"
	sessionmodel "praxis/internal/session"
	taskmodel "praxis/internal/task"
	"praxis/internal/toolcall"
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
	ResolveTaskModel(string, string, string) (modelapi.Config, error)
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
	Tasks          TaskStore
	ToolExecutions toolcall.Store
	Executions     ExecutionSlots
	Messages       MessageStreams
	PrimaryAgent   PrimaryAgentProvider
	Definitions    AgentDefinitionProvider
	Models         ModelConfigResolver
	Contexts       sessionmodel.ContextStore
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// Service 负责 Task 的创建、队列准备、上下文冻结、恢复、进度和结算，不持有单次执行的临时状态。
type Service struct {
	controls       agentmodel.ControlStore
	tx             txrunner.Runner
	hookDispatcher *hook.Dispatcher
	logger         *logging.Logger
	eventObserver  func(TerminalEvent)
	workspaces     workspacemodel.Store
	sessions       SessionStore
	agents         agentmodel.Store
	tasks          TaskStore
	toolExecutions toolcall.Store
	executions     ExecutionSlots
	messages       MessageStreams
	primary        PrimaryAgentProvider
	models         ModelConfigResolver
	definitions    AgentDefinitionProvider
	contexts       sessionmodel.ContextStore
	clock          clock.Clock
	ids            identity.IDGenerator
}

// NewService 检查 Task 用例依赖；输入规范化由公开用例的输入边界负责。
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
		"tasks":           deps.Tasks,
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
		tasks:          deps.Tasks,
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

// Get 读取 Task 持久化事实，供工具调用和执行恢复复用。
func (s *Service) Get(ctx context.Context, id identity.TaskID) (taskmodel.Task, error) {
	return s.tasks.Get(ctx, id)
}

// Save 保存已完成状态转换的 Task。
func (s *Service) Save(ctx context.Context, value taskmodel.Task) error {
	return s.tasks.Save(ctx, value)
}

// ReleaseExecution 释放准入时建立的进程内执行绑定；只清理匹配的 Task，
// 不会取消或删除同一 Agent 后续建立的绑定。
func (s *Service) ReleaseExecution(agentID identity.AgentID, taskID identity.TaskID) {
	s.executions.Release(agentID, taskID)
}

// Abort 结算未被 runtime 接管的 Task 并释放匹配的执行绑定；结算失败时保留绑定。
func (s *Service) Abort(ctx context.Context, taskID identity.TaskID) error {
	if ctx == nil {
		return errors.New("task abort context is required")
	}
	ended, err := s.Settle(ctx, taskID, taskmodel.TaskInterrupted, taskmodel.TaskFailureInterrupted, "")
	if err != nil {
		return err
	}
	s.ReleaseExecution(ended.AgentID, taskID)
	return nil
}

// PrimaryAgentProvider 解析直接输入所属会话的 main agent。
type PrimaryAgentProvider interface {
	GetOrCreatePrimaryAgent(context.Context, identity.SessionID, identity.RequestID) (agentmodel.Agent, error)
}
