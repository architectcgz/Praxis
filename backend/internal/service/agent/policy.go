// Package agent 负责 Agent 状态读取、安全策略和结构化结果写入用例。
package agent

import (
	agentmodel "praxis/internal/agent"
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"
	securitymodel "praxis/internal/security"
	workflowmodel "praxis/internal/workflow"

	"context"
	"errors"
	"fmt"
	"reflect"

	"praxis/internal/repository"
	runtimecontract "praxis/internal/runtime"
	"praxis/internal/system"
)

type Config struct {
	Transactions repository.TxRunner
	Agents       repository.SessionAgentRepository
	Policies     repository.AgentSecurityPolicyRepository
	Executions   repository.AgentExecutionRepository
	Waits        repository.WaitConditionRepository
	Controls     repository.AgentControlCommandRepository
	Messages     func(context.Context, contracts.SessionID, contracts.AgentID, int) ([]runtimecontract.AgentSessionMessage, error)
	Clock        system.Clock
}

type Service struct {
	tx         repository.TxRunner
	agents     repository.SessionAgentRepository
	policies   repository.AgentSecurityPolicyRepository
	executions repository.AgentExecutionRepository
	waits      repository.WaitConditionRepository
	controls   repository.AgentControlCommandRepository
	messages   func(context.Context, contracts.SessionID, contracts.AgentID, int) ([]runtimecontract.AgentSessionMessage, error)
	clock      system.Clock
}

// AgentView 是单个 Agent 的持久化详情视图。
type AgentView struct {
	Agent           agentmodel.Agent
	ActiveExecution *executionmodel.AgentExecution
	Executions      []executionmodel.AgentExecution
	Waits           []workflowmodel.WaitCondition
	Controls        []workflowmodel.AgentControlCommand
}

type UpdatePolicyParams struct {
	RequestID        contracts.RequestID
	AgentID          contracts.AgentID
	ExpectedRevision uint64
	Policy           securitymodel.AgentSecurityPolicy
}

type UpdatePolicyResult struct {
	AgentID         contracts.AgentID
	Policy          securitymodel.AgentSecurityPolicy
	ExistingRequest bool
}

func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions":      config.Transactions,
		"agents":            config.Agents,
		"security policies": config.Policies,
		"executions":        config.Executions,
		"wait conditions":   config.Waits,
		"control requests":  config.Controls,
	} {
		if value == nil {
			return nil, fmt.Errorf("agent service %s is required", name)
		}
	}
	if config.Messages == nil {
		return nil, errors.New("agent service messages is required")
	}
	return &Service{
		tx:         config.Transactions,
		agents:     config.Agents,
		policies:   config.Policies,
		executions: config.Executions,
		waits:      config.Waits,
		controls:   config.Controls,
		messages:   config.Messages,
		clock:      system.ClockOrDefault(config.Clock),
	}, nil
}

// UpdatePolicy 在一个事务内写入安全策略版本、Agent 版本指针和幂等结果。
func (s *Service) UpdatePolicy(ctx context.Context, params UpdatePolicyParams) (UpdatePolicyResult, error) {
	if ctx == nil {
		return UpdatePolicyResult{}, errors.New("update agent security policy context is required")
	}
	if params.RequestID == "" || params.AgentID == "" || params.ExpectedRevision == 0 {
		return UpdatePolicyResult{}, contracts.New(contracts.InvalidRequest, "")
	}
	if err := params.Policy.Validate(); err != nil {
		return UpdatePolicyResult{}, contracts.New(contracts.InvalidRequest, "")
	}
	result := UpdatePolicyResult{}
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, found, err := s.policies.GetByRevision(txCtx, params.AgentID, params.Policy.Revision)
		if err != nil {
			return err
		}
		if found {
			if !reflect.DeepEqual(existing, params.Policy) {
				return contracts.ErrRequestConflict
			}
			result = UpdatePolicyResult{AgentID: params.AgentID, Policy: existing, ExistingRequest: true}
			return nil
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		if agent.SecurityPolicyRevision != params.ExpectedRevision || params.Policy.Revision != params.ExpectedRevision+1 {
			return contracts.ErrRevisionConflict
		}
		if err := s.policies.Save(txCtx, agent.ID, params.Policy); err != nil {
			return err
		}
		agent.SecurityPolicyRevision = params.Policy.Revision
		agent.UpdatedAt = s.clock.Now().UTC()
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result = UpdatePolicyResult{AgentID: agent.ID, Policy: params.Policy}
		return nil
	})
	return result, err
}

// GetAgentView 返回 Agent、执行、等待和控制状态的持久化详情。
func (s *Service) GetAgentView(ctx context.Context, agentID contracts.AgentID, limit int) (AgentView, error) {
	if ctx == nil {
		return AgentView{}, errors.New("agent query context is required")
	}
	if agentID == "" {
		return AgentView{}, invalidAgentQuery("agent id is required")
	}
	if s.executions == nil || s.waits == nil || s.controls == nil {
		return AgentView{}, errors.New("agent detail query is unavailable")
	}
	agent, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return AgentView{}, err
	}
	active, err := s.executions.GetActiveByAgent(ctx, agentID)
	if errors.Is(err, contracts.ErrNotFound) {
		active = executionmodel.AgentExecution{}
	} else if err != nil {
		return AgentView{}, fmt.Errorf("load active agent execution: %w", err)
	}
	executions, err := s.executions.ListByAgent(ctx, agentID, limit)
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
		Agent:      agent,
		Executions: executions,
		Waits:      waits,
		Controls:   controls,
	}
	if active.ID != "" {
		activeCopy := active
		view.ActiveExecution = &activeCopy
	}
	return view, nil
}

// ListAgentMessages 读取 Agent transcript 消息。
func (s *Service) ListAgentMessages(ctx context.Context, agentID contracts.AgentID, limit int) ([]runtimecontract.AgentSessionMessage, error) {
	if ctx == nil {
		return nil, errors.New("agent message context is required")
	}
	if agentID == "" {
		return nil, invalidAgentQuery("agent id is required")
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

// invalidAgentQuery 将非法读取请求转换为对外稳定的业务错误。
func invalidAgentQuery(message string) error {
	return contracts.New(contracts.InvalidRequest, message)
}
