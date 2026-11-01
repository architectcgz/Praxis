package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/identity"
)

// appendEntry 由执行服务决定记录归属；主执行先更新 Session，再将同一引用交给 Run。
// 日志失败保留双方已接纳的内存事实；重复条目不再次发送 Hook 或条目通知。
func (s *Service) appendEntry(ctx context.Context, run *agentexecution.Run, value sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	entry := new(value)
	inserted := false
	var writeErr error
	if run.Main() {
		stored, added, err := run.Session.AppendEntry(ctx, run.Snapshot().BranchID, value)
		if err != nil && !added {
			return sessionentry.ContextEntry{}, false, entryFailure(run, err)
		}
		entry, inserted, writeErr = stored, added, err
	}
	accepted, added, err := run.Accept(entry)
	if err != nil {
		return sessionentry.ContextEntry{}, false, entryFailure(run, err)
	}
	if !run.Main() {
		inserted = added
	}
	if inserted {
		if accepted.Role == role.Assistant && run.Main() {
			s.notifyAssistant(ctx, run, accepted)
		}
		s.emitter.Emit(coreevent.Event{
			Source: coreevent.SourceForRun(run, identity.TurnID(accepted.TurnID)),
			Payload: coreevent.EntriesAppended{
				Entries:   []sessionentry.ContextEntry{accepted},
				Transient: !run.Main(),
			},
		})
	}
	if writeErr != nil {
		return accepted, inserted, entryFailure(run, writeErr)
	}
	return accepted, inserted, nil
}

func (s *Service) notifyAssistant(ctx context.Context, run *agentexecution.Run, entry sessionentry.ContextEntry) {
	event := hook.EntryCommitted{
		Identity: executionIdentity(run, identity.TurnID(entry.TurnID)),
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
