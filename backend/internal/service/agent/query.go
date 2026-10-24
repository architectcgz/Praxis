// Package agent 提供 Agent 实例创建、启动恢复、状态查询和消息查询用例。
package agent

import (
	"context"
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"time"
)

type Dependencies struct {
	Agent            func(identity.AgentID) (*agentmodel.Agent, error)
	RegisterAgent    func(context.Context, *agentmodel.Agent) (*agentmodel.Agent, error)
	ExecutionRecords repository.AgentExecution
	Sessions         repository.Session
	Entries          repository.Entries
	Definitions      func(identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

type Service struct {
	agent            func(identity.AgentID) (*agentmodel.Agent, error)
	registerAgent    func(context.Context, *agentmodel.Agent) (*agentmodel.Agent, error)
	executionRecords repository.AgentExecution
	sessions         repository.Session
	entries          repository.Entries
	definitions      func(identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

// AgentView 读取内存实例的状态并组合执行详情，不复制带锁的 Agent 实例。
type AgentView struct {
	ID                 identity.AgentID
	SessionID          identity.SessionID
	DefinitionID       identity.AgentDefinitionID
	Kind               agentmodel.AgentKind
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Name               string                    `json:"name"`
	State              agentmodel.AgentState     `json:"state"`
	CurrentExecutionID identity.AgentExecutionID `json:"currentExecutionId"`
	CurrentExecution   *agentexecution.Execution `json:"currentExecution"`
	LatestExecution    *agentexecution.Execution `json:"latestExecution"`
}

// DescribeAgent 从已加载的角色定义读取展示名称；名称变化不改变持久化身份。
func (s *Service) DescribeAgent(value *agentmodel.Agent) (AgentView, error) {
	definition, err := s.definitions(value.DefinitionID)
	if err != nil {
		return AgentView{}, fmt.Errorf("load agent definition for display: %w", err)
	}
	state, executionID, updatedAt := value.State()
	return AgentView{
		ID:                 value.ID,
		SessionID:          value.SessionID,
		DefinitionID:       value.DefinitionID,
		Kind:               value.Kind,
		CreatedAt:          value.CreatedAt,
		UpdatedAt:          updatedAt,
		Name:               definition.Name,
		State:              state,
		CurrentExecutionID: executionID,
	}, nil
}

// NewService 使用装配层提供的完整依赖创建 Agent 查询服务。
func NewService(deps Dependencies) *Service {
	return &Service{
		agent:            deps.Agent,
		registerAgent:    deps.RegisterAgent,
		executionRecords: deps.ExecutionRecords,
		entries:          deps.Entries,
		definitions:      deps.Definitions,
		sessions:         deps.Sessions,
	}
}

// GetAgentView 返回 Agent 状态、当前执行与最近一次执行。
func (s *Service) GetAgentView(ctx context.Context, agentID identity.AgentID) (AgentView, error) {
	if ctx == nil {
		return AgentView{}, errors.New("agent query context is required")
	}
	value, err := s.agent(agentID)
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
	if view.CurrentExecutionID != "" {
		if view.LatestExecution != nil && view.LatestExecution.ID == view.CurrentExecutionID {
			view.CurrentExecution = view.LatestExecution
		} else {
			current, err := s.executionRecords.Get(ctx, view.CurrentExecutionID)
			if err != nil {
				return AgentView{}, fmt.Errorf("load current agent execution: %w", err)
			}
			view.CurrentExecution = &current
		}
	}
	return view, nil
}
