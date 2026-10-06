// Package queue 负责独立任务入队及 FIFO 回合启动。
package queue

import (
	runtimecontract "praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	turnmodel "praxis/internal/core/turn"
	workflowmodel "praxis/internal/core/workflow"
	"praxis/internal/repository"
	"praxis/internal/system"

	"context"
	"errors"
	"fmt"
)

// InputFactory 将当前上下文、策略和模型选择冻结为不可变的回合输入。
type InputFactory interface {
	MaterializeTurnInput(context.Context, agentmodel.Agent, string, string, string, string, string) (turnmodel.InputSnapshot, error)
}

// RuntimeActivator 在 queued turn 创建提交后通知 Agent runtime。
type RuntimeActivator interface {
	Activate(context.Context, turnmodel.Turn, runtimecontract.TurnLifecycle) error
}

type Config struct {
	Transactions    repository.TxRunner
	Agents          repository.SessionAgentRepository
	Turns           repository.TurnRepository
	QueuedWork      repository.QueuedWorkRepository
	SessionMessages repository.SessionMessageRepository
	AgentMessages   repository.AgentMessageRepository
	Inputs          InputFactory
	Activator       RuntimeActivator
	Lifecycle       runtimecontract.TurnLifecycle
	Clock           system.Clock
	IDs             system.IDGenerator
}

type Service struct {
	tx              repository.TxRunner
	agents          repository.SessionAgentRepository
	turns           repository.TurnRepository
	queuedWork      repository.QueuedWorkRepository
	sessionMessages repository.SessionMessageRepository
	agentMessages   repository.AgentMessageRepository
	inputs          InputFactory
	activator       RuntimeActivator
	lifecycle       runtimecontract.TurnLifecycle
	clock           system.Clock
	ids             system.IDGenerator
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
	Turn            turnmodel.Turn
	Started         bool
	ActivationError string
}

func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions":       config.Transactions,
		"agents":             config.Agents,
		"turns":              config.Turns,
		"queued work":        config.QueuedWork,
		"session messages":   config.SessionMessages,
		"agent messages":     config.AgentMessages,
		"turn input factory": config.Inputs,
	} {
		if value == nil {
			return nil, fmt.Errorf("turn queue service %s is required", name)
		}
	}
	return &Service{
		tx: config.Transactions, agents: config.Agents, turns: config.Turns,
		queuedWork: config.QueuedWork, sessionMessages: config.SessionMessages,
		agentMessages: config.AgentMessages, inputs: config.Inputs,
		activator: config.Activator, lifecycle: config.Lifecycle,
		clock: system.ClockOrDefault(config.Clock), ids: system.IDsOrDefault(config.IDs),
	}, nil
}

// EnqueueWork 使用入口已校验的 ID 和规范化 Prompt 持久化队列项，重复请求返回已有结果。
func (s *Service) EnqueueWork(ctx context.Context, params EnqueueParams) (EnqueueResult, error) {
	if ctx == nil {
		return EnqueueResult{}, errors.New("enqueue work context is required")
	}
	result := EnqueueResult{}
	startEligible := false
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		at := s.clock.Now().UTC()
		inputMessage := sessionmodel.MessageData{
			ID: "input:" + params.RequestID.String(), RequestID: params.RequestID.String(),
			Role: sessionmodel.RoleUser, AuthorKind: sessionmodel.AuthorUser,
			Blocks: []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: params.Prompt}}, CreatedAt: at,
		}
		if err := s.appendQueuedInput(txCtx, agent, inputMessage); err != nil {
			return err
		}
		existingRequest, err := s.queuedWork.FindByRequest(txCtx, agent.ID, params.RequestID)
		if err == nil {
			if existingRequest.InputMessageID != inputMessage.ID {
				return contracts.ErrRequestConflict
			}
			result.Work, result.ExistingWork = existingRequest, true
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		if _, err := s.turns.FindByRequest(txCtx, agent.ID, params.RequestID); err == nil {
			return contracts.ErrRequestConflict
		} else if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		existing, err := s.queuedWork.Get(txCtx, params.ID)
		if err == nil {
			if existing.RequestID != params.RequestID || existing.InputMessageID != inputMessage.ID {
				return contracts.ErrRequestConflict
			}
			result.Work, result.ExistingWork = existing, true
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		sequence, err := s.queuedWork.NextSequence(txCtx, agent.ID)
		if err != nil {
			return err
		}
		work, err := workflowmodel.NewQueuedWork(
			params.ID, agent.SessionID, agent.ID, params.RequestID, inputMessage.ID, sequence, at,
		)
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

// StartNextQueuedWork 按 FIFO 启动可执行任务；agentID 来自已校验请求或持久化的结算结果。
func (s *Service) StartNextQueuedWork(ctx context.Context, agentID contracts.AgentID) (StartResult, error) {
	if ctx == nil {
		return StartResult{}, errors.New("start queued work context is required")
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
		active, err := s.turns.CountActiveBySession(txCtx, agent.SessionID)
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
		input, err := s.inputs.MaterializeTurnInput(txCtx, agent, "", "", "", "", work.InputMessageID)
		if err != nil {
			return err
		}
		at := s.clock.Now()
		turn, err := turnmodel.NewQueuedWorkTurn(
			contracts.TurnID(s.ids.New("turn")), agent.SessionID, agent.ID,
			work.ID, work.RequestID, input, at,
		)
		if err != nil {
			return err
		}
		if err := work.Start(turn.ID, at); err != nil {
			return err
		}
		if err := agent.Start(turn.ID, at); err != nil {
			return err
		}
		if err := s.turns.Save(txCtx, turn); err != nil {
			return err
		}
		if err := s.queuedWork.Save(txCtx, work); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Work, result.Turn, result.Started = work, turn, true
		return nil
	})
	if err != nil || !result.Started || s.activator == nil || s.lifecycle == nil {
		return result, err
	}
	if err := s.activator.Activate(context.WithoutCancel(ctx), result.Turn, s.lifecycle); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

func (s *Service) appendQueuedInput(
	ctx context.Context,
	agent agentmodel.Agent,
	data sessionmodel.MessageData,
) error {
	if agent.CanReadSessionContext() {
		_, err := s.sessionMessages.Append(ctx, sessionmodel.SessionMessage{SessionID: agent.SessionID, Data: data})
		return err
	}
	_, err := s.agentMessages.Append(ctx, agentmodel.AgentMessage{AgentID: agent.ID, Data: data})
	return err
}
