package execution

import (
	"context"
	"fmt"
	"praxis/internal/errorcode"
	"time"

	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
)

// ConfirmRunning 区分首次启动与重复确认；审批等待不改变运行状态。
// 首次启动记录开始时间，已结束的执行不能重新启动。
// executionID 来自 Coordinator 准入时已校验的持久化 Execution。
func (s *Service) ConfirmRunning(ctx context.Context, run *agentexecution.Run) error {
	err := s.update(ctx, run, func(task *agentexecution.ExecutionRecord) error {
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
		s.publishExecutionTiming(run.Snapshot())
	}
	return err
}

// Settle 保存所属执行者确定的实际结果，不根据迟到的取消信号改写完成结果。
// 写入失败由 Coordinator 保留占用和结果后重试；重复结算返回已有终态，不再次发布事件。
func (s *Service) Settle(ctx context.Context, run *agentexecution.Run, outcome agentexecution.ExecutionOutcome, failureCode agentexecution.ExecutionFailureCode, failureMessage string) (result agentexecution.ExecutionRecord, err error) {
	endedExecution := run.Snapshot()
	executionID := endedExecution.ID
	// 在终态提交前持有实例引用，避免会话在提交后被删除导致状态收尾失去目标。
	agent, err := s.agent(endedExecution.AgentID)
	if err != nil {
		return agentexecution.ExecutionRecord{}, err
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
			return agentexecution.ExecutionRecord{}, fmt.Errorf("task cannot transition from %q to %q: %w", endedExecution.Status, agentexecution.ExecutionEnded, errorcode.ErrInvalidTransition)
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
		if run.Main() {
			err = s.records.Save(ctx, endedExecution)
		}
		if err != nil {
			s.logger.Errorf(
				"operation=execution_end request_id=%s session_id=%s agent_id=%s execution_id=%s status=failed outcome=%s error=%v",
				endedExecution.RequestID,
				endedExecution.SessionID,
				endedExecution.AgentID,
				executionID,
				outcome,
				err,
			)
			return agentexecution.ExecutionRecord{}, err
		}
	}
	if err := run.Update(func(record *agentexecution.ExecutionRecord) error { *record = endedExecution; return nil }); err != nil {
		return agentexecution.ExecutionRecord{}, err
	}
	// 终态已接纳；Main 命名只使用本次已持有的输入，子执行不修改会话名称。
	defer func() {
		if !run.Main() {
			return
		}
		if nameErr := s.nameSessionAfterAnswer(context.WithoutCancel(ctx), run, endedExecution, endedExecution.Outcome, endedExecution.EndedAt); nameErr != nil {
			s.logger.Errorf("name settled session execution_id=%s error=%v", endedExecution.ID, nameErr)
		}
	}()
	agent.FinishExecution(endedExecution)
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
		var payload coreevent.Payload = coreevent.ExecutionEnded{
			Outcome:        string(endedExecution.Outcome),
			FailureCode:    string(endedExecution.FailureCode),
			FailureMessage: endedExecution.FailureMessage,
		}
		if endedExecution.FailureCode == agentexecution.ExecutionFailureRequestCanceled {
			payload = coreevent.RequestCanceled{}
		}
		s.emitter.Emit(coreevent.Event{
			Scope: coreevent.Scope{
				SessionID:   endedExecution.SessionID,
				AgentID:     endedExecution.AgentID,
				ExecutionID: endedExecution.ID,
			},
			Payload: payload,
		})
	}
	return endedExecution, nil
}

func (s *Service) nameSessionAfterAnswer(
	ctx context.Context,
	run *agentexecution.Run,
	task agentexecution.ExecutionRecord,
	outcome agentexecution.ExecutionOutcome,
	at time.Time,
) error {
	if outcome != agentexecution.ExecutionCompleted && outcome != agentexecution.ExecutionYielded {
		return nil
	}
	message, found := run.Entry(task.InputEntryID)
	if !found {
		return errorcode.ErrNotFound
	}
	return s.sessions.NameFromInput(ctx, task.SessionID, message.TextContent(), at)
}
