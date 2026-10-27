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
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// loopExecution 绑定一次 Loop 调用的执行归属；生命周期与该次调用相同，不在共享 Service 中保存。
// 主执行通过 Session 读写历史；Run 仅保留执行证据，Loop 取得独立模型投影。
type loopExecution struct {
	service *Service
	run     *agentexecution.Run
}

func (e *loopExecution) NextContext(ctx context.Context) (appcontext.ModelContext, error) {
	value, err := e.service.buildContext(ctx, e.run)
	if err == nil {
		return value, nil
	}
	if loop.FailureClassification(err) == loop.FailureStorage {
		return appcontext.ModelContext{}, err
	}
	if _, ok := errors.AsType[*hook.Error](err); ok {
		return appcontext.ModelContext{}, loop.NewFailure(loop.FailureInternal, err)
	}
	if _, ok := errors.AsType[*appcontext.ContextWindowExceededError](err); ok {
		return appcontext.ModelContext{}, err
	}
	return appcontext.ModelContext{}, loop.NewFailure(loop.FailureContract, err)
}

// AppendEntry 先向主会话追加条目，再记录本次执行证据；子执行只写入私有历史。
// 提交后的 Hook 不改变已接纳结果，重复条目不重复触发通知。
func (e *loopExecution) AppendEntry(ctx context.Context, value sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	inserted := false
	if e.run.Main() {
		task := e.run.Snapshot()
		stored, added, err := e.service.history.AppendEntry(ctx, task.SessionID, value)
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

// UpdateToolResult 先更新主会话历史，再同步执行证据；子执行只更新私有历史和上下文。
func (e *loopExecution) UpdateToolResult(ctx context.Context, id, text string) (sessionentry.ContextEntry, error) {
	prior, found := e.run.Entry(id)
	if !found {
		return sessionentry.ContextEntry{}, e.EntryFailure(errorcode.ErrNotFound)
	}
	task := e.run.Snapshot()
	if prior.ExecutionID != task.ID.String() {
		return sessionentry.ContextEntry{}, e.EntryFailure(errorcode.ErrRequestConflict)
	}
	if e.run.Main() {
		if _, err := e.service.history.UpdateToolResult(ctx, task.SessionID, id, text); err != nil {
			return sessionentry.ContextEntry{}, e.EntryFailure(err)
		}
	}
	stored, err := e.run.UpdateToolResult(id, text)
	if err != nil {
		return sessionentry.ContextEntry{}, e.EntryFailure(err)
	}
	return stored, nil
}

// ReplaceContext 在摘要已接纳后直接保存压缩结果，不进行前后差异合并。
func (e *loopExecution) ReplaceContext(ctx context.Context, entries []sessionentry.ContextEntry) error {
	task := e.run.Snapshot()
	values := make([]sessionentry.ContextEntry, 0, len(entries))
	for _, entry := range entries {
		// 当前任务输入的保护只属于本轮，不能让它在后续任务中永久不可压缩。
		if entry.ID == task.InputEntryID {
			entry.Protected = false
		}
		values = append(values, entry)
	}
	var err error
	if e.run.Main() {
		err = e.service.history.ReplaceContext(ctx, task.SessionID, values)
	} else {
		err = e.run.ReplaceContext(values)
	}
	if err != nil {
		return e.EntryFailure(err)
	}
	return nil
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
