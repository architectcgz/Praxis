package run

import (
	"context"
	"fmt"
	"praxis/internal/errorcode"
	"time"

	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	sessionmodel "praxis/internal/core/session"
)

// ConfirmRunning 区分首次启动与重复确认；审批等待不改变运行状态。
// 首次启动记录开始时间，已结束的执行不能重新启动。
// runID 来自 Coordinator 准入时已校验的持久化 Run。
func (s *Service) ConfirmRunning(ctx context.Context, run *agentrun.Run) error {
	err := s.update(ctx, run, func(task *agentrun.RunRecord) error {
		switch task.Status {
		case agentrun.RunStarting:
			task.Start(s.clock.Now())
		case agentrun.RunRunning, agentrun.RunEnding:
			return nil
		default:
			return agentmodel.ErrUnavailable
		}
		return nil
	})
	if err == nil {
		if s.emitter != nil {
			s.emitter.Emit(coreevent.Event{
				Source:  coreevent.SourceForRun(run, ""),
				Payload: coreevent.RunStarted{Run: run.Snapshot()},
			})
		}
	}
	return err
}

// Settle 保存所属执行者确定的实际结果，不根据迟到的取消信号改写完成结果。
// 日志写入失败仍接纳并发布内存终态，同时返回结果和错误；重复结算不再次发布事件。
func (s *Service) Settle(ctx context.Context, run *agentrun.Run, outcome agentrun.RunOutcome, failureCode agentrun.RunFailureCode, failureMessage string) (result agentrun.RunRecord, err error) {
	endedRun := run.Snapshot()
	runID := endedRun.ID
	// Hook 在会话锁外观察本次结算；通知不能改变执行者已确定的结果。
	ctx, scope := s.hookDispatcher.Start(ctx, hook.Operation{
		Identity: hook.Identity{
			SessionID: endedRun.SessionID,
			AgentID:   endedRun.AgentID,
			RunID:     endedRun.ID,
		},
		Kind: hook.OperationRunSettle,
		Name: "settle_execution",
	})
	defer func() { scope.End(hook.Result{Err: err, Outcome: string(result.Outcome)}) }()
	firstEnd := endedRun.Status != agentrun.RunEnded
	if firstEnd {
		if !endedRun.Active() {
			return agentrun.RunRecord{}, fmt.Errorf("task cannot transition from %q to %q: %w", endedRun.Status, agentrun.RunEnded, errorcode.ErrInvalidTransition)
		}
		at := s.clock.Now()
		if endedRun.Status == agentrun.RunStarting {
			// 尚未启动即进入结算时，将本次结算时间同时记录为启动时间。
			endedRun.Start(at)
		}
		if endedRun.Status == agentrun.RunRunning {
			endedRun.BeginEnding()
		}
		endedRun.End(outcome, failureCode, at)
		if outcome == agentrun.RunFailed {
			endedRun.FailureMessage = failureMessage
		}
		err = s.records.SaveRun(ctx, endedRun)
		if err != nil {
			s.logger.Errorf(
				"operation=run_end request_id=%s session_id=%s agent_id=%s run_id=%s status=failed outcome=%s error=%v",
				endedRun.RequestID,
				endedRun.SessionID,
				endedRun.AgentID,
				runID,
				outcome,
				err,
			)
		}
	}
	if err := run.Update(func(record *agentrun.RunRecord) error { *record = endedRun; return nil }); err != nil {
		return agentrun.RunRecord{}, err
	}
	// 终态已接纳；Main 命名只使用本次已持有的输入，子执行不修改会话名称。
	defer func() {
		if !run.Main() {
			return
		}
		if nameErr := s.nameSessionAfterAnswer(context.WithoutCancel(ctx), run, endedRun, endedRun.Outcome, endedRun.EndedAt); nameErr != nil {
			s.logger.Errorf("name settled session run_id=%s error=%v", endedRun.ID, nameErr)
		}
	}()
	run.Agent.FinishRun(endedRun.AgentState(), endedRun.EndedAt)
	if !firstEnd {
		return endedRun, nil
	}
	s.logger.Infof(
		"operation=run_end request_id=%s session_id=%s agent_id=%s run_id=%s status=accepted outcome=%s",
		endedRun.RequestID,
		endedRun.SessionID,
		endedRun.AgentID,
		runID,
		outcome,
	)
	event := hook.RunEvent{
		Identity: hook.Identity{
			SourceTaskID: endedRun.SourceTaskID,
			SessionID:    endedRun.SessionID,
			AgentID:      endedRun.AgentID,
			RunID:        endedRun.ID,
		},
		Outcome:     string(endedRun.Outcome),
		FailureCode: string(endedRun.FailureCode),
	}
	if hookErr := s.hookDispatcher.RunCommitted(context.WithoutCancel(ctx), event); hookErr != nil {
		s.logger.Errorf("execution committed hook failed: %v", hookErr)
	}
	// 每轮仅发布一个终态事件；仅实际响应用户取消的执行发布取消通知。
	if s.emitter != nil {
		entries := run.Entries()
		var payload coreevent.Payload = coreevent.RunEnded{
			Run:     endedRun,
			State:   endedRun.AgentState(),
			Entries: entries,
		}
		if endedRun.FailureCode == agentrun.RunFailureRequestCanceled {
			payload = coreevent.RequestCanceled{
				Run:     endedRun,
				State:   endedRun.AgentState(),
				Entries: entries,
			}
		}
		s.emitter.Emit(coreevent.Event{
			Source: coreevent.EventSource{
				SessionID: endedRun.SessionID,
				AgentID:   endedRun.AgentID,
				BranchID:  endedRun.BranchID,
				BindingID: run.BindingID,
				RunID:     endedRun.ID,
			},
			Payload: payload,
		})
	}
	return endedRun, err
}

func (s *Service) nameSessionAfterAnswer(
	ctx context.Context,
	run *agentrun.Run,
	task agentrun.RunRecord,
	outcome agentrun.RunOutcome,
	at time.Time,
) error {
	if outcome != agentrun.RunCompleted && outcome != agentrun.RunYielded {
		return nil
	}
	session, err := s.sessions.Get(ctx, task.SessionID)
	if err != nil {
		return err
	}
	if session.Title != "" {
		return nil
	}
	message, found := run.Entry(task.InputEntryID)
	if !found {
		return errorcode.ErrNotFound
	}
	title := sessionmodel.TitleFromInput(message.TextContent())
	if title == "" {
		return nil
	}
	return s.sessions.UpdateTitle(ctx, task.SessionID, session.Title, title, at)
}
