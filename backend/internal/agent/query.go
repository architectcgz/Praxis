// Package agent 提供 Agent 状态查询、消息查询和执行控制用例。
package agent

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/clock"
	"praxis/internal/fault"
	"praxis/internal/identity"
	messagemodel "praxis/internal/message"
	txrunner "praxis/internal/transaction"
)

// ExecutionControl 只在控制意图持久化成功后传递取消信号。
type ExecutionControl interface {
	Pause(identity.AgentID, identity.AgentExecutionID) bool
	Stop(identity.AgentID, identity.AgentExecutionID) bool
}

// TaskReader 只查询 Agent 展示和固定目标控制所需的 Task 事实。
type TaskReader interface {
	Get(context.Context, identity.AgentExecutionID) (Execution, error)
	GetActiveByAgent(context.Context, identity.AgentID) (Execution, error)
	ListByAgent(context.Context, identity.AgentID, int) ([]Execution, error)
}

type Dependencies struct {
	Transactions txrunner.Runner
	Agents       Store
	Tasks        TaskReader
	Controls     ControlStore
	Executions   ExecutionControl
	Messages     messagemodel.Reader
	Clock        clock.Clock
}

type Service struct {
	tx         txrunner.Runner
	agents     Store
	tasks      TaskReader
	controls   ControlStore
	executions ExecutionControl
	messages   messagemodel.Reader
	clock      clock.Clock
}

// AgentView 是单个 Agent 的持久化详情视图。
type AgentView struct {
	Agent      Agent
	ActiveTask *Execution
	Tasks      []Execution
	Controls   []AgentControlCommand
}

// NewService 校验查询依赖并创建 Agent 查询服务；缺少仓储或消息读取函数时返回错误。
func NewService(deps Dependencies) (*Service, error) {
	for name, value := range map[string]any{
		"transactions":     deps.Transactions,
		"agents":           deps.Agents,
		"tasks":            deps.Tasks,
		"control requests": deps.Controls,
	} {
		if value == nil {
			return nil, fmt.Errorf("agent service %s is required", name)
		}
	}
	if deps.Messages == nil || deps.Executions == nil {
		return nil, errors.New("agent service messages and executions are required")
	}
	return &Service{
		tx:         deps.Transactions,
		agents:     deps.Agents,
		tasks:      deps.Tasks,
		controls:   deps.Controls,
		executions: deps.Executions,
		messages:   deps.Messages,
		clock:      clock.ClockOrDefault(deps.Clock),
	}, nil
}

// GetAgentView 返回 Agent、Task 和控制状态，不包含 workflow 编排状态。
func (s *Service) GetAgentView(ctx context.Context, agentID identity.AgentID, limit int) (AgentView, error) {
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
	if errors.Is(err, fault.ErrNotFound) {
		active = Execution{}
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
func (s *Service) ListAgentMessages(ctx context.Context, agentID identity.AgentID, limit int) ([]messagemodel.Message, error) {
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
	stream, err := s.messages.LoadMessages(ctx, agent.SessionID, agent.ID, limit)
	if err != nil {
		return nil, err
	}
	return stream.Messages, nil
}
