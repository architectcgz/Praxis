package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/security"
	"praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"time"
)

// ToolApprovals 保存审批决定，实际工具执行由 Coordinator 继续处理。
type ToolApprovals struct {
	DecideOperation func(context.Context, toolexecution.Execution) (security.ApprovalDecision, error)
	Transactions    repository.Transaction
	Tasks           repository.AgentExecution
	Executions      repository.ToolExecution
}

// ToolDecision 固定审批目标和已规范化的参数摘要，避免决定作用于变更后的调用。
type ToolDecision struct {
	SessionID       identity.SessionID
	AgentID         identity.AgentID
	ExecutionID     identity.AgentExecutionID
	CallID          string
	ArgumentsDigest string
}

// Decide 在事务中检查调用归属和当前授权规则，幂等保存用户决定。
func (a ToolApprovals) Decide(ctx context.Context, params ToolDecision, approve bool) (agentmodel.Execution, error) {
	var task agentmodel.Execution
	err := a.Transactions.InTx(ctx, func(txCtx context.Context) error {
		var err error
		task, err = a.Tasks.Get(txCtx, params.ExecutionID)
		if err != nil {
			return err
		}
		if task.SessionID != params.SessionID || task.AgentID != params.AgentID {
			return fault.ErrRequestConflict
		}
		invocation, err := a.Executions.FindByExecutionCall(txCtx, task.ID, params.CallID)
		if err != nil {
			return err
		}
		if params.ArgumentsDigest != toolexecution.ArgumentsDigest(invocation.Name, invocation.NormalizedArguments) {
			return fault.ErrRequestConflict
		}
		if approve && invocation.Approval.Source == toolexecution.ApprovalSourceUser ||
			!approve && invocation.FailureCode == toolexecution.FailureApprovalDenied {
			return nil
		}
		if !task.Active() || invocation.Status != toolexecution.StatusAwaitingApproval {
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
			if invocation.Status != toolexecution.StatusAwaitingApproval {
				return a.Executions.Save(txCtx, invocation)
			}
		}
		if approve {
			err = invocation.Approve(toolexecution.ApprovalRecord{Source: toolexecution.ApprovalSourceUser, ArgumentsDigest: params.ArgumentsDigest, ApprovedAt: time.Now().UTC()})
		} else {
			err = invocation.Deny(toolexecution.Result{InlineContent: "The user denied this tool call.", ErrorCode: toolexecution.FailureApprovalDenied}, time.Now().UTC())
		}
		if err != nil {
			return err
		}
		return a.Executions.Save(txCtx, invocation)
	})
	return task, err
}

// ApproveToolCall 提交批准后唤醒仍活跃的任务，提交失败不发送执行信号。
func (s *Coordinator) ApproveToolCall(ctx context.Context, params ToolDecision) error {
	return s.decideToolCall(ctx, params, true)
}

// DenyToolCall 提交拒绝后唤醒仍活跃的任务，由执行循环处理已结算的调用。
func (s *Coordinator) DenyToolCall(ctx context.Context, params ToolDecision) error {
	return s.decideToolCall(ctx, params, false)
}
func (s *Coordinator) decideToolCall(ctx context.Context, params ToolDecision, approve bool) error {
	task, err := s.approvals.Decide(ctx, params, approve)
	if err != nil {
		return err
	}
	if task.Active() {
		return s.ResumeApproved(context.WithoutCancel(ctx), task)
	}
	return nil
}
func (s *Coordinator) ListToolApprovals(ctx context.Context, agentID identity.AgentID) ([]toolexecution.Execution, error) {
	values, err := s.approvals.Executions.ListAwaitingByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	pending := values[:0]
	for _, invocation := range values {
		task, err := s.approvals.Tasks.Get(ctx, invocation.ExecutionID)
		if err != nil {
			return nil, err
		}
		if task.Active() {
			pending = append(pending, invocation)
		}
	}
	return pending, nil
}
