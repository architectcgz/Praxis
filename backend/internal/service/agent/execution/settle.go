package execution

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/hook"
	"praxis/internal/fault"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
	"time"
)

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
	err := s.update(ctx, executionID, func(task *agentmodel.Execution) error {
		if task.Status == agentmodel.ExecutionRunning || task.Status == agentmodel.ExecutionEnding {
			return nil
		}
		if task.Status != agentmodel.ExecutionStarting && task.Status != agentmodel.ExecutionWaitingApproval {
			return fault.New(agentmodel.UnavailableCode, "")
		}
		if err := task.MarkRunning(s.clock.Now()); err != nil {
			return err
		}
		return nil
	})
	if err == nil {
		execution, readErr := s.records.Get(ctx, executionID)
		if readErr != nil {
			s.logger.Errorf("execution timing lookup failed: %v", readErr)
		} else {
			s.publishExecutionTiming(execution)
		}
	}
	return err
}

// Settle 仲裁控制意图并保存 Execution 终态，Agent 与控制命令状态由查询派生。
// 返回控制命令仲裁后的持久化回合；重复结束返回已有终态，不再次发布事件。
// OperationHook 观察结算前后；条件写入失败时不把待提交 Outcome 当作最终结果。
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
	// Hook 在存储锁外执行；条件写入冲突时重新读取状态及控制命令再仲裁。
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
	requested := params
	for {
		params = requested
		err = func(txCtx context.Context) error {
			task, err := s.records.Get(txCtx, params.ExecutionID)
			if err != nil {
				return err
			}
			endedExecution = task
			if task.Status == agentmodel.ExecutionEnded {
				return nil
			}
			controls, err := s.controls.ListOpenByAgent(txCtx, task.AgentID, 0)
			if err != nil {
				return err
			}
			// 终态之前提交的控制命令优先；之后的新命令使本次条件写入冲突。
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
			if err := s.records.Save(txCtx, task); err != nil {
				return err
			}
			endedExecution = task
			firstEnd = true
			return nil
		}(ctx)
		if !errors.Is(err, fault.ErrRevisionConflict) {
			break
		}
		if err = ctx.Err(); err != nil {
			break
		}
	}
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
	// 终态已提交，附属命名失败只记录日志；重复结算可以继续完成命名。
	defer func() {
		if nameErr := s.nameSessionAfterAnswer(context.WithoutCancel(ctx), endedExecution, endedExecution.Outcome, endedExecution.EndedAt); nameErr != nil {
			s.logger.Errorf("name settled session execution_id=%s error=%v", endedExecution.ID, nameErr)
		}
	}()
	if firstEnd {
		s.publishExecutionTiming(endedExecution)
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
	event := hook.ExecutionEvent{
		Identity: hook.Identity{
			SourceTaskID: endedExecution.SourceTaskID,
			SessionID:    endedExecution.SessionID,
			AgentID:      endedExecution.AgentID,
			ExecutionID:  endedExecution.ID,
		},
		Outcome:     string(endedExecution.Outcome),
		FailureCode: string(endedExecution.FailureCode),
	}
	if endedExecution.Outcome == agentmodel.ExecutionPaused {
		if hookErr := s.hookDispatcher.ExecutionPaused(context.WithoutCancel(ctx), event); hookErr != nil {
			s.logger.Errorf("execution paused hook failed: %v", hookErr)
		}
	}
	if hookErr := s.hookDispatcher.ExecutionCommitted(context.WithoutCancel(ctx), event); hookErr != nil {
		s.logger.Errorf("execution committed hook failed: %v", hookErr)
	}
	// 每轮仅发布一个终态事件；取消原因来自持久化控制命令，不由 outcome 推断。
	if s.eventObserver != nil {
		kind := EventExecutionEnded
		if endedExecution.FailureCode == agentmodel.ExecutionFailureRequestCanceled {
			kind = EventRequestCanceled
		}
		s.eventObserver(Event{
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
	owner, err := s.agents.Get(ctx, task.AgentID)
	if err != nil {
		return err
	}
	message, err := s.entries.Get(ctx, agentservice.EntryStream(owner), task.InputEntryID)
	if err != nil {
		return err
	}
	return s.sessions.NameFromInput(ctx, task.SessionID, message.TextContent(), at)
}

// WaitForApproval 提交非终态等待状态，控制命令仍由结算仲裁。
func (s *Service) WaitForApproval(ctx context.Context, id identity.AgentExecutionID) error {
	return s.update(ctx, id, func(value *agentmodel.Execution) error {
		return value.WaitForApproval()
	})
}
