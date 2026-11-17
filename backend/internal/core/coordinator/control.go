package coordinator

import (
	"context"
	"errors"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"time"
)

// RunControl 定位用户选择的执行，不以命令到达时的当前执行替换目标。
type RunControl struct {
	SessionID identity.SessionID
	AgentID   identity.AgentID
	RunID     identity.RunID
}

type ToolDecision = executionmodel.ControlRequest
type ToolControl = executionmodel.ControlRequest

// StopAgent 中断请求指定的 Run，保留尚未开始的输入。
func (s *Coordinator) StopAgent(ctx context.Context, request RunControl) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()

	active, applied, err := s.cancelRun(ctx, request, errRunInterrupted)
	if err != nil || !applied {
		return err
	}
	s.mu.Lock()
	binding := s.schedules[request.SessionID]
	source := coreevent.EventSource{
		SessionID: active.sessionID,
		AgentID:   request.AgentID,
		RunID:     active.runID,
	}
	s.mu.Unlock()
	if binding != nil && request.AgentID == identity.AgentID(request.SessionID) {
		binding.queue.Pause()
		s.loop.Emitter.Emit(taskQueueEvent(binding))
	}
	s.loop.Emitter.Emit(coreevent.Event{
		Source:  source,
		Payload: coreevent.RunInterruptRequested{AppliedAt: time.Now().UTC()},
	})
	return nil
}

func (s *Coordinator) cancelRun(ctx context.Context, request RunControl, cause error) (*activeRun, bool, error) {
	key := runKey{request.AgentID}
	s.mu.Lock()
	active := s.runs[key]
	s.mu.Unlock()
	if active == nil {
		return nil, false, nil
	}
	// Run 可能刚保存、尚未登记身份；只等待本次准备完成，不等待执行退出。
	// 保留本次查找到的执行句柄，避免等待期间换成下一次执行后误中断。
	select {
	case <-active.ready:
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
	s.mu.Lock()
	if s.closed || s.runs[key] != active {
		s.mu.Unlock()
		return nil, false, nil
	}
	if active.sessionID != request.SessionID || active.runID != request.RunID {
		s.mu.Unlock()
		return nil, false, errorcode.ErrRequestConflict
	}
	run := active.run
	s.mu.Unlock()
	if run != nil && run.Snapshot().Status == sessionentry.RunEnding {
		return nil, false, nil
	}
	if active.ctx.Err() != nil {
		return active, errors.Is(context.Cause(active.ctx), cause), nil
	}
	active.cancel(cause)
	return active, true, nil
}

func (s *Coordinator) ApproveToolCall(ctx context.Context, params ToolDecision) error {
	return s.decideToolCall(ctx, params, true)
}

func (s *Coordinator) DenyToolCall(ctx context.Context, params ToolDecision) error {
	return s.decideToolCall(ctx, params, false)
}

// decideToolCall 只更新活跃调用并唤醒所属执行者，不重启执行生命周期。
func (s *Coordinator) decideToolCall(ctx context.Context, params ToolDecision, approve bool) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	if err := s.tools.DecideApproval(ctx, params, approve); err != nil {
		return err
	}
	s.notifyToolApprovalWaiter(params.RunID)
	return nil
}

func (s *Coordinator) ListToolApprovals(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID) ([]executionmodel.Approval, error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.running.Done()
	return s.tools.ListApprovals(ctx, sessionID, agentID)
}

// StopToolCall 只发送停止信号，回执由当前工具路径生成。
func (s *Coordinator) StopToolCall(ctx context.Context, request ToolControl) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	if err := s.tools.StopCall(ctx, request); err != nil {
		return err
	}
	s.notifyToolApprovalWaiter(request.RunID)
	return nil
}

// notifyToolApprovalWaiter 按唯一执行标识通知等待者，不依赖请求声明的 Agent 归属，也不承载决定本身。
// 通道保留提前到达的通知；已有待处理通知时合并发送，避免持锁阻塞。
func (s *Coordinator) notifyToolApprovalWaiter(runID identity.RunID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, active := range s.runs {
		if active.runID != runID {
			continue
		}
		select {
		case active.toolApprovalWake <- struct{}{}:
		default:
		}
		return
	}
}

// waitForToolApproval 等待审批决定或工具停止通知；取消执行时直接退出等待，不重启 Loop。
func (s *Coordinator) waitForToolApproval(ctx context.Context, active *activeRun) error {
	select {
	case <-active.toolApprovalWake:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
