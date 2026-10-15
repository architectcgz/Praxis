// Package task 负责 Task 创建、准备、恢复绑定、进度持久化和终态结算，不负责执行调度。
package task

import (
	"context"
	"errors"
	"fmt"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/hooks"
	"praxis/internal/logging"
	"praxis/internal/repository"
	"praxis/internal/system"
)

// AgentDefinitionProvider 提供已校验的 Agent 定义。
type AgentDefinitionProvider interface {
	Definition(contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

// ModelConfigResolver 从模型目录解析选定模型或 Agent 默认模型配置。
type ModelConfigResolver interface {
	ResolveTaskModel(string, string, string) (model.Config, error)
	ResolveDefaultTaskModel(contracts.AgentDefinitionID) (model.Config, error)
}

// ExecutionSlots 在准入事务中预留身份，并在回滚或结算提交后释放。
type ExecutionSlots interface {
	Reserve(context.Context, contracts.AgentID, contracts.TaskID) (context.Context, bool, error)
	Release(contracts.AgentID, contracts.TaskID)
	Pause(contracts.AgentID, contracts.TaskID) bool
	Stop(contracts.AgentID, contracts.TaskID) bool
}

type Config struct {
	Controls       repository.AgentControlCommandRepository
	Transactions   repository.TxRunner
	HookDispatcher *hooks.Dispatcher
	Logger         *logging.Logger
	EventObserver  func(TerminalEvent)
	Workspaces     repository.WorkspaceRepository
	Sessions       repository.SessionRepository
	Agents         repository.SessionAgentRepository
	Tasks          repository.TaskRepository
	Executions     ExecutionSlots
	Messages       repository.MessageStreams
	PrimaryAgent   PrimaryAgentProvider
	Definitions    AgentDefinitionProvider
	Models         ModelConfigResolver
	ContextBuilder *TaskContextBuilder
	Clock          system.Clock
	IDs            system.IDGenerator
}

// Service 统一负责 Task 准入、确定执行输入、恢复绑定、进度持久化及原子结算。
type Service struct {
	controls       repository.AgentControlCommandRepository
	tx             repository.TxRunner
	hookDispatcher *hooks.Dispatcher
	logger         *logging.Logger
	eventObserver  func(TerminalEvent)
	workspaces     repository.WorkspaceRepository
	sessions       repository.SessionRepository
	agents         repository.SessionAgentRepository
	tasks          repository.TaskRepository
	executions     ExecutionSlots
	messages       repository.MessageStreams
	primary        PrimaryAgentProvider
	models         ModelConfigResolver
	definitions    AgentDefinitionProvider
	contextBuilder *TaskContextBuilder
	clock          system.Clock
	ids            system.IDGenerator
}

// NewService 创建统一 Task 服务；缺少依赖或配置目录不是规范化绝对路径时返回错误。
func NewService(config Config) (*Service, error) {
	if config.Executions == nil {
		return nil, errors.New("task service executions are required")
	}
	if config.ContextBuilder == nil {
		return nil, errors.New("task service context builder is required")
	}
	for name, value := range map[string]any{
		"transactions":  config.Transactions,
		"controls":      config.Controls,
		"workspaces":    config.Workspaces,
		"sessions":      config.Sessions,
		"agents":        config.Agents,
		"tasks":         config.Tasks,
		"messages":      config.Messages,
		"primary agent": config.PrimaryAgent,
		"definitions":   config.Definitions,
		"models":        config.Models,
	} {
		if value == nil {
			return nil, fmt.Errorf("task service %s is required", name)
		}
	}
	return &Service{
		controls:       config.Controls,
		tx:             config.Transactions,
		hookDispatcher: config.HookDispatcher,
		logger:         logging.NewFactory().Ensure(config.Logger),
		eventObserver:  config.EventObserver,
		workspaces:     config.Workspaces,
		sessions:       config.Sessions,
		agents:         config.Agents,
		tasks:          config.Tasks,
		executions:     config.Executions,
		messages:       config.Messages,
		primary:        config.PrimaryAgent,
		models:         config.Models,
		definitions:    config.Definitions,
		contextBuilder: config.ContextBuilder,
		clock:          system.ClockOrDefault(config.Clock),
		ids:            system.IDsOrDefault(config.IDs),
	}, nil
}

// ReleaseExecution 释放准入时建立的进程内执行绑定；只清理匹配的 Task，
// 不会取消或删除同一 Agent 后续建立的绑定。
func (s *Service) ReleaseExecution(agentID contracts.AgentID, taskID contracts.TaskID) {
	s.executions.Release(agentID, taskID)
}

// Abort 结算未被 runtime 接管的 Task 并释放匹配的执行绑定；结算失败时保留绑定。
func (s *Service) Abort(ctx context.Context, taskID contracts.TaskID) error {
	if ctx == nil {
		return errors.New("task abort context is required")
	}
	ended, err := s.Settle(ctx, taskID, taskmodel.TaskInterrupted, contracts.TaskFailureInterrupted, "")
	if err != nil {
		return err
	}
	s.ReleaseExecution(ended.AgentID, taskID)
	return nil
}

// PrimaryAgentProvider 解析直接输入所属会话的 main agent。
type PrimaryAgentProvider interface {
	GetOrCreatePrimaryAgent(context.Context, contracts.SessionID, contracts.RequestID) (agentmodel.Agent, error)
}
