package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/loop"
)

func (s *Service) modelExecutionParams(run *agentexecution.Run) (loop.ExecutionParams, loop.ExecutionDependencies) {
	task := run.Snapshot()
	return loop.ExecutionParams{
			ID:           task.ID,
			SourceTaskID: task.SourceTaskID,
			SessionID:    task.SessionID,
			BranchID:     task.BranchID,
			BindingID:    run.BindingID,
			AgentID:      task.AgentID,
			RequestID:    task.RequestID,
			Model:        task.Config.Model.Clone(),
		}, loop.ExecutionDependencies{
			Execution: &modelProgress{service: s, run: run},
			Timing:    run.Timing,
		}
}

// contextContent 只读取本次 Run 的 Context，当前输入保护不回写共享 Entry。
func (s *Service) contextContent(ctx context.Context, run *agentexecution.Run) (appcontext.Content, error) {
	if err := ctx.Err(); err != nil {
		return appcontext.Content{}, err
	}
	task := run.Snapshot()
	return appcontext.ContentFromEntries(task.Config.SystemPrompt, task.InputEntryID, run.ContextEntries()), nil
}
