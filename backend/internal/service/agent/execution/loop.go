package execution

import (
	"context"
	"errors"
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
// Run 的历史由业务提交路径更新，Loop 只能取得独立的模型投影。
type loopExecution struct {
	service *Service
	run     *agentexecution.Run
}

func (e *loopExecution) NextContext(ctx context.Context) (appcontext.ModelContext, error) {
	value, err := e.service.buildContext(ctx, e.run)
	if err == nil {
		return value, nil
	}
	if _, ok := errors.AsType[*hook.Error](err); ok {
		return appcontext.ModelContext{}, loop.NewFailure(loop.FailureInternal, err)
	}
	if _, ok := errors.AsType[*appcontext.ContextWindowExceededError](err); ok {
		return appcontext.ModelContext{}, err
	}
	return appcontext.ModelContext{}, loop.NewFailure(loop.FailureContract, err)
}

// AppendEntry 先由 Main 仓储接纳，再更新本次历史；子执行只写入本次历史。
// 提交后的 Hook 不改变已接纳结果，重复条目不重复触发通知。
func (e *loopExecution) AppendEntry(ctx context.Context, value sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	inserted := false
	if e.run.Main() {
		task := e.run.Snapshot()
		stored, added, err := e.service.entries.Append(ctx, sessionentry.Record{
			SessionID: task.SessionID,
			AgentID:   task.AgentID,
			Data:      value,
		})
		if err != nil {
			return sessionentry.ContextEntry{}, false, e.EntryFailure(err)
		}
		value, inserted = stored, added
	}
	accepted, added, err := e.run.Accept(value)
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
