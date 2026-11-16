package run

import (
	"context"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/loop"
)

func (s *Run) modelRunParams() (loop.RunParams, loop.RunDependencies) {
	record := s.Snapshot()
	return loop.RunParams{
			ID:           record.ID,
			SourceTaskID: record.SourceTaskID,
			SessionID:    record.SessionID,

			AgentID:      record.AgentID,
			SystemPrompt: record.Config.SystemPrompt,
			Model:        record.Config.Model,
		}, loop.RunDependencies{
			AcceptSubagentReports: s.acceptReports,
			AcceptInstructions:    s.acceptInstructions,
			FinishInstructions:    s.finishInstructions,
			Budget:                &appcontext.Budget{},
			ReadEntries:           s.loopEntries,
			AppendEntry:           s.AppendEntry,
		}

}

// loopEntries 从本次执行绑定的 Session 读取主历史或子历史的只读引用列表。
func (s *Run) loopEntries(ctx context.Context) ([]*sessionentry.ContextEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record := s.Snapshot()
	entries, err := s.Session.ModelEntries(ctx, record.AgentID, record.InputEntryID)
	if err != nil {
		return nil, storageFailure(err)
	}
	return entries, nil
}
