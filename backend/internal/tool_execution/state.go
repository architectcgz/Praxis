package toolexecution

import (
	"bytes"
	"context"
	"praxis/internal/contracts"
	securitymodel "praxis/internal/core/security"
	toolmodel "praxis/internal/core/tool_invocation"
	toolcontracts "praxis/internal/tools/contracts"
	"time"
)

const settlementTimeout = 5 * time.Second

// reserveCall 对一次调用只扣减一次额度，审批、拒绝和重启都复用同一保留记录。
func (s *Executor) reserveCall(ctx context.Context, id contracts.ToolInvocationID) (toolmodel.ToolInvocation, error) {
	var invocation toolmodel.ToolInvocation
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		var err error
		invocation, err = s.invocations.Get(txCtx, id)
		if err != nil {
			return err
		}
		if invocation.BudgetReserved || invocation.Status.Terminal() {
			return nil
		}
		task, err := s.tasks.Get(txCtx, invocation.TaskID)
		if err != nil {
			return err
		}
		limit := task.Input.Security.Permissions.ResourceLimits.MaxToolCalls
		if limit <= 0 {
			limit = 128
		}
		if task.Progress.ToolCalls >= limit {
			return contracts.New(contracts.TaskResourceLimit, "tool call limit exceeded")
		}
		task.Progress.ToolCalls++
		invocation.BudgetReserved = true
		if err := s.tasks.Save(txCtx, task); err != nil {
			return err
		}
		return s.invocations.Save(txCtx, invocation)
	})
	return invocation, err
}

func (s *Executor) start(
	ctx context.Context,
	id contracts.ToolInvocationID,
	path string,
	effective *contracts.SecuritySnapshot,
) (toolmodel.ToolInvocation, bool, error) {
	var invocation toolmodel.ToolInvocation
	execute := false
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.invocations.Get(txCtx, id)
		if err != nil {
			return err
		}
		if current.Status == toolmodel.ToolInvocationApproved {
			task, err := s.tasks.Get(txCtx, current.TaskID)
			if err != nil {
				return err
			}
			security, err := s.loadSecurity(txCtx, contracts.ToolInvocationContext{
				TaskID:              current.TaskID,
				TurnID:              current.TurnID,
				SessionID:           current.SessionID,
				AgentID:             current.AgentID,
				WorkspacePath:       task.Input.WorkspacePath,
				SecurityFingerprint: task.Input.Security.Fingerprint,
			})
			if err != nil {
				return err
			}
			*effective = security
			if !securitymodel.AllowsToolCall(security, current.Name, path) || securitymodel.RequiresToolApproval(security, current.Name) && current.Approval.Source != contracts.ApprovalSourceUser {
				denied := toolcontracts.NewToolError(string(toolmodel.ToolFailureInterrupted), "The authorization was revoked before execution.")
				if err := current.Interrupt(toolmodel.ToolInvocationResult{InlineContent: denied.Payload, ErrorCode: toolmodel.ToolFailureInterrupted}, s.clock.Now()); err != nil {
					return err
				}
				invocation = current
				return s.invocations.Save(txCtx, current)
			}
			prior, err := s.invocations.ListByTask(txCtx, task.ID)
			if err != nil {
				return err
			}
			if security.ToolPermissions[current.Name].RequiresWrite || current.Name == contracts.ToolBash || current.Name == contracts.ToolApplyPatch {
				for _, previous := range prior {
					if previous.Status == toolmodel.ToolInvocationUnknown && previous.Name == current.Name && (previous.ArgumentsDigest == current.ArgumentsDigest || bytes.Equal(previous.NormalizedArguments, current.NormalizedArguments)) {
						result := toolcontracts.NewToolError(string(toolmodel.ToolFailureInterrupted), "A previous identical call has unknown side effects. Confirm its outcome before retrying.")
						if err := current.Interrupt(toolmodel.ToolInvocationResult{InlineContent: result.Payload, ErrorCode: toolmodel.ToolFailureInterrupted}, s.clock.Now()); err != nil {
							return err
						}
						invocation = current
						return s.invocations.Save(txCtx, current)
					}
				}
			}
			if err := current.Start(s.clock.Now()); err != nil {
				return err
			}
			if err := s.invocations.Save(txCtx, current); err != nil {
				return err
			}
			execute = true
		}
		invocation = current
		return nil
	})
	return invocation, execute, err
}

func (s *Executor) succeed(
	ctx context.Context,
	id contracts.ToolInvocationID,
	result toolcontracts.ToolResult,
) error {
	domainResult := toolmodel.ToolInvocationResult{
		InlineContent: result.Payload, SideEffect: result.SideEffect, Truncated: result.Truncated,
	}
	return s.settle(ctx, id, func(invocation *toolmodel.ToolInvocation) error {
		return invocation.Succeed(domainResult, s.clock.Now())
	})
}

func (s *Executor) fail(
	ctx context.Context,
	id contracts.ToolInvocationID,
	result toolcontracts.ToolResult,
) error {
	domainResult := toolmodel.ToolInvocationResult{
		InlineContent: result.Payload, ErrorCode: toolmodel.ToolFailureExecutionFailed,
	}
	return s.settle(ctx, id, func(invocation *toolmodel.ToolInvocation) error {
		return invocation.Fail(domainResult, toolmodel.ToolFailureExecutionFailed, s.clock.Now())
	})
}

func (s *Executor) interrupt(
	ctx context.Context,
	id contracts.ToolInvocationID,
	result toolcontracts.ToolResult,
) error {
	domainResult := toolmodel.ToolInvocationResult{
		InlineContent: result.Payload, ErrorCode: toolmodel.ToolFailureResultUnknown,
	}
	return s.settle(ctx, id, func(invocation *toolmodel.ToolInvocation) error {
		return invocation.MarkResultUnknown(domainResult, s.clock.Now())
	})
}

func (s *Executor) settle(
	ctx context.Context,
	id contracts.ToolInvocationID,
	transition func(*toolmodel.ToolInvocation) error,
) error {
	settlementContext := ctx
	cancel := func() {}
	if ctx.Err() != nil {
		settlementContext, cancel = context.WithTimeout(context.WithoutCancel(ctx), settlementTimeout)
	}
	defer cancel()
	return s.tx.InTx(settlementContext, func(txCtx context.Context) error {
		invocation, err := s.invocations.Get(txCtx, id)
		if err != nil {
			return err
		}
		if invocation.Status.Terminal() {
			return nil
		}
		if err := transition(&invocation); err != nil {
			return err
		}
		return s.invocations.Save(txCtx, invocation)
	})
}

func settledResult(invocation toolmodel.ToolInvocation) (toolcontracts.ToolResult, bool) {
	if !invocation.Status.Terminal() {
		return toolcontracts.ToolResult{}, false
	}
	return toolcontracts.ToolResult{
		Payload: invocation.Result.InlineContent, ErrorClass: string(invocation.Result.ErrorCode),
		SideEffect: invocation.Result.SideEffect, Truncated: invocation.Result.Truncated,
	}, true
}
