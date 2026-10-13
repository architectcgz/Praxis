package runtime

import (
	"context"
	"errors"
	"time"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	"praxis/internal/repository"
	"praxis/internal/request"
)

// ToolApprovals 只接受应用审批命令；实际工具执行由 Runtime 继续处理。
type ToolApprovals struct {
	Transactions repository.TxRunner
	Tasks        repository.TaskRepository
	Invocations  repository.ToolInvocationRepository
	// UserID 来自可信的桌面会话，不接受前端自行声明审批者身份。
	UserID string
}

func (a ToolApprovals) Decide(ctx context.Context, params request.ToolDecision, approve bool) (taskmodel.Task, error) {
	var task taskmodel.Task
	if a.UserID == "" {
		return task, errors.New("trusted approver identity is required")
	}
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
		if invocation.DecisionBy == a.UserID && invocation.DecisionApproved == approve {
			return nil
		}
		if !task.Active() || invocation.Status != toolmodel.ToolInvocationAwaitingApproval {
			return contracts.ErrRequestConflict
		}
		invocation.DecisionBy = a.UserID
		invocation.DecisionApproved = approve
		if approve {
			err = invocation.Approve(contracts.ApprovalRecord{Source: contracts.ApprovalSourceUser, PolicyFingerprint: task.Input.Security.Fingerprint, ApprovedAt: time.Now().UTC()})
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
		return s.activator.Activate(context.WithoutCancel(ctx), task)
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
