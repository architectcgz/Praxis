// Package queue owns durable independent work admission and FIFO execution start.
package queue

import (
	agentmodel "praxis/internal/agent"
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"
	workflowmodel "praxis/internal/workflow"

	"context"
	"errors"
	"fmt"
	"strings"

	"praxis/internal/repository"
	runtimecontract "praxis/internal/runtime"
	"praxis/internal/system"
)

// InputFactory freezes the current context, policy and model selection into an
// immutable execution input snapshot.
type InputFactory interface {
	MaterializeExecutionInput(context.Context, agentmodel.Agent, string, string, string, string) (executionmodel.ExecutionInputSnapshot, error)
}

// RuntimeActivator notifies the scheduler after an execution creation commits.
type RuntimeActivator interface {
	TryActivate(context.Context, executionmodel.AgentExecution, runtimecontract.ExecutionLifecycle) error
}

type Config struct {
	Transactions repository.TxRunner
	Agents       repository.SessionAgentRepository
	Executions   repository.AgentExecutionRepository
	QueuedWork   repository.QueuedWorkRepository
	Inputs       InputFactory
	Activator    RuntimeActivator
	Lifecycle    runtimecontract.ExecutionLifecycle
	Clock        system.Clock
	IDs          system.IDGenerator
}

type Service struct {
	tx         repository.TxRunner
	agents     repository.SessionAgentRepository
	executions repository.AgentExecutionRepository
	queuedWork repository.QueuedWorkRepository
	inputs     InputFactory
	activator  RuntimeActivator
	lifecycle  runtimecontract.ExecutionLifecycle
	clock      system.Clock
	ids        system.IDGenerator
}

type EnqueueParams struct {
	ID        contracts.WorkItemID
	RequestID contracts.RequestID
	AgentID   contracts.AgentID
	Prompt    string
}

type EnqueueResult struct {
	Work            workflowmodel.QueuedWork
	ExistingWork    bool
	ActivationError string
}

type StartResult struct {
	Work            workflowmodel.QueuedWork
	Execution       executionmodel.AgentExecution
	Started         bool
	ActivationError string
}

func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions":            config.Transactions,
		"agents":                  config.Agents,
		"executions":              config.Executions,
		"queued work":             config.QueuedWork,
		"execution input factory": config.Inputs,
	} {
		if value == nil {
			return nil, fmt.Errorf("execution queue service %s is required", name)
		}
	}
	return &Service{tx: config.Transactions, agents: config.Agents, executions: config.Executions, queuedWork: config.QueuedWork, inputs: config.Inputs, activator: config.Activator, lifecycle: config.Lifecycle, clock: system.ClockOrDefault(config.Clock), ids: system.IDsOrDefault(config.IDs)}, nil
}

func (s *Service) EnqueueWork(ctx context.Context, params EnqueueParams) (EnqueueResult, error) {
	if ctx == nil {
		return EnqueueResult{}, errors.New("enqueue work context is required")
	}
	if strings.TrimSpace(params.ID.String()) == "" || strings.TrimSpace(params.RequestID.String()) == "" || strings.TrimSpace(params.AgentID.String()) == "" || strings.TrimSpace(params.Prompt) == "" {
		return EnqueueResult{}, contracts.New(contracts.InvalidRequest, "")
	}
	result := EnqueueResult{}
	startEligible := false
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.queuedWork.Get(txCtx, params.ID)
		if err == nil {
			if existing.AgentID != params.AgentID || existing.Prompt != strings.TrimSpace(params.Prompt) {
				return contracts.New(contracts.InvalidRequest, "")
			}
			result.Work, result.ExistingWork = existing, true
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		sequence, err := s.queuedWork.NextSequence(txCtx, agent.ID)
		if err != nil {
			return err
		}
		at := s.clock.Now()
		work, err := workflowmodel.NewQueuedWork(params.ID, agent.SessionID, agent.ID, sequence, params.Prompt, at)
		if err != nil {
			return err
		}
		if err := s.queuedWork.Save(txCtx, work); err != nil {
			return err
		}
		result.Work = work
		startEligible = agent.State.Startable()
		return nil
	})
	if err != nil || result.ExistingWork || !startEligible {
		return result, err
	}
	started, err := s.StartNextQueuedWork(context.WithoutCancel(ctx), params.AgentID)
	if err != nil {
		result.ActivationError = err.Error()
	} else {
		result.ActivationError = started.ActivationError
	}
	return result, nil
}

func (s *Service) StartNextQueuedWork(ctx context.Context, agentID contracts.AgentID) (StartResult, error) {
	if ctx == nil {
		return StartResult{}, errors.New("start queued work context is required")
	}
	if strings.TrimSpace(agentID.String()) == "" {
		return StartResult{}, contracts.New(contracts.InvalidRequest, "")
	}
	result := StartResult{}
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		agent, err := s.agents.Get(txCtx, agentID)
		if err != nil {
			return err
		}
		if !agent.State.Startable() {
			return nil
		}
		active, err := s.executions.CountActiveBySession(txCtx, agent.SessionID)
		if err != nil {
			return err
		}
		if active >= 1 {
			return nil
		}
		work, err := s.queuedWork.FindNextPendingByAgent(txCtx, agent.ID)
		if errors.Is(err, contracts.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		input, err := s.inputs.MaterializeExecutionInput(txCtx, agent, "", "", "", work.Prompt)
		if err != nil {
			return err
		}
		at := s.clock.Now()
		execution, err := executionmodel.NewQueuedWorkExecution(
			contracts.AgentExecutionID(s.ids.New("execution")), agent.SessionID, agent.ID,
			work.ID, work.Prompt, input, at,
		)
		if err != nil {
			return err
		}
		if err := work.Start(execution.ID, at); err != nil {
			return err
		}
		if err := agent.Start(execution.ID, at); err != nil {
			return err
		}
		if err := s.executions.Save(txCtx, execution); err != nil {
			return err
		}
		if err := s.queuedWork.Save(txCtx, work); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Work, result.Execution, result.Started = work, execution, true
		return nil
	})
	if err != nil || !result.Started || s.activator == nil || s.lifecycle == nil {
		return result, err
	}
	if err := s.activator.TryActivate(context.WithoutCancel(ctx), result.Execution, s.lifecycle); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}
