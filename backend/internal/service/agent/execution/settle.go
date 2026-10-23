package execution

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/errorcode"
	"time"

	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
)

// ConfirmRunning 区分首次启动与重复确认；审批等待不改变运行状态。
// 首次启动记录开始时间，已结束的执行不能重新启动。
// executionID 来自 Coordinator 准入时已校验的持久化 Execution。
func (s *Service) ConfirmRunning(ctx context.Context, executionID identity.AgentExecutionID) error {
	if ctx == nil {
		return errors.New("task start context is required")
	}
	err := s.update(ctx, executionID, func(task *agentexecution.Execution) error {
		switch task.Status {
		case agentexecution.ExecutionStarting:
			task.Start(s.clock.Now())
		case agentexecution.ExecutionRunning, agentexecution.ExecutionEnding:
			return nil
		default:
			return agentmodel.ErrUnavailable
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

// Settle 保存所属执行者确定的实际结果，不根据控制命令改写完成结果。
// 写入失败由 Coordinator 保留占用和结果后重试；重复结算返回已有终态，不再次发布事件。
func (s *Service) Settle(ctx context.Context, executionID identity.AgentExecutionID, outcome agentexecution.ExecutionOutcome, failureCode agentexecution.ExecutionFailureCode, failureMessage string) (result agentexecution.Execution, err error) {
	if ctx == nil {
		return agentexecution.Execution{}, errors.New("task end context is required")
	}
	endedExecution, err := s.records.Get(ctx, executionID)
	if err != nil {
		s.logger.Errorf("operation=execution_end execution_id=%s status=failed outcome=%s error=%v", executionID, outcome, err)
		return agentexecution.Execution{}, err
	}
	// Hook 在存储锁外观察本次结算；通知不能改变执行者已确定的结果。
	ctx, scope := s.hookDispatcher.Start(ctx, hook.Operation{
		Identity: hook.Identity{
			SessionID:   endedExecution.SessionID,
			AgentID:     endedExecution.AgentID,
			ExecutionID: endedExecution.ID,
		},
		Kind: hook.OperationExecutionSettle,
		Name: "settle_execution",
	})
	defer func() { scope.End(hook.Result{Err: err, Outcome: string(result.Outcome)}) }()
	firstEnd := endedExecution.Status != agentexecution.ExecutionEnded
	if firstEnd {
		if !endedExecution.Active() {
			return agentexecution.Execution{}, fmt.Errorf("task cannot transition from %q to %q: %w", endedExecution.Status, agentexecution.ExecutionEnded, errorcode.ErrInvalidTransition)
		}
		at := s.clock.Now()
		if endedExecution.Status == agentexecution.ExecutionStarting {
			// 尚未启动即进入结算时，将本次结算时间同时记录为启动时间。
			endedExecution.Start(at)
		}
		if endedExecution.Status == agentexecution.ExecutionRunning {
			endedExecution.BeginEnding()
		}
		endedExecution.End(outcome, failureCode, at)
		if outcome == agentexecution.ExecutionFailed {
			endedExecution.FailureMessage = failureMessage
		}
		if err = s.records.Save(ctx, endedExecution); err != nil {
			s.logger.Errorf(
				"operation=execution_end request_id=%s session_id=%s agent_id=%s execution_id=%s status=failed outcome=%s error=%v",
				endedExecution.RequestID,
				endedExecution.SessionID,
				endedExecution.AgentID,
				executionID,
				outcome,
				err,
			)
			return agentexecution.Execution{}, err
		}
	}
	// 终态已提交，附属命名失败只记录日志；重复结算可以继续完成命名。
	defer func() {
		if nameErr := s.nameSessionAfterAnswer(context.WithoutCancel(ctx), endedExecution, endedExecution.Outcome, endedExecution.EndedAt); nameErr != nil {
			s.logger.Errorf("name settled session execution_id=%s error=%v", endedExecution.ID, nameErr)
		}
	}()
	if !firstEnd {
		return endedExecution, nil
	}
	s.publishExecutionTiming(endedExecution)
	s.logger.Infof(
		"operation=execution_end request_id=%s session_id=%s agent_id=%s execution_id=%s status=committed outcome=%s",
		endedExecution.RequestID,
		endedExecution.SessionID,
		endedExecution.AgentID,
		executionID,
		outcome,
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
	if endedExecution.Outcome == agentexecution.ExecutionPaused {
		if hookErr := s.hookDispatcher.ExecutionPaused(context.WithoutCancel(ctx), event); hookErr != nil {
			s.logger.Errorf("execution paused hook failed: %v", hookErr)
		}
	}
	if hookErr := s.hookDispatcher.ExecutionCommitted(context.WithoutCancel(ctx), event); hookErr != nil {
		s.logger.Errorf("execution committed hook failed: %v", hookErr)
	}
	// 每轮仅发布一个终态事件；仅实际响应用户取消的执行发布取消通知。
	if s.emitter != nil {
		kind := coreevent.ExecutionEnded
		if endedExecution.FailureCode == agentexecution.ExecutionFailureRequestCanceled {
			kind = coreevent.RequestCanceled
		}
		s.emitter.Emit(coreevent.Event{
			Kind:           kind,
			SessionID:      endedExecution.SessionID,
			AgentID:        endedExecution.AgentID,
			ExecutionID:    endedExecution.ID,
			Outcome:        string(endedExecution.Outcome),
			FailureCode:    string(endedExecution.FailureCode),
			FailureMessage: endedExecution.FailureMessage,
		})
	}
	return endedExecution, nil
}

func (s *Service) nameSessionAfterAnswer(
	ctx context.Context,
	task agentexecution.Execution,
	outcome agentexecution.ExecutionOutcome,
	at time.Time,
) error {
	if outcome != agentexecution.ExecutionCompleted && outcome != agentexecution.ExecutionYielded {
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
