package coordinator

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	taskmodel "praxis/internal/core/session"
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

// waitForToolApproval 只等待已有执行的唤醒并恢复运行状态，不发布审批通知。
func (s *Coordinator) waitForToolApproval(ctx context.Context, task agentexecution.Execution, active *taskmodel.ActiveTask) error {
	select {
	case <-active.WakeSignal():
		return s.startExecution(task, "approval")
	case <-ctx.Done():
		return ctx.Err()
	}
}
