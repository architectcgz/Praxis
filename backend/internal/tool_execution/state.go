package toolexecution

import (
	"bytes"
	"context"
	"time"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	toolcontracts "praxis/internal/tools/contracts"
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
		if task.Progress.ToolCalls >= taskmodel.MaxToolCalls {
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

// start 在同一事务中重新读取配置、核对审批参数并保存 running。
// 配置重载与这里共享事务边界；已启动的进程不受后续配置变更影响。
func (s *Executor) start(ctx context.Context, id contracts.ToolInvocationID) (toolmodel.ToolInvocation, bool, error) {
	var invocation toolmodel.ToolInvocation
	execute := false
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.invocations.Get(txCtx, id)
		if err != nil {
			return err
		}
		invocation = current
		if current.Status != toolmodel.ToolInvocationApproved && current.Status != toolmodel.ToolInvocationAwaitingApproval {
			return nil
		}
		task, err := s.tasks.Get(txCtx, current.TaskID)
		if err != nil {
			return err
		}
		if err := s.checkTask(txCtx, contracts.ToolInvocationContext{
			TaskID:        current.TaskID,
			TurnID:        current.TurnID,
			SessionID:     current.SessionID,
			AgentID:       current.AgentID,
			WorkspacePath: task.Config.WorkspacePath,
		}); err != nil {
			return err
		}
		decision, err := s.rules.Decide(txCtx, current)
		if err != nil {
			return err
		}
		previousStatus := current.Status
		if err := current.RecheckAuthorization(decision, s.clock.Now()); err != nil {
			return err
		}
		invocation = current
		if current.Status != previousStatus {
			return s.invocations.Save(txCtx, current)
		}
		if current.Status != toolmodel.ToolInvocationApproved {
			return nil
		}
		if current.Approval.ArgumentsDigest != toolmodel.ArgumentsDigest(current.Name, current.NormalizedArguments) {
			return contracts.ErrRequestConflict
		}
		if current.Name == contracts.ToolBash || current.Name == contracts.ToolApplyPatch {
			prior, err := s.invocations.ListByTask(txCtx, task.ID)
			if err != nil {
				return err
			}
			for _, previous := range prior {
				if previous.Status == toolmodel.ToolInvocationUnknown && previous.Name == current.Name &&
					(previous.ArgumentsDigest == current.ArgumentsDigest || bytes.Equal(previous.NormalizedArguments, current.NormalizedArguments)) {
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
		invocation = current
		execute = true
		return nil
	})
	return invocation, execute, err
}

// reject 结算执行前的拒绝；已经批准的调用使用 interrupted，不改写运行中调用。
func (s *Executor) reject(ctx context.Context, id contracts.ToolInvocationID, code toolmodel.ToolFailureCode, message string) (toolcontracts.ToolResult, error) {
	var invocation toolmodel.ToolInvocation
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.invocations.Get(txCtx, id)
		if err != nil {
			return err
		}
		if current.Status.Terminal() {
			invocation = current
			return nil
		}
		if current.Status == toolmodel.ToolInvocationRunning {
			return contracts.New(contracts.TaskBusy, "tool call is already running")
		}
		if current.Status == toolmodel.ToolInvocationApproved {
			code = toolmodel.ToolFailureInterrupted
		}
		result := toolcontracts.NewToolError(string(code), message)
		domain := toolmodel.ToolInvocationResult{InlineContent: result.Payload, ErrorCode: code}
		if current.Status == toolmodel.ToolInvocationApproved {
			err = current.Interrupt(domain, s.clock.Now())
		} else {
			err = current.Deny(domain, s.clock.Now())
		}
		if err != nil {
			return err
		}
		if err := s.invocations.Save(txCtx, current); err != nil {
			return err
		}
		invocation = current
		return nil
	})
	result, _ := settledResult(invocation)
	return result, err
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
