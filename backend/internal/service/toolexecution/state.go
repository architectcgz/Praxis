package toolexecution

import (
	"bytes"
	"context"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
	"time"
)

const settlementTimeout = 5 * time.Second

// reserveCall 对一次调用只扣减一次额度，审批、拒绝和重启都复用同一保留记录。
func (s *Executor) reserveCall(ctx context.Context, id identity.ToolExecutionID) (executionmodel.Execution, error) {
	var invocation executionmodel.Execution
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		var err error
		invocation, err = s.executions.Get(txCtx, id)
		if err != nil {
			return err
		}
		if invocation.BudgetReserved || invocation.Status.Terminal() {
			return nil
		}
		task, err := s.tasks.Get(txCtx, invocation.ExecutionID)
		if err != nil {
			return err
		}
		if task.Progress.ToolCalls >= agentmodel.MaxToolCalls {
			return fault.New(agentservice.ExecutionResourceLimit, "tool call limit exceeded")
		}
		task.Progress.ToolCalls++
		invocation.BudgetReserved = true
		if err := s.tasks.Save(txCtx, task); err != nil {
			return err
		}
		return s.executions.Save(txCtx, invocation)
	})
	return invocation, err
}

// start 在同一事务中重新读取配置、核对审批参数并保存 running。
// 配置重载与这里共享事务边界；已启动的进程不受后续配置变更影响。
func (s *Executor) start(ctx context.Context, id identity.ToolExecutionID) (executionmodel.Execution, bool, error) {
	var invocation executionmodel.Execution
	execute := false
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.executions.Get(txCtx, id)
		if err != nil {
			return err
		}
		invocation = current
		if current.Status != executionmodel.StatusApproved && current.Status != executionmodel.StatusAwaitingApproval {
			return nil
		}
		task, err := s.tasks.Get(txCtx, current.ExecutionID)
		if err != nil {
			return err
		}
		if err := s.checkTask(txCtx, executionmodel.ToolExecutionMetadata{
			ExecutionID:   current.ExecutionID,
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
			return s.executions.Save(txCtx, current)
		}
		if current.Status != executionmodel.StatusApproved {
			return nil
		}
		if current.Approval.ArgumentsDigest != executionmodel.ArgumentsDigest(current.Name, current.NormalizedArguments) {
			return fault.ErrRequestConflict
		}
		if current.Name == tool.ToolBash || current.Name == tool.ToolApplyPatch {
			prior, err := s.executions.ListByExecution(txCtx, task.ID)
			if err != nil {
				return err
			}
			for _, previous := range prior {
				if previous.Status == executionmodel.StatusUnknown && previous.Name == current.Name &&
					(previous.ArgumentsDigest == current.ArgumentsDigest || bytes.Equal(previous.NormalizedArguments, current.NormalizedArguments)) {
					result := tool.NewToolError(string(executionmodel.FailureInterrupted), "A previous identical call has unknown side effects. Confirm its outcome before retrying.")
					if err := current.Interrupt(executionmodel.Result{InlineContent: result.Payload, ErrorCode: executionmodel.FailureInterrupted}, s.clock.Now()); err != nil {
						return err
					}
					invocation = current
					return s.executions.Save(txCtx, current)
				}
			}
		}
		if err := current.Start(s.clock.Now()); err != nil {
			return err
		}
		if err := s.executions.Save(txCtx, current); err != nil {
			return err
		}
		invocation = current
		execute = true
		return nil
	})
	return invocation, execute, err
}

// reject 结算执行前的拒绝；已经批准的调用使用 interrupted，不改写运行中调用。
func (s *Executor) reject(ctx context.Context, id identity.ToolExecutionID, code executionmodel.FailureCode, message string) (tool.ToolResult, error) {
	var invocation executionmodel.Execution
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.executions.Get(txCtx, id)
		if err != nil {
			return err
		}
		if current.Status.Terminal() {
			invocation = current
			return nil
		}
		if current.Status == executionmodel.StatusRunning {
			return fault.New(agentservice.ExecutionBusy, "tool call is already running")
		}
		if current.Status == executionmodel.StatusApproved {
			code = executionmodel.FailureInterrupted
		}
		result := tool.NewToolError(string(code), message)
		domain := executionmodel.Result{InlineContent: result.Payload, ErrorCode: code}
		if current.Status == executionmodel.StatusApproved {
			err = current.Interrupt(domain, s.clock.Now())
		} else {
			err = current.Deny(domain, s.clock.Now())
		}
		if err != nil {
			return err
		}
		if err := s.executions.Save(txCtx, current); err != nil {
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
	id identity.ToolExecutionID,
	result tool.ToolResult,
) error {
	domainResult := executionmodel.Result{
		InlineContent: result.Payload, SideEffect: result.SideEffect, Truncated: result.Truncated,
	}
	return s.settle(ctx, id, func(invocation *executionmodel.Execution) error {
		return invocation.Succeed(domainResult, s.clock.Now())
	})
}

func (s *Executor) fail(
	ctx context.Context,
	id identity.ToolExecutionID,
	result tool.ToolResult,
) error {
	domainResult := executionmodel.Result{
		InlineContent: result.Payload, ErrorCode: executionmodel.FailureExecutionFailed,
	}
	return s.settle(ctx, id, func(invocation *executionmodel.Execution) error {
		return invocation.Fail(domainResult, executionmodel.FailureExecutionFailed, s.clock.Now())
	})
}

func (s *Executor) interrupt(
	ctx context.Context,
	id identity.ToolExecutionID,
	result tool.ToolResult,
) error {
	domainResult := executionmodel.Result{
		InlineContent: result.Payload, ErrorCode: executionmodel.FailureResultUnknown,
	}
	return s.settle(ctx, id, func(invocation *executionmodel.Execution) error {
		return invocation.MarkResultUnknown(domainResult, s.clock.Now())
	})
}

func (s *Executor) settle(
	ctx context.Context,
	id identity.ToolExecutionID,
	transition func(*executionmodel.Execution) error,
) error {
	settlementContext := ctx
	cancel := func() {}
	if ctx.Err() != nil {
		settlementContext, cancel = context.WithTimeout(context.WithoutCancel(ctx), settlementTimeout)
	}
	defer cancel()
	return s.tx.InTx(settlementContext, func(txCtx context.Context) error {
		invocation, err := s.executions.Get(txCtx, id)
		if err != nil {
			return err
		}
		if invocation.Status.Terminal() {
			return nil
		}
		if err := transition(&invocation); err != nil {
			return err
		}
		return s.executions.Save(txCtx, invocation)
	})
}

func settledResult(invocation executionmodel.Execution) (tool.ToolResult, bool) {
	if !invocation.Status.Terminal() {
		return tool.ToolResult{}, false
	}
	return tool.ToolResult{
		Payload: invocation.Result.InlineContent, ErrorClass: string(invocation.Result.ErrorCode),
		SideEffect: invocation.Result.SideEffect, Truncated: invocation.Result.Truncated,
	}, true
}
