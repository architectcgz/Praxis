package coordinator

import (
	"context"
	"praxis/internal/core/approval"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
)

// StopRun 只取消命中的活动执行；主 Run 的取消与队列交接使用同一把会话命令锁。
func (s *Coordinator) StopRun(ctx context.Context, sessionID identity.SessionID, runID identity.RunID) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()

	unlock, err := s.lockSession(ctx, sessionID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	schedule := s.schedules[sessionID]
	if schedule == nil || schedule.active == nil || schedule.active.run == nil {
		s.mu.Unlock()
		unlock()
		return nil
	}
	active := schedule.active
	run := active.run
	s.mu.Unlock()
	if run.ID() != runID {
		unlock()
		run.CancelSubagentRun(runID)
		return nil
	}
	if run.TerminalSaved() {
		unlock()
		return nil
	}
	schedule.queue.Pause()
	active.cancel(errRunInterrupted)
	notification := taskQueueEvent(schedule)
	unlock()
	s.loop.Emitter.Emit(notification)
	return nil
}

func (s *Coordinator) ApproveToolCall(ctx context.Context, params approval.DecisionRequest) error {
	return s.decideToolCall(ctx, params, true)
}

func (s *Coordinator) DenyToolCall(ctx context.Context, params approval.DecisionRequest) error {
	return s.decideToolCall(ctx, params, false)
}

// decideToolCall 只更新活跃调用并唤醒所属执行者，不重启执行生命周期。
func (s *Coordinator) decideToolCall(ctx context.Context, params approval.DecisionRequest, approve bool) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	return s.approvals.DecideApproval(ctx, params, approve)
}

// StopToolCall 只发送停止信号，回执由当前工具路径生成。
func (s *Coordinator) StopToolCall(ctx context.Context, request executionmodel.ControlRequest) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	return s.tools.StopCall(ctx, request)
}
