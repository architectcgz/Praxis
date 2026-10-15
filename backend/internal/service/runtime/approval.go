package runtime

import (
	"context"
	"encoding/json"
	"time"

	"praxis/internal/contracts"
	"praxis/internal/core/security"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	"praxis/internal/repository"
	"praxis/internal/request"
)

// CommandPolicy 提供审批时的当前命令决策。
type CommandPolicy interface {
	DecideCommand(string) security.CommandDecision
}

// ToolApprovals 保存审批决定，实际工具执行由 Runtime 继续处理。
type ToolApprovals struct {
	CommandPolicy CommandPolicy
	Transactions  repository.TxRunner
	Tasks         repository.TaskRepository
	Invocations   repository.ToolInvocationRepository
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
		if approve && invocation.Name == contracts.ToolBash {
			var arguments struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(invocation.NormalizedArguments, &arguments); err != nil {
				return err
			}
			if a.CommandPolicy.DecideCommand(arguments.Command).Action == security.CommandDeny {
				err = invocation.Deny(toolmodel.ToolInvocationResult{InlineContent: "The command is prohibited by the current command policy.", ErrorCode: toolmodel.ToolFailureNotAllowed}, time.Now().UTC())
				if err != nil {
					return err
				}
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
