package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	appcontext "praxis/internal/core/context"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/identity"
)

// loopExecution 绑定一次 Loop 调用的执行归属；生命周期与该次调用相同，不在共享 Service 中保存。
// 主执行先向 Session 提交，再将同一 Entry 引用交给 Run 的 Context 和执行证据。
type loopExecution struct {
	service *Service
	run     *agentexecution.Run
}

func (e *loopExecution) NextContext(ctx context.Context) (appcontext.Content, error) {
	return e.service.contextContent(ctx, e.run)
}

// AppendEntry 先向主会话追加条目，再记录本次执行证据；子执行只向本次 Run 接纳条目。
// 提交后的 Hook 不改变已接纳结果，重复条目不重复触发通知。
func (e *loopExecution) AppendEntry(ctx context.Context, value sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	inserted := false
	entry := new(value)
	if e.run.Main() {
		stored, added, err := e.run.Session.AppendEntry(ctx, e.run.Snapshot().BranchID, value)
		if err != nil {
			return sessionentry.ContextEntry{}, false, e.EntryFailure(err)
		}
		entry, inserted = stored, added
	}
	accepted, added, err := e.run.Accept(entry)
	if err != nil {
		return sessionentry.ContextEntry{}, false, e.EntryFailure(err)
	}
	if !e.run.Main() {
		inserted = added
	}
	if inserted && accepted.Role == role.Assistant && e.run.Main() {
		e.notifyAssistant(ctx, accepted)
	}
	return accepted, inserted, nil
}

func (e *loopExecution) PublishEntries(turnID identity.TurnID, entries []sessionentry.ContextEntry) {
	if len(entries) == 0 || e.service.emitter == nil {
		return
	}
	task := e.run.Snapshot()
	e.service.emitter.Emit(coreevent.Event{
		Scope: coreevent.Scope{
			SessionID:   task.SessionID,
			BranchID:    task.BranchID,
			BindingID:   e.run.BindingID,
			AgentID:     task.AgentID,
			ExecutionID: task.ID,
			TurnID:      turnID,
		},
		Payload: coreevent.EntriesAppended{
			Entries:   entries,
			Transient: !e.run.Main(),
		},
	})
}

func (e *loopExecution) NextRequestSequence(ctx context.Context) (uint64, error) {
	sequence, err := e.service.NextRequestSequence(ctx, e.run)
	if err != nil {
		return 0, e.EntryFailure(err)
	}
	return sequence, nil
}

func (e *loopExecution) RecordModelRequest(ctx context.Context, sequence uint64, iteration bool) error {
	if err := e.service.RecordModelRequest(ctx, e.run, sequence, iteration); err != nil {
		return e.EntryFailure(err)
	}
	return nil
}

func (e *loopExecution) RecordModelOutput(ctx context.Context, size int) error {
	if err := e.service.RecordModelOutput(ctx, e.run, size); err != nil {
		return e.EntryFailure(err)
	}
	return nil
}

func (e *loopExecution) EntryFailure(err error) error {
	if e.run.Main() {
		return loop.NewFailure(loop.FailureStorage, err)
	}
	return loop.NewFailure(loop.FailureContract, err)
}

func (e *loopExecution) identity(turnID identity.TurnID) hook.Identity {
	task := e.run.Snapshot()
	return hook.Identity{
		SourceTaskID: task.SourceTaskID,
		SessionID:    task.SessionID,
		AgentID:      task.AgentID,
		ExecutionID:  task.ID,
		TurnID:       turnID,
	}
}

func (e *loopExecution) notifyAssistant(ctx context.Context, entry sessionentry.ContextEntry) {
	event := hook.EntryCommitted{
		Identity: e.identity(identity.TurnID(entry.TurnID)),
		Entry:    entry,
	}
	ctx = context.WithoutCancel(ctx)
	if err := e.service.hookDispatcher.AssistantCommitted(ctx, event); err != nil {
		e.service.logger.Errorf("assistant committed hook failed: %v", err)
	}
	if entry.StopReason == string(providerapi.StopCompleted) || entry.StopReason == string(providerapi.StopSequence) {
		if err := e.service.hookDispatcher.FinalOutputCommitted(ctx, event); err != nil {
			e.service.logger.Errorf("final output committed hook failed: %v", err)
		}
	}
}
