package toolexecution

import (
	"context"

	"praxis/internal/contracts"
	toolmodel "praxis/internal/core/tool_invocation"
	toolcontracts "praxis/internal/tools/contracts"
)

// checkTask 固定执行归属与工作区，不能通过重新准备参数改变被批准的操作。
func (s *Executor) checkTask(ctx context.Context, provided contracts.ToolInvocationContext) error {
	task, err := s.tasks.Get(ctx, provided.TaskID)
	if err != nil {
		return err
	}
	if !task.Active() || task.SessionID != provided.SessionID || task.AgentID != provided.AgentID ||
		task.Config.WorkspacePath != provided.WorkspacePath {
		return contracts.ErrRequestConflict
	}
	return nil
}

func (s *Executor) admit(ctx context.Context, invocation toolmodel.ToolInvocation, call toolcontracts.NormalizedToolCall) (toolmodel.ToolInvocation, error) {
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.invocations.Get(txCtx, invocation.ID)
		if err != nil {
			return err
		}
		if current.Status == toolmodel.ToolInvocationRequested {
			if err := current.Prepare(call.NormalizedArguments, call.Path); err != nil {
				return err
			}
			if err := s.invocations.Save(txCtx, current); err != nil {
				return err
			}
		}
		invocation = current
		return nil
	})
	return invocation, err
}

func (s *Executor) authorize(ctx context.Context, id contracts.ToolInvocationID) (toolmodel.ToolInvocation, error) {
	var invocation toolmodel.ToolInvocation
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		var err error
		invocation, err = s.invocations.Get(txCtx, id)
		if err != nil || invocation.Status != toolmodel.ToolInvocationPrepared {
			return err
		}
		decision, err := s.rules.Decide(txCtx, invocation)
		if err != nil {
			return err
		}
		if err := invocation.ApplyAuthorization(decision, s.clock.Now()); err != nil {
			return err
		}
		return s.invocations.Save(txCtx, invocation)
	})
	if err == nil && invocation.NeedsRiskReview() {
		return s.review(ctx, invocation.ID)
	}
	return invocation, err
}
