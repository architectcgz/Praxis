package coordinator

import (
	"context"
	"praxis/internal/engine/approval"
	"praxis/internal/engine/toolexecution"
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
	if schedule == nil {
		s.mu.Unlock()
		unlock()
		return nil
	}
	active := schedule.active
	if active == nil || active.run == nil || active.run.ID() != runID {
		s.mu.Unlock()
		unlock()
		schedule.subagents.CancelSubagentRun(runID)
		return nil
	}
	if active.run.TerminalSaved() {
		s.mu.Unlock()
		unlock()
		return nil
	}
	schedule.queue.Pause()
	notification := taskQueueEvent(schedule)
	s.mu.Unlock()
	active.cancel(errRunInterrupted)
	unlock()
	s.emitter.Emit(notification)
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
func (s *Coordinator) StopToolCall(ctx context.Context, request toolexecution.ControlRequest) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	return s.tools.StopCall(ctx, request)
}
