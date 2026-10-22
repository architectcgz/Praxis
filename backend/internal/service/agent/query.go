// Package agent 提供 Agent 状态查询、消息查询和执行控制用例。
package agent

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// ExecutionControl 只在控制意图持久化成功后传递取消信号。
type ExecutionControl interface {
	Pause(identity.AgentID, identity.AgentExecutionID) bool
	Stop(identity.AgentID, identity.AgentExecutionID) bool
}

type Dependencies struct {
	Agents           repository.Agent
	ExecutionRecords repository.AgentExecution
	Controls         repository.AgentControl
	Executions       ExecutionControl
	Sessions         repository.Session
	Entries          repository.Entries
	Clock            clock.Clock
}

type Service struct {
	agents           repository.Agent
	executionRecords repository.AgentExecution
	controls         repository.AgentControl
	executions       ExecutionControl
	sessions         repository.Session
	entries          repository.Entries
	clock            clock.Clock
}

// AgentView 是单个 Agent 的持久化详情视图。
type AgentView struct {
	Agent           agentmodel.Agent
	ActiveExecution *agentmodel.Execution
	Executions      []agentmodel.Execution
	Controls        []agentmodel.AgentControlCommand
}

// NewService 校验查询依赖并创建 Agent 查询服务；缺少仓储或消息读取函数时返回错误。
func NewService(deps Dependencies) (*Service, error) {
	for name, value := range map[string]any{
		"sessions":         deps.Sessions,
		"agents":           deps.Agents,
		"executions":       deps.ExecutionRecords,
		"control requests": deps.Controls,
	} {
		if value == nil {
			return nil, fmt.Errorf("agent service %s is required", name)
		}
	}
	if deps.Entries == nil || deps.Executions == nil {
		return nil, errors.New("agent service entries and executions are required")
	}
	return &Service{
		agents:           deps.Agents,
		executionRecords: deps.ExecutionRecords,
		controls:         deps.Controls,
		executions:       deps.Executions,
		entries:          deps.Entries,
		sessions:         deps.Sessions,
		clock:            clock.ClockOrDefault(deps.Clock),
	}, nil
}

// GetAgentView 返回 Agent、执行、预约和控制状态，不包含 workflow 编排状态。
func (s *Service) GetAgentView(ctx context.Context, agentID identity.AgentID, limit int) (AgentView, error) {
	if ctx == nil {
		return AgentView{}, errors.New("agent query context is required")
	}
	if s.executionRecords == nil || s.controls == nil {
		return AgentView{}, errors.New("agent detail query is unavailable")
	}
	agent, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return AgentView{}, err
	}
	active, err := s.executionRecords.GetActiveByAgent(ctx, agentID)
	if errors.Is(err, fault.ErrNotFound) {
		active = agentmodel.Execution{}
	} else if err != nil {
		return AgentView{}, fmt.Errorf("load active agent task: %w", err)
	}
	tasks, err := s.executionRecords.ListByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	controls, err := s.controls.ListOpenByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	view := AgentView{
		Agent:      agent,
		Executions: tasks,
		Controls:   controls,
	}
	if active.ID != "" {
		activeCopy := active
		view.ActiveExecution = &activeCopy
	}
	return view, nil
}
