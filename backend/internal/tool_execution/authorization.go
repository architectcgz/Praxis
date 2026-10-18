package toolexecution

import (
	"context"

	"praxis/internal/core/fault"
	"praxis/internal/core/identity"
	toolmodel "praxis/internal/core/tool_execution"
	toolcontracts "praxis/internal/tools/contracts"
)

// checkTask 固定执行归属与工作区，不能通过重新准备参数改变被批准的操作。
func (s *Executor) checkTask(ctx context.Context, provided toolmodel.ToolExecutionMetadata) error {
	task, err := s.tasks.Get(ctx, provided.TaskID)
	if err != nil {
		return err
	}
	if !task.Active() || task.SessionID != provided.SessionID || task.AgentID != provided.AgentID ||
		task.Config.WorkspacePath != provided.WorkspacePath {
		return fault.ErrRequestConflict
	}
	return nil
}

func (s *Executor) admit(ctx context.Context, invocation toolmodel.Execution, call toolcontracts.NormalizedToolCall) (toolmodel.Execution, error) {
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.executions.Get(txCtx, invocation.ID)
		if err != nil {
			return err
		}
		if current.Status == toolmodel.StatusRequested {
			if err := current.Prepare(call.NormalizedArguments, call.Path); err != nil {
				return err
			}
			if err := s.executions.Save(txCtx, current); err != nil {
				return err
			}
		}
		invocation = current
		return nil
	})
	return invocation, err
}

func (s *Executor) authorize(ctx context.Context, id identity.ToolExecutionID) (toolmodel.Execution, error) {
	var invocation toolmodel.Execution
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		var err error
		invocation, err = s.executions.Get(txCtx, id)
		if err != nil || invocation.Status != toolmodel.StatusPrepared {
			return err
		}
		decision, err := s.rules.Decide(txCtx, invocation)
		if err != nil {
			return err
		}
		if err := invocation.ApplyAuthorization(decision, s.clock.Now()); err != nil {
			return err
		}
		return s.executions.Save(txCtx, invocation)
	})
	if err == nil && invocation.NeedsRiskReview() {
		return s.review(ctx, invocation.ID)
	}
	return invocation, err
}
