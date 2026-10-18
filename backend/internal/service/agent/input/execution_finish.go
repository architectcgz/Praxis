package input

import (
	"context"
	"errors"
	"praxis/internal/core/hook"
	"praxis/internal/fault"
	"praxis/internal/identity"
	agentmodel "praxis/internal/service/agent"

	"time"
)

const (
	TerminalExecutionEnded  = "execution_ended"
	TerminalRequestCanceled = "request_canceled"
)

// TerminalEvent 是事务提交后发布的最小终态事实。
type TerminalEvent struct {
	Kind           string
	SessionID      identity.SessionID
	AgentID        identity.AgentID
	ExecutionID    identity.AgentExecutionID
	Outcome        string
	FailureCode    agentmodel.ExecutionFailureCode
	FailureMessage string
}

type settleParams struct {
	ExecutionID    identity.AgentExecutionID
	Outcome        agentmodel.ExecutionOutcome
	FailureCode    agentmodel.ExecutionFailureCode
	FailureMessage string
}

// ConfirmRunning 将指定执行推进到 Running；重复确认不会改变开始时间。
// 已结束的执行不能重新启动，用户消息正文保留用于重复请求校验。
// executionID 来自 Coordinator 准入时已校验的持久化 Execution。
func (s *Service) ConfirmRunning(ctx context.Context, executionID identity.AgentExecutionID) error {
	if ctx == nil {
		return errors.New("task start context is required")
	}
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		task, err := s.records.Get(txCtx, executionID)
		if err != nil {
			return err
		}
		if task.Status == agentmodel.ExecutionRunning || task.Status == agentmodel.ExecutionEnding {
			return nil
		}
		if task.Status != agentmodel.ExecutionStarting && task.Status != agentmodel.ExecutionWaitingApproval {
			return fault.New(agentmodel.UnavailableCode, "")
		}
		if err := task.MarkRunning(s.clock.Now()); err != nil {
			return err
		}
		return s.records.Save(txCtx, task)
	})
}

// Settle 接收 Coordinator 的执行结果，原子更新 Execution、Agent 和控制命令。
// 返回控制命令仲裁后的持久化回合；重复结束返回已有终态，不再次发布事件。
// OperationHook 在事务外观察结算前后；回滚时只返回 Err，不把待提交 Outcome 当作最终结果。
func (s *Service) Settle(ctx context.Context, executionID identity.AgentExecutionID, outcome agentmodel.ExecutionOutcome, failureCode agentmodel.ExecutionFailureCode, failureMessage string) (result agentmodel.Execution, err error) {
	params := settleParams{
		ExecutionID:    executionID,
		Outcome:        outcome,
		FailureCode:    failureCode,
		FailureMessage: failureMessage,
	}
	if ctx == nil {
		return agentmodel.Execution{}, errors.New("task end context is required")
	}
	if !agentmodel.ValidExecutionOutcome(params.Outcome) {
		return agentmodel.Execution{}, fault.New(fault.InvalidRequest, "")
	}
	// 只预读不可变归属，Hook 不能在持有仓储事务锁时启动；状态与控制命令仍在事务内重新仲裁。
	observedExecution, err := s.records.Get(ctx, params.ExecutionID)
	if err != nil {
		s.logger.Errorf("operation=execution_end execution_id=%s status=failed outcome=%s error=%v", params.ExecutionID, params.Outcome, err)
		return agentmodel.Execution{}, err
	}
	ctx, scope := s.hookDispatcher.Start(ctx, hook.Operation{
		Identity: hook.Identity{
			SessionID:   observedExecution.SessionID,
			AgentID:     observedExecution.AgentID,
			ExecutionID: observedExecution.ID,
		},
		Kind: hook.OperationExecutionSettle,
		Name: "settle_execution",
	})
	defer func() { scope.End(hook.Result{Err: err, Outcome: string(result.Outcome)}) }()
	var endedExecution agentmodel.Execution
	var firstEnd bool
	err = s.tx.InTx(ctx, func(txCtx context.Context) error {
		task, err := s.records.Get(txCtx, params.ExecutionID)
		if err != nil {
			return err
		}
		endedExecution = task
		if task.Status == agentmodel.ExecutionEnded {
			return nil
		}
		controls, err := s.controls.ListOpenByAgent(txCtx, task.AgentID, 100)
		if err != nil {
			return err
		}
		// 结束事务之前提交的控制命令优先，取消与正常完成竞争时以提交顺序为准。
		for _, control := range controls {
			if control.TargetExecutionID != task.ID {
				continue
			}
			if params.FailureCode != agentmodel.ExecutionFailureRequestCanceled {
				params.Outcome = agentmodel.ExecutionPaused
			}
			if control.Kind == agentmodel.AgentControlCancel {
				params.Outcome = agentmodel.ExecutionInterrupted
			}
			params.FailureCode = agentmodel.ExecutionFailureRequestCanceled
			params.FailureMessage = ""
		}
		at := s.clock.Now()
		if task.Status == agentmodel.ExecutionStarting {
			if err := task.MarkRunning(at); err != nil {
				return err
			}
		}
		if task.Status == agentmodel.ExecutionRunning || task.Status == agentmodel.ExecutionWaitingApproval {
			if err := task.BeginEnding(at); err != nil {
				return err
			}
		}
		if err := task.End(params.Outcome, params.FailureCode, at); err != nil {
			return err
		}
		if params.Outcome == agentmodel.ExecutionFailed {
			task.FailureMessage = params.FailureMessage
		}
		agent, err := s.agents.Get(txCtx, task.AgentID)
		if err != nil {
			return err
		}
		if agent.CurrentExecutionID != task.ID {
			return fault.New(agentmodel.UnavailableCode, "")
		}
		if err := agent.EndExecution(params.Outcome, at); err != nil {
			return err
		}
		for index := range controls {
			control := &controls[index]
			if control.TargetExecutionID != task.ID {
				continue
			}
			if err := control.MarkApplied(at); err != nil {
				return err
			}
			if err := s.controls.Save(txCtx, *control); err != nil {
				return err
			}
		}
		if err := s.records.Save(txCtx, task); err != nil {
			return err
		}
		endedExecution = task
		firstEnd = true
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		if err := s.reconcileToolResults(txCtx, task.SessionID, task.AgentID, false); err != nil {
			return err
		}
		return s.nameSessionAfterAnswer(txCtx, task, params.Outcome, at)
	})
	if err != nil {
		s.logger.Errorf(
			"operation=execution_end request_id=%s session_id=%s agent_id=%s execution_id=%s status=failed outcome=%s error=%v",
			endedExecution.RequestID,
			endedExecution.SessionID,
			endedExecution.AgentID,
			params.ExecutionID,
			params.Outcome,
			err,
		)
		return agentmodel.Execution{}, err
	}
	if !firstEnd {
		return endedExecution, nil
	}
	s.logger.Infof(
		"operation=execution_end request_id=%s session_id=%s agent_id=%s execution_id=%s status=committed outcome=%s",
		endedExecution.RequestID,
		endedExecution.SessionID,
		endedExecution.AgentID,
		params.ExecutionID,
		params.Outcome,
	)
	committedExecution := endedExecution
	committedExecution.Config = endedExecution.Config.Clone()
	committedExecution.ContextSelection = endedExecution.ContextSelection.Clone()
	if hookErr := s.hookDispatcher.OnCommitted(context.WithoutCancel(ctx), hook.CommittedEvent{
		Identity: hook.Identity{
			SessionID:   endedExecution.SessionID,
			AgentID:     endedExecution.AgentID,
			ExecutionID: endedExecution.ID,
		},
		Execution: &hook.Execution{ID: committedExecution.ID, Outcome: string(committedExecution.Outcome), FailureCode: string(committedExecution.FailureCode)},
	}); hookErr != nil {
		s.logger.Errorf("task committed hook failed: %v", hookErr)
	}
	// 每轮仅发布一个终态事件；取消原因来自持久化控制命令，不由 outcome 推断。
	if s.eventObserver != nil {
		kind := TerminalExecutionEnded
		if endedExecution.FailureCode == agentmodel.ExecutionFailureRequestCanceled {
			kind = TerminalRequestCanceled
		}
		s.eventObserver(TerminalEvent{
			Kind:           kind,
			SessionID:      endedExecution.SessionID,
			AgentID:        endedExecution.AgentID,
			ExecutionID:    endedExecution.ID,
			Outcome:        string(endedExecution.Outcome),
			FailureCode:    endedExecution.FailureCode,
			FailureMessage: endedExecution.FailureMessage,
		})
	}
	return endedExecution, nil
}

func (s *Service) nameSessionAfterAnswer(
	ctx context.Context,
	task agentmodel.Execution,
	outcome agentmodel.ExecutionOutcome,
	at time.Time,
) error {
	if outcome != agentmodel.ExecutionCompleted && outcome != agentmodel.ExecutionYielded {
		return nil
	}
	stream, err := s.messages.LoadMessages(ctx, task.SessionID, task.AgentID, 0)
	if err != nil {
		return err
	}
	for _, message := range stream.Messages {
		if message.ExecutionID != task.ID.String() || message.Role != "user" {
			continue
		}
		session, err := s.sessions.Get(ctx, task.SessionID)
		if err != nil {
			return err
		}
		if session.Title == "" {
			session.NameFromInput(message.TextContent(), at)
			if err := s.sessions.Save(ctx, session); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

// WaitForApproval 提交非终态等待状态，控制命令仍由结算事务仲裁。
func (s *Service) WaitForApproval(ctx context.Context, id identity.AgentExecutionID) error {
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		value, err := s.records.Get(txCtx, id)
		if err != nil {
			return err
		}
		if err := value.WaitForApproval(); err != nil {
			return err
		}
		return s.records.Save(txCtx, value)
	})
}
