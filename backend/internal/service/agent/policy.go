// Package agent 提供 Agent 状态和消息查询用例。
package agent

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	turnmodel "praxis/internal/core/turn"
	workflowmodel "praxis/internal/core/workflow"

	"context"
	"errors"
	"fmt"

	"praxis/internal/repository"
)

type Config struct {
	Agents   repository.SessionAgentRepository
	Turns    repository.TurnRepository
	Waits    repository.WaitConditionRepository
	Controls repository.AgentControlCommandRepository
	Messages func(context.Context, contracts.SessionID, contracts.AgentID, int) ([]sessionmodel.MessageData, error)
}

type Service struct {
	agents   repository.SessionAgentRepository
	turns    repository.TurnRepository
	waits    repository.WaitConditionRepository
	controls repository.AgentControlCommandRepository
	messages func(context.Context, contracts.SessionID, contracts.AgentID, int) ([]sessionmodel.MessageData, error)
}

// AgentView 是单个 Agent 的持久化详情视图。
type AgentView struct {
	Agent      agentmodel.Agent
	ActiveTurn *turnmodel.Turn
	Turns      []turnmodel.Turn
	Waits      []workflowmodel.WaitCondition
	Controls   []workflowmodel.AgentControlCommand
}

// NewService 校验查询依赖并创建 Agent 查询服务；缺少仓储或消息读取函数时返回错误。
func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"agents":           config.Agents,
		"turns":            config.Turns,
		"wait conditions":  config.Waits,
		"control requests": config.Controls,
	} {
		if value == nil {
			return nil, fmt.Errorf("agent service %s is required", name)
		}
	}
	if config.Messages == nil {
		return nil, errors.New("agent service messages is required")
	}
	return &Service{
		agents:   config.Agents,
		turns:    config.Turns,
		waits:    config.Waits,
		controls: config.Controls,
		messages: config.Messages,
	}, nil
}

// GetAgentView 返回 Agent、执行、等待和控制状态的持久化详情。
func (s *Service) GetAgentView(ctx context.Context, agentID contracts.AgentID, limit int) (AgentView, error) {
	if ctx == nil {
		return AgentView{}, errors.New("agent query context is required")
	}
	if s.turns == nil || s.waits == nil || s.controls == nil {
		return AgentView{}, errors.New("agent detail query is unavailable")
	}
	agent, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return AgentView{}, err
	}
	active, err := s.turns.GetActiveByAgent(ctx, agentID)
	if errors.Is(err, contracts.ErrNotFound) {
		active = turnmodel.Turn{}
	} else if err != nil {
		return AgentView{}, fmt.Errorf("load active agent turn: %w", err)
	}
	turns, err := s.turns.ListByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	waits, err := s.waits.ListUnresolvedByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	controls, err := s.controls.ListOpenByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	view := AgentView{
		Agent:    agent,
		Turns:    turns,
		Waits:    waits,
		Controls: controls,
	}
	if active.ID != "" {
		activeCopy := active
		view.ActiveTurn = &activeCopy
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
