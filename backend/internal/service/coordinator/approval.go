package coordinator

import (
	"context"
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

// 人工决定只更新活跃调用并唤醒所属执行者，不重启执行生命周期。
func (s *Coordinator) decideToolCall(ctx context.Context, params ToolDecision, approve bool) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	if err := s.tools.DecideApproval(ctx, params, approve); err != nil {
		return err
	}
	s.notifyToolApprovalWaiter(params.ExecutionID)
	return nil
}

func (s *Coordinator) ListToolApprovals(ctx context.Context, agentID identity.AgentID) ([]toolservice.Approval, error) {
	return s.tools.ListApprovals(ctx, agentID)
}

// StopToolCall 只发送停止信号，回执由当前工具路径生成。
func (s *Coordinator) StopToolCall(ctx context.Context, request ToolControl) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	task, err := s.tools.StopCall(ctx, request)
	if err != nil {
		return err
	}
	if task.Active() {
		s.notifyToolApprovalWaiter(task.ID)
	}
	return nil
}

// notifyToolApprovalWaiter 按唯一执行标识通知等待者，不依赖请求声明的 Agent 归属，也不承载决定本身。
// 通道保留提前到达的通知；已有待处理通知时合并发送，避免持锁阻塞。
func (s *Coordinator) notifyToolApprovalWaiter(executionID identity.AgentExecutionID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, active := range s.executions {
		if active.executionID != executionID || active.ending {
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
func (s *Coordinator) waitForToolApproval(ctx context.Context, active *activeExecution) error {
	select {
	case <-active.toolApprovalWake:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
