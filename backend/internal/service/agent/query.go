// Package agent 提供 Agent 状态查询、消息查询和执行控制用例。
package agent

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
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
	Definitions      func(identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
	Clock            clock.Clock
}

type Service struct {
	agents           repository.Agent
	executionRecords repository.AgentExecution
	controls         repository.AgentControl
	executions       ExecutionControl
	sessions         repository.Session
	entries          repository.Entries
	definitions      func(identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
	clock            clock.Clock
}

// AgentView 直接组合 Agent 实体、派生运行状态和页面所需的执行信息。
type AgentView struct {
	agentmodel.Agent
	Name               string                    `json:"name"`
	State              agentmodel.AgentState     `json:"state"`
	CurrentExecutionID identity.AgentExecutionID `json:"currentExecutionId"`
	CurrentExecution   *agentexecution.Execution `json:"currentExecution"`
	LatestExecution    *agentexecution.Execution `json:"latestExecution"`
}

// DescribeAgent 从已加载的角色定义读取展示名称；名称变化不改变持久化身份。
func (s *Service) DescribeAgent(value agentmodel.Agent) (AgentView, error) {
	definition, err := s.definitions(value.DefinitionID)
	if err != nil {
		return AgentView{}, fmt.Errorf("load agent definition for display: %w", err)
	}
	return AgentView{
		Agent:              value,
		Name:               definition.Name,
		State:              value.State,
		CurrentExecutionID: value.CurrentExecutionID,
	}, nil
}

// NewService 校验查询依赖并创建 Agent 查询服务；缺少仓储或消息读取函数时返回错误。
func NewService(deps Dependencies) (*Service, error) {
	if deps.Definitions == nil {
		return nil, errors.New("agent service definitions are required")
	}
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
		definitions:      deps.Definitions,
		sessions:         deps.Sessions,
		clock:            clock.ClockOrDefault(deps.Clock),
	}, nil
}

// GetAgentView 返回当前执行与最近一次执行，不返回执行历史和控制命令列表。
func (s *Service) GetAgentView(ctx context.Context, agentID identity.AgentID) (AgentView, error) {
	if ctx == nil {
		return AgentView{}, errors.New("agent query context is required")
	}
	value, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return AgentView{}, err
	}
	view, err := s.DescribeAgent(value)
	if err != nil {
		return AgentView{}, err
	}
	latest, err := s.executionRecords.ListByAgent(ctx, agentID, 1)
	if err != nil {
		return AgentView{}, err
	}
	if len(latest) > 0 {
		view.LatestExecution = &latest[0]
	}
	if value.CurrentExecutionID != "" {
		if view.LatestExecution != nil && view.LatestExecution.ID == value.CurrentExecutionID {
			view.CurrentExecution = view.LatestExecution
		} else {
			current, err := s.executionRecords.Get(ctx, value.CurrentExecutionID)
			if err != nil {
				return AgentView{}, fmt.Errorf("load current agent execution: %w", err)
			}
			view.CurrentExecution = &current
		}
	}
	return view, nil
}
