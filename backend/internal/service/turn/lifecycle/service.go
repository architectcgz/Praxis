// Package lifecycle 负责 Turn 开始确认和结束状态的持久化。
package lifecycle

import (
	runtimecontract "praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	turnmodel "praxis/internal/core/turn"
	workflowmodel "praxis/internal/core/workflow"

	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"praxis/internal/logging"
	"praxis/internal/repository"
	"praxis/internal/system"
)

type QueueStarter interface {
	StartNextQueuedWork(context.Context, contracts.AgentID) (bool, error)
}

type Config struct {
	Transactions  repository.TxRunner
	Sessions      repository.SessionRepository
	Agents        repository.SessionAgentRepository
	Turns         repository.TurnRepository
	QueuedWork    repository.QueuedWorkRepository
	Controls      repository.AgentControlCommandRepository
	Messages      repository.MessageLoader
	Clock         system.Clock
	Logger        *logging.Logger
	EventObserver runtimecontract.AgentEventObserver
}

type Service struct {
	tx            repository.TxRunner
	sessions      repository.SessionRepository
	agents        repository.SessionAgentRepository
	turns         repository.TurnRepository
	queuedWork    repository.QueuedWorkRepository
	controls      repository.AgentControlCommandRepository
	messages      repository.MessageLoader
	clock         system.Clock
	logger        *logging.Logger
	eventObserver runtimecontract.AgentEventObserver
	queueMu       sync.RWMutex
	queue         QueueStarter
}

type Params struct {
	TurnID         contracts.TurnID
	Outcome        turnmodel.TurnOutcome
	FailureCode    contracts.TurnFailureCode
	FailureMessage string
}

func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions": config.Transactions,
		"sessions":     config.Sessions,
		"agents":       config.Agents,
		"turns":        config.Turns,
		"queued work":  config.QueuedWork,
		"controls":     config.Controls,
		"messages":     config.Messages,
	} {
		if value == nil {
			return nil, fmt.Errorf("turn lifecycle service %s is required", name)
		}
	}
	return &Service{
		tx:            config.Transactions,
		sessions:      config.Sessions,
		agents:        config.Agents,
		turns:         config.Turns,
		queuedWork:    config.QueuedWork,
		controls:      config.Controls,
		messages:      config.Messages,
		clock:         system.ClockOrDefault(config.Clock),
		logger:        logging.NewFactory().Ensure(config.Logger),
		eventObserver: config.EventObserver,
	}, nil
}

// SetQueueStarter 注入队列推进器，普通完成后继续 FIFO，取消后不自动推进。
func (s *Service) SetQueueStarter(starter QueueStarter) error {
	if starter == nil {
		return errors.New("queue starter is required")
	}
	s.queueMu.Lock()
	s.queue = starter
	s.queueMu.Unlock()
	return nil
}

// StartRuntimeTurn 将指定执行推进到 Running；重复确认不会改变开始时间。
// 已结束的执行不能重新启动，输入正文保留用于重复请求校验。
// turnID 来自 runtime 准入时已校验的持久化 Turn。
func (s *Service) StartRuntimeTurn(ctx context.Context, turnID contracts.TurnID) error {
	if ctx == nil {
		return errors.New("turn start context is required")
	}
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		turn, err := s.turns.Get(txCtx, turnID)
		if err != nil {
			return err
		}
		if turn.Status == turnmodel.TurnRunning || turn.Status == turnmodel.TurnEnding {
			return nil
		}
		if turn.Status != turnmodel.TurnStarting {
			return contracts.New(contracts.AgentUnavailable, "")
		}
		if err := turn.MarkRunning(s.clock.Now()); err != nil {
			return err
		}
		return s.turns.Save(txCtx, turn)
	})
}

// EndRuntimeTurn 接收 runtime 的执行结果；事务提交后才发布终态事件。
func (s *Service) EndRuntimeTurn(ctx context.Context, turnID contracts.TurnID, outcome turnmodel.TurnOutcome, failureCode contracts.TurnFailureCode, failureMessage string) error {
	return s.End(ctx, Params{
		TurnID:         turnID,
		Outcome:        outcome,
		FailureCode:    contracts.TurnFailureCode(strings.TrimSpace(string(failureCode))),
		FailureMessage: strings.TrimSpace(failureMessage),
	})
}

// End 在同一事务中更新 Turn、Agent、队列项和控制命令；重复结束不再次发布事件。
func (s *Service) End(ctx context.Context, params Params) error {
	if ctx == nil {
		return errors.New("turn end context is required")
	}
	if !knownOutcome(params.Outcome) {
		return contracts.New(contracts.InvalidRequest, "")
	}
	var endedTurn turnmodel.Turn
	var firstEnd bool
	var advanceQueue bool
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		turn, err := s.turns.Get(txCtx, params.TurnID)
		if err != nil {
			return err
		}
		endedTurn = turn
		if turn.Status == turnmodel.TurnEnded {
			return nil
		}
		controls, err := s.controls.ListOpenByAgent(txCtx, turn.AgentID, 100)
		if err != nil {
			return err
		}
		// 结束事务之前提交的控制命令优先，取消与正常完成竞争时以提交顺序为准。
		for _, control := range controls {
			if control.TargetTurnID != turn.ID {
				continue
			}
			if params.FailureCode != contracts.TurnFailureRequestCanceled {
				params.Outcome = turnmodel.TurnPaused
			}
			if control.Kind == workflowmodel.AgentControlCancel {
				params.Outcome = turnmodel.TurnInterrupted
			}
			params.FailureCode = contracts.TurnFailureRequestCanceled
			params.FailureMessage = ""
		}
		at := s.clock.Now()
		if turn.Status == turnmodel.TurnStarting {
			if err := turn.MarkRunning(at); err != nil {
				return err
			}
		}
		if turn.Status == turnmodel.TurnRunning {
			if err := turn.BeginEnding(at); err != nil {
				return err
			}
		}
		if err := turn.End(params.Outcome, params.FailureCode, at); err != nil {
			return err
		}
		if params.Outcome == turnmodel.TurnFailed {
			turn.FailureMessage = params.FailureMessage
		}
		agent, err := s.agents.Get(txCtx, turn.AgentID)
		if err != nil {
			return err
		}
		if agent.CurrentTurnID != turn.ID {
			return contracts.New(contracts.AgentUnavailable, "")
		}
		if err := agent.EndTurn(params.Outcome, at); err != nil {
			return err
		}
		if turn.WorkItemID != "" {
			work, err := s.queuedWork.Get(txCtx, turn.WorkItemID)
			if err != nil {
				return err
			}
			if work.AgentID != agent.ID || work.TurnID != turn.ID {
				return contracts.New(contracts.AgentUnavailable, "")
			}
			if err := work.End(turn.ID, params.Outcome, params.FailureCode, at); err != nil {
				return err
			}
			if err := s.queuedWork.Save(txCtx, work); err != nil {
				return err
			}
			advanceQueue = params.Outcome == turnmodel.TurnCompleted || params.Outcome == turnmodel.TurnYielded
		}
		for index := range controls {
			control := &controls[index]
			if control.TargetTurnID != turn.ID {
				continue
			}
			if err := control.MarkApplied(at); err != nil {
				return err
			}
			if err := s.controls.Save(txCtx, *control); err != nil {
				return err
			}
		}
		if err := s.turns.Save(txCtx, turn); err != nil {
			return err
		}
		endedTurn = turn
		firstEnd = true
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		return s.nameSessionAfterAnswer(txCtx, turn, params.Outcome, at)
	})
	if err != nil {
		s.logger.Errorf(
			"operation=turn_end request_id=%s session_id=%s agent_id=%s turn_id=%s status=failed outcome=%s error=%v",
			endedTurn.RequestID,
			endedTurn.SessionID,
			endedTurn.AgentID,
			params.TurnID,
			params.Outcome,
			err,
		)
		return err
	}
	if !firstEnd {
		return nil
	}
	s.logger.Infof(
		"operation=turn_end request_id=%s session_id=%s agent_id=%s turn_id=%s status=committed outcome=%s",
		endedTurn.RequestID,
		endedTurn.SessionID,
		endedTurn.AgentID,
		params.TurnID,
		params.Outcome,
	)
	// 每轮仅发布一个终态事件；取消原因来自持久化控制命令，不由 outcome 推断。
	if s.eventObserver != nil {
		kind := runtimecontract.AgentEventTurnEnded
		if endedTurn.FailureCode == contracts.TurnFailureRequestCanceled {
			kind = runtimecontract.AgentEventRequestCanceled
		}
		s.eventObserver(runtimecontract.AgentEvent{
			Kind:           kind,
			SessionID:      endedTurn.SessionID,
			AgentID:        endedTurn.AgentID,
			TurnID:         endedTurn.ID,
			Outcome:        endedTurn.Outcome,
			FailureCode:    endedTurn.FailureCode,
			FailureMessage: endedTurn.FailureMessage,
		})
	}
	if !advanceQueue {
		return nil
	}
	s.queueMu.RLock()
	queue := s.queue
	s.queueMu.RUnlock()
	if queue != nil {
		_, _ = queue.StartNextQueuedWork(context.WithoutCancel(ctx), endedTurn.AgentID)
	}
	return nil
}

func knownOutcome(outcome turnmodel.TurnOutcome) bool {
	switch outcome {
	case turnmodel.TurnCompleted, turnmodel.TurnYielded, turnmodel.TurnPaused, turnmodel.TurnFailed, turnmodel.TurnInterrupted:
		return true
	default:
		return false
	}
}

func (s *Service) nameSessionAfterAnswer(
	ctx context.Context,
	turn turnmodel.Turn,
	outcome turnmodel.TurnOutcome,
	at time.Time,
) error {
	if turn.Reason != turnmodel.TurnUserInput ||
		(outcome != turnmodel.TurnCompleted && outcome != turnmodel.TurnYielded) {
		return nil
	}
	stream, err := s.messages.LoadMessages(ctx, turn.SessionID, turn.AgentID, 0)
	if err != nil {
		return err
	}
	for _, message := range stream.Messages {
		if message.TurnID != turn.ID.String() || message.Role != "user" {
			continue
		}
		var content strings.Builder
		for _, block := range message.Blocks {
			if block.Kind == "text" {
				content.WriteString(block.Text)
			}
		}
		session, err := s.sessions.Get(ctx, turn.SessionID)
		if err != nil {
			return err
		}
		if session.Title == "" {
			session.NameFromInput(content.String(), at)
			if err := s.sessions.Save(ctx, session); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}
