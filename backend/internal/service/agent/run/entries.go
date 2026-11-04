package run

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/identity"
)

// appendEntry 显式选择主分支或子历史，提交后将同一引用交给 Run。
// 日志失败保留双方已接纳的内存事实；重复条目不再次发送 Hook 或条目通知。
func (s *Service) appendEntry(ctx context.Context, run *agentrun.Run, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
	record := run.Snapshot()
	var entry *sessionentry.ContextEntry
	var inserted bool
	var writeErr error
	if run.Main() {
		entry, inserted, writeErr = run.Session.AppendEntry(ctx, record.BranchID, value)
	} else {
		entry, inserted, writeErr = run.Session.AppendSubagentEntry(ctx, record.AgentID, record.BranchID, value)
	}
	if writeErr != nil && !inserted {
		return nil, false, storageFailure(writeErr)
	}
	_, _, err := run.Accept(entry)
	if err != nil {
		return nil, false, storageFailure(err)
	}
	if inserted {
		if entry.Role == role.Assistant && run.Main() {
			s.notifyAssistant(ctx, run, *entry)
		}
		s.emitter.Emit(coreevent.Event{
			Source: coreevent.SourceForRun(run, identity.TurnID(entry.TurnID)),
			Payload: coreevent.EntriesAppended{
				Entries: []sessionentry.ContextEntry{*entry},
			},
		})
	}
	if writeErr != nil {
		return entry, inserted, storageFailure(writeErr)
	}
	return entry, inserted, nil
}

func (s *Service) notifyAssistant(ctx context.Context, run *agentrun.Run, entry sessionentry.ContextEntry) {
	event := hook.EntryCommitted{
		Identity: runIdentity(run, identity.TurnID(entry.TurnID)),
		Entry:    entry,
	}
	ctx = context.WithoutCancel(ctx)
	if err := s.hookDispatcher.AssistantCommitted(ctx, event); err != nil {
		s.logger.Errorf("assistant committed hook failed: %v", err)
	}
	if entry.StopReason == string(providerapi.StopCompleted) || entry.StopReason == string(providerapi.StopSequence) {
		if err := s.hookDispatcher.FinalOutputCommitted(ctx, event); err != nil {
			s.logger.Errorf("final output committed hook failed: %v", err)
		}
	}
}
