package coordinator

import (
	"context"
	taskmodel "praxis/internal/core/session"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	toolservice "praxis/internal/service/toolexecution"
)

type ToolDecision = executionmodel.ControlRequest
type ToolControl = executionmodel.ControlRequest

func (s *Coordinator) ApproveToolCall(ctx context.Context, params ToolDecision) error {
	return s.decideToolCall(ctx, params, true)
}
func (s *Coordinator) DenyToolCall(ctx context.Context, params ToolDecision) error {
	return s.decideToolCall(ctx, params, false)
}

// 审批事实由 Service 保存，Coordinator 只在提交后唤醒已有执行。
func (s *Coordinator) decideToolCall(ctx context.Context, params ToolDecision, approve bool) error {
	task, err := s.tools.DecideApproval(ctx, params, approve)
	if err != nil {
		return err
	}
	if task.Active() {
		return s.ResumeApproved(context.WithoutCancel(ctx), task)
	}
	return nil
}

func (s *Coordinator) ListToolApprovals(ctx context.Context, agentID identity.AgentID) ([]toolservice.Approval, error) {
	return s.toolRecords.ListApprovals(ctx, agentID)
}

// StopToolCall 在控制事实提交后唤醒同一 Loop，待审批调用也能消费取消回执。
func (s *Coordinator) StopToolCall(ctx context.Context, request ToolControl) error {
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

// waitForToolApproval 只等待当前 Loop 的唤醒，不重复启动或发布 ExecutionResumed。
func (s *Coordinator) waitForToolApproval(ctx context.Context, active *taskmodel.ActiveTask) error {
	select {
	case <-active.WakeSignal():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
