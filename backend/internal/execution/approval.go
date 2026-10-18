package execution

import (
	"context"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/security"
	taskmodel "praxis/internal/task"
	"praxis/internal/toolcall"
	txrunner "praxis/internal/transaction"
	"time"
)

// ToolApprovals 保存审批决定，实际工具执行由 Runtime 继续处理。
type ToolApprovals struct {
	DecideOperation func(context.Context, toolcall.Execution) (security.ApprovalDecision, error)
	Transactions    txrunner.Runner
	Tasks           taskmodel.Store
	Executions      toolcall.Store
}

// ToolDecision 固定审批目标和已规范化的参数摘要，避免决定作用于变更后的调用。
type ToolDecision struct {
	SessionID       identity.SessionID
	AgentID         identity.AgentID
	TaskID          identity.TaskID
	CallID          string
	ArgumentsDigest string
}

// Decide 在事务中检查调用归属和当前授权规则，幂等保存用户决定。
func (a ToolApprovals) Decide(ctx context.Context, params ToolDecision, approve bool) (taskmodel.Task, error) {
	var task taskmodel.Task
	err := a.Transactions.InTx(ctx, func(txCtx context.Context) error {
		var err error
		task, err = a.Tasks.Get(txCtx, params.TaskID)
		if err != nil {
			return err
		}
		if task.SessionID != params.SessionID || task.AgentID != params.AgentID {
			return fault.ErrRequestConflict
		}
		invocation, err := a.Executions.FindByTaskCall(txCtx, task.ID, params.CallID)
		if err != nil {
			return err
		}
		if params.ArgumentsDigest != toolcall.ArgumentsDigest(invocation.Name, invocation.NormalizedArguments) {
			return fault.ErrRequestConflict
		}
		if approve && invocation.Approval.Source == toolcall.ApprovalSourceUser ||
			!approve && invocation.FailureCode == toolcall.FailureApprovalDenied {
			return nil
		}
		if !task.Active() || invocation.Status != toolcall.StatusAwaitingApproval {
			return fault.ErrRequestConflict
		}
		if approve {
			decision, err := a.DecideOperation(txCtx, invocation)
			if err != nil {
				return err
			}
			if err := invocation.RecheckAuthorization(decision, time.Now()); err != nil {
				return err
			}
			if invocation.Status != toolcall.StatusAwaitingApproval {
				return a.Executions.Save(txCtx, invocation)
			}
		}
		if approve {
			err = invocation.Approve(toolcall.ApprovalRecord{Source: toolcall.ApprovalSourceUser, ArgumentsDigest: params.ArgumentsDigest, ApprovedAt: time.Now().UTC()})
		} else {
			err = invocation.Deny(toolcall.Result{InlineContent: "The user denied this tool call.", ErrorCode: toolcall.FailureApprovalDenied}, time.Now().UTC())
		}
		if err != nil {
			return err
		}
		return a.Executions.Save(txCtx, invocation)
	})
	return task, err
}

// ApproveToolCall 提交批准后唤醒仍活跃的任务，提交失败不发送执行信号。
func (s *Runtime) ApproveToolCall(ctx context.Context, params ToolDecision) error {
	return s.decideToolCall(ctx, params, true)
}

// DenyToolCall 提交拒绝后唤醒仍活跃的任务，由执行循环处理已结算的调用。
func (s *Runtime) DenyToolCall(ctx context.Context, params ToolDecision) error {
	return s.decideToolCall(ctx, params, false)
}
func (s *Runtime) decideToolCall(ctx context.Context, params ToolDecision, approve bool) error {
	task, err := s.approvals.Decide(ctx, params, approve)
	if err != nil {
		return err
	}
	if task.Active() {
		return s.coordinator.ResumeApproved(context.WithoutCancel(ctx), task)
	}
	return nil
}
func (s *Runtime) ListToolApprovals(ctx context.Context, agentID identity.AgentID) ([]toolcall.Execution, error) {
	values, err := s.approvals.Executions.ListAwaitingByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	pending := values[:0]
	for _, invocation := range values {
		task, err := s.approvals.Tasks.Get(ctx, invocation.TaskID)
		if err != nil {
			return nil, err
		}
		if task.Active() {
			pending = append(pending, invocation)
		}
	}
	return pending, nil
}
