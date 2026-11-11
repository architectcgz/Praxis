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
			SystemPrompt: task.Config.SystemPrompt,
			Model:        task.Config.Model,
		}, loop.RunDependencies{
			Recorder: &runRecorder{service: s, run: run},
			ReadEntries: func(ctx context.Context) ([]*sessionentry.ContextEntry, error) {
				return s.loopEntries(ctx, run)
			},
			AppendEntry: func(ctx context.Context, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
				return s.appendEntry(ctx, run, value)
			},
		}
}

// loopEntries 从本次执行绑定的 Session 读取主分支或子历史的只读引用列表。
func (s *Service) loopEntries(ctx context.Context, run *agentrun.Run) ([]*sessionentry.ContextEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	task := run.Snapshot()
	var entries []*sessionentry.ContextEntry
	var err error
	if run.Main() {
		entries, err = run.Session.BranchEntries(ctx, task.BranchID)
	} else {
		entries, err = run.Session.SubagentEntries(ctx, task.AgentID)
	}
	if err != nil {
		return nil, storageFailure(err)
	}
	return entries, nil
}
