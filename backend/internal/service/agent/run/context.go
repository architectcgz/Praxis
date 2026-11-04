package run

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/loop"
	sessionentry "praxis/internal/core/session/entry"
)

func (s *Service) modelRunParams(run *agentrun.Run) (loop.RunParams, loop.RunDependencies) {
	task := run.Snapshot()
	return loop.RunParams{
			ID:           task.ID,
			SourceTaskID: task.SourceTaskID,
			SessionID:    task.SessionID,
			BranchID:     task.BranchID,
			BindingID:    run.BindingID,
			AgentID:      task.AgentID,
			RequestID:    task.RequestID,
			Model:        task.Config.Model.Clone(),
		}, loop.RunDependencies{
			Progress: &modelProgress{service: s, run: run},
			AppendEntry: func(ctx context.Context, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
				return s.appendEntry(ctx, run, value)
			},
		}
}

// loopInput 从 Session 读取主分支或子历史，模型上下文的引用列表由 Loop 创建。
func (s *Service) loopInput(ctx context.Context, run *agentrun.Run) (loop.Input, error) {
	if err := ctx.Err(); err != nil {
		return loop.Input{}, err
	}
	task := run.Snapshot()
	entries, err := run.Session.AgentEntries(ctx, task.AgentID, task.BranchID)
	if err != nil {
		return loop.Input{}, storageFailure(err)
	}
	return loop.Input{
		SystemPrompt: task.Config.SystemPrompt,
		InputEntryID: task.InputEntryID,
		Entries:      entries,
	}, nil
}
