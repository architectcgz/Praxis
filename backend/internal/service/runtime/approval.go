package runtime

import (
	"context"
	"time"

	"praxis/internal/contracts"
	"praxis/internal/core/security"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	"praxis/internal/repository"
	"praxis/internal/request"
)

// ToolApprovals 保存审批决定，实际工具执行由 Runtime 继续处理。
type ToolApprovals struct {
	DecideOperation func(context.Context, toolmodel.ToolInvocation) (security.ApprovalDecision, error)
	Transactions    repository.TxRunner
	Tasks           repository.TaskRepository
	Invocations     repository.ToolInvocationRepository
}

func (a ToolApprovals) Decide(ctx context.Context, params request.ToolDecision, approve bool) (taskmodel.Task, error) {
	var task taskmodel.Task
	err := a.Transactions.InTx(ctx, func(txCtx context.Context) error {
		var err error
		task, err = a.Tasks.Get(txCtx, params.TaskID)
		if err != nil {
			return err
		}
		if task.SessionID != params.SessionID || task.AgentID != params.AgentID {
			return contracts.ErrRequestConflict
		}
		invocation, err := a.Invocations.FindByTaskCall(txCtx, task.ID, params.CallID)
		if err != nil {
			return err
		}
		if params.ArgumentsDigest != toolmodel.ArgumentsDigest(invocation.Name, invocation.NormalizedArguments) {
			return contracts.ErrRequestConflict
		}
		if approve && invocation.Approval.Source == contracts.ApprovalSourceUser ||
			!approve && invocation.FailureCode == toolmodel.ToolFailureApprovalDenied {
			return nil
		}
		if !task.Active() || invocation.Status != toolmodel.ToolInvocationAwaitingApproval {
			return contracts.ErrRequestConflict
		}
		if approve {
			decision, err := a.DecideOperation(txCtx, invocation)
			if err != nil {
				return err
			}
			if err := invocation.RecheckAuthorization(decision, time.Now()); err != nil {
				return err
			}
			if invocation.Status != toolmodel.ToolInvocationAwaitingApproval {
				return a.Invocations.Save(txCtx, invocation)
			}
		}
		if approve {
			err = invocation.Approve(contracts.ApprovalRecord{Source: contracts.ApprovalSourceUser, ArgumentsDigest: params.ArgumentsDigest, ApprovedAt: time.Now().UTC()})
		} else {
			err = invocation.Deny(toolmodel.ToolInvocationResult{InlineContent: "The user denied this tool call.", ErrorCode: toolmodel.ToolFailureApprovalDenied}, time.Now().UTC())
		}
		if err != nil {
			return err
		}
		return a.Invocations.Save(txCtx, invocation)
	})
	return task, err
}

func (s *Service) ApproveToolCall(ctx context.Context, params request.ToolDecision) error {
	return s.decideToolCall(ctx, params, true)
}
func (s *Service) DenyToolCall(ctx context.Context, params request.ToolDecision) error {
	return s.decideToolCall(ctx, params, false)
}
func (s *Service) decideToolCall(ctx context.Context, params request.ToolDecision, approve bool) error {
	task, err := s.approvals.Decide(ctx, params, approve)
	if err != nil {
		return err
	}
	if task.Active() {
		return s.activator.ResumeApproved(context.WithoutCancel(ctx), task)
	}
	return nil
}
func (s *Service) ListToolApprovals(ctx context.Context, agentID contracts.AgentID) ([]toolmodel.ToolInvocation, error) {
	values, err := s.approvals.Invocations.ListAwaitingByAgent(ctx, agentID)
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
