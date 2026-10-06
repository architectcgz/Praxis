// Package agent 提供 Agent 状态查询、消息查询和执行控制用例。
package agent

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"

	"context"
	"errors"
	"fmt"

	"praxis/internal/repository"
	"praxis/internal/system"
)

type Config struct {
	Transactions repository.TxRunner
	Agents       repository.SessionAgentRepository
	Tasks        repository.TaskRepository
	Controls     repository.AgentControlCommandRepository
	Executions   *agentmodel.Executions
	Messages     func(context.Context, contracts.SessionID, contracts.AgentID, int) ([]sessionmodel.MessageData, error)
	Clock        system.Clock
}

type Service struct {
	tx         repository.TxRunner
	agents     repository.SessionAgentRepository
	tasks      repository.TaskRepository
	controls   repository.AgentControlCommandRepository
	executions *agentmodel.Executions
	messages   func(context.Context, contracts.SessionID, contracts.AgentID, int) ([]sessionmodel.MessageData, error)
	clock      system.Clock
}

// AgentView 是单个 Agent 的持久化详情视图。
type AgentView struct {
	Agent      agentmodel.Agent
	ActiveTask *taskmodel.Task
	Tasks      []taskmodel.Task
	Controls   []agentmodel.AgentControlCommand
}

// NewService 校验查询依赖并创建 Agent 查询服务；缺少仓储或消息读取函数时返回错误。
func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions":     config.Transactions,
		"agents":           config.Agents,
		"tasks":            config.Tasks,
		"control requests": config.Controls,
	} {
		if value == nil {
			return nil, fmt.Errorf("agent service %s is required", name)
		}
	}
	if config.Messages == nil || config.Executions == nil {
		return nil, errors.New("agent service messages and executions are required")
	}
	return &Service{
		tx:         config.Transactions,
		agents:     config.Agents,
		tasks:      config.Tasks,
		controls:   config.Controls,
		executions: config.Executions,
		messages:   config.Messages,
		clock:      system.ClockOrDefault(config.Clock),
	}, nil
}

// GetAgentView 返回 Agent、Task 和控制状态，不包含 workflow 编排状态。
func (s *Service) GetAgentView(ctx context.Context, agentID contracts.AgentID, limit int) (AgentView, error) {
	if ctx == nil {
		return AgentView{}, errors.New("agent query context is required")
	}
	if s.tasks == nil || s.controls == nil {
		return AgentView{}, errors.New("agent detail query is unavailable")
	}
	agent, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return AgentView{}, err
	}
	active, err := s.tasks.GetActiveByAgent(ctx, agentID)
	if errors.Is(err, contracts.ErrNotFound) {
		active = taskmodel.Task{}
	} else if err != nil {
		return AgentView{}, fmt.Errorf("load active agent task: %w", err)
	}
	tasks, err := s.tasks.ListByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	controls, err := s.controls.ListOpenByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	view := AgentView{
		Agent:    agent,
		Tasks:    tasks,
		Controls: controls,
	}
	if active.ID != "" {
		activeCopy := active
		view.ActiveTask = &activeCopy
	}
	return view, nil
}

// ListAgentMessages 读取 Agent 自有消息流。
func (s *Service) ListAgentMessages(ctx context.Context, agentID contracts.AgentID, limit int) ([]sessionmodel.MessageData, error) {
	if ctx == nil {
		return nil, errors.New("agent message context is required")
	}
	if s.messages == nil {
		return nil, errors.New("agent message query is unavailable")
	}
	agent, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return s.messages(ctx, agent.SessionID, agent.ID, limit)
}
