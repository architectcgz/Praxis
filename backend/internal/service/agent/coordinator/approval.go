package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	taskmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	toolservice "praxis/internal/service/toolexecution"
)

type ToolDecision = toolservice.ApprovalRequest

func (s *Coordinator) ApproveToolCall(ctx context.Context, params ToolDecision) error {
	return s.decideToolCall(ctx, params, true)
}
func (s *Coordinator) DenyToolCall(ctx context.Context, params ToolDecision) error {
	return s.decideToolCall(ctx, params, false)
}

// 审批事实由 Service 保存，Coordinator 只在提交后唤醒已有执行。
func (s *Coordinator) decideToolCall(ctx context.Context, params ToolDecision, approve bool) error {
	task, err := s.tools.Decide(ctx, params, approve)
	if err != nil {
		return err
	}
	if task.Active() {
		return s.ResumeApproved(context.WithoutCancel(ctx), task)
	}
	return nil
}

func (s *Coordinator) ListToolApprovals(ctx context.Context, agentID identity.AgentID) ([]toolservice.Approval, error) {
	return s.tools.ListApprovals(ctx, agentID)
}

// StopToolCall 在控制事实提交后唤醒同一 Loop，待审批调用也能消费取消回执。
func (s *Coordinator) StopToolCall(ctx context.Context, request toolservice.ControlRequest) error {
	task, err := s.tools.StopCall(ctx, request)
	if err != nil {
		return err
	}
	if task.Active() {
		if err := s.Activate(context.WithoutCancel(ctx), task); err != nil {
			return err
		}
		s.activeTasks.Wake(task.AgentID, task.ID)
	}
	return nil
}

// waitForToolApproval 在状态提交后发布通知，审批提前到达时由 wake 的缓冲保留唤醒。
func (s *Coordinator) waitForToolApproval(ctx context.Context, task agentmodel.Execution, active *taskmodel.ActiveTask, call sessionentry.ToolCall) error {
	if err := s.operations.WaitForApproval(ctx, task.ID); err != nil {
		return err
	}
	s.events.Publish(Event{
		Kind:        EventToolApprovalRequired,
		SessionID:   call.SessionID,
		AgentID:     call.AgentID,
		ExecutionID: call.ExecutionID,
		TurnID:      call.TurnID,
		CallID:      call.CallID,
		Name:        string(call.Name),
	})
	select {
	case <-active.WakeSignal():
		return s.startExecution(task)
	case <-ctx.Done():
		return ctx.Err()
	}
}
