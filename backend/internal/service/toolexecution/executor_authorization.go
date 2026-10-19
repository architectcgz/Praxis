package toolexecution

import (
	"context"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
)

// checkTask 固定执行归属与工作区，不能通过重新准备参数改变被批准的操作。
func (s *Executor) checkTask(ctx context.Context, provided executionmodel.ToolExecutionMetadata) error {
	task, err := s.tasks.Get(ctx, provided.ExecutionID)
	if err != nil {
		return err
	}
	if !task.Active() || task.SessionID != provided.SessionID || task.AgentID != provided.AgentID ||
		task.Config.WorkspacePath != provided.WorkspacePath {
		return fault.ErrRequestConflict
	}
	return nil
}

func (s *Executor) admit(ctx context.Context, invocation executionmodel.Execution, call tool.NormalizedToolCall) (executionmodel.Execution, error) {
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.executions.Get(txCtx, invocation.ID)
		if err != nil {
			return err
		}
		if current.Status == executionmodel.StatusRequested {
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

func (s *Executor) authorize(ctx context.Context, id identity.ToolExecutionID) (executionmodel.Execution, error) {
	var invocation executionmodel.Execution
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		var err error
		invocation, err = s.executions.Get(txCtx, id)
		if err != nil || invocation.Status != executionmodel.StatusPrepared {
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
