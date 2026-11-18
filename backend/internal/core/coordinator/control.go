package coordinator

import (
	"context"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
)

// AgentControl 以会话和 Agent 身份定位控制入口，不要求调用方知道执行编号。
type AgentControl struct {
	SessionID identity.SessionID
	AgentID   identity.AgentID
}

type ToolDecision = executionmodel.ControlRequest
type ToolControl = executionmodel.ControlRequest

// StopAgent 取消指定 Agent；子任务的取消能力由 Parent 持有。
func (s *Coordinator) StopAgent(ctx context.Context, request AgentControl) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()

	s.mu.Lock()
	binding := s.schedules[request.SessionID]
	var active *activeRun
	if binding != nil {
		active = binding.active
	}
	s.mu.Unlock()
	if active == nil {
		return nil
	}
	if request.AgentID == identity.AgentID(request.SessionID) {
		binding.queue.Pause()
		s.loop.Emitter.Emit(taskQueueEvent(binding))
		active.cancel(errRunInterrupted)
		return nil
	}
	select {
	case <-active.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	if active.cancelSubagent == nil {
		return nil
	}
	return active.cancelSubagent(request.AgentID)
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
	return s.tools.DecideApproval(ctx, params, approve)
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
	return s.tools.StopCall(ctx, request)
}
