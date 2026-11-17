package run

import (
	"context"
	"errors"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/core/timing"
	"praxis/internal/identity"
	"praxis/internal/logging"
	"praxis/internal/repository"
	"sync"
)

type Dependencies struct {
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	Emitter        *coreevent.Emitter
	Sessions       repository.Session
	Clock          clock.Clock
	IDs            identity.IDGenerator
}

// SessionContext 在执行准备时绑定，执行期间不再通过 SessionID 查找可变会话。
type SessionContext interface {
	ModelEntries(context.Context, identity.AgentID, string) ([]*sessionentry.ContextEntry, error)
	Entry(context.Context, string) (*sessionentry.ContextEntry, error)
	Entries(context.Context) ([]*sessionentry.ContextEntry, error)
	SubagentEntries(context.Context, identity.AgentID) ([]*sessionentry.ContextEntry, error)
	BindHistory(identity.AgentID) sessionentry.AppendEntry
	CreateSubagent(context.Context, identity.AgentID) error
}

// Run 表示一次执行的内存实例，由 Coordinator 独占生命周期，Agent 身份及配置保存在执行记录中。
// 记录与执行证据仅由所属执行路径串行访问；查询读取 Session 已接纳的值，控制入口只提交控制意图。
// 执行证据共享 Session 已接纳的只读 Entry；Loop 按轮读取 Session 历史构建模型请求。
type Run struct {
	collaboration collaboration
	commitMu      sync.Mutex
	mu            sync.RWMutex
	deps          Dependencies
	Session       SessionContext

	cancel        context.CancelCauseFunc
	terminalSaved bool
	waiting       int
	main          bool
	appendFact    sessionentry.AppendEntry
	record        sessionentry.RunRecord
	entries       []*sessionentry.ContextEntry
	entryIndexes  map[string]int
}

func (r *Run) Main() bool { return r.main }

func (r *Run) Snapshot() sessionentry.RunRecord { r.mu.RLock(); defer r.mu.RUnlock(); return r.record }

func (r *Run) Entries() []sessionentry.ContextEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return sessionentry.Values(r.entries)
}

// AppendEntry 保存事实并接纳同一只读引用；通知和观察 Hook 由执行流程显式调用。
func (r *Run) AppendEntry(ctx context.Context, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
	r.commitMu.Lock()
	defer r.commitMu.Unlock()
	value.RunID = r.Snapshot().ID.String()
	if value.CreatedAt.IsZero() {
		value.CreatedAt = r.deps.Clock.Now()
	}
	stored, inserted, err := r.appendFact(ctx, value)
	if stored != nil && inserted {
		r.accept(stored)
	}
	if err != nil {
		return stored, inserted, storageFailure(err)
	}
	return stored, inserted, nil
}

// accept 只消费接纳边界给出的只读事实，不重新执行准入校验。
func (r *Run) accept(value *sessionentry.ContextEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.acceptEvidence(value)
	r.record = sessionentry.Apply(r.record, value)
}

// Entry 返回 Session 已接纳的同一条目，调用方只读使用。
func (r *Run) Entry(id string) (*sessionentry.ContextEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	index, found := r.entryIndexes[id]
	if !found {
		return nil, false
	}
	return r.entries[index], true
}

func (r *Run) acceptEvidence(entry *sessionentry.ContextEntry) {
	r.entryIndexes[entry.ID] = len(r.entries)
	r.entries = append(r.entries, entry)
}

func (r *Run) EventSource(turnID identity.TurnID) coreevent.EventSource {
	record := r.Snapshot()
	return coreevent.EventSource{
		SessionID: record.SessionID,
		AgentID:   record.AgentID,
		RunID:     record.ID,
		TurnID:    turnID,
	}
}

// BindCancellation 由 Coordinator 在执行准备时绑定，供所属子执行报告无法保存等失败直接停止本次执行。
func (r *Run) BindCancellation(cancel context.CancelCauseFunc) { r.cancel = cancel }
func (r *Run) Cancel(cause error)                              { r.cancel(cause) }

func (r *Run) BeginWaiting() {
	r.mu.Lock()
	r.waiting++
	first := r.waiting == 1
	r.mu.Unlock()
	if first {
		r.deps.Emitter.Emit(coreevent.Event{
			Source:  r.EventSource(""),
			Payload: coreevent.AgentStateChanged{State: agentmodel.AgentWaiting},
		})
	}
}

func (r *Run) EndWaiting() {
	r.mu.Lock()
	r.waiting--
	last := r.waiting == 0
	r.mu.Unlock()
	if last {
		r.deps.Emitter.Emit(coreevent.Event{
			Source:  r.EventSource(""),
			Payload: coreevent.AgentStateChanged{State: agentmodel.AgentExecuting},
		})
	}
}

func (r *Run) State() agentmodel.AgentState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.waiting > 0 && r.record.Snapshot().Active() {
		return agentmodel.AgentWaiting
	}
	return r.record.AgentState()
}

func (r *Run) TerminalSaved() bool { r.mu.RLock(); defer r.mu.RUnlock(); return r.terminalSaved }

// RecordModelRequest 与 RecordModelResponse 复用唯一事实提交入口；调用者只提供本次尝试的事实。
func (r *Run) RecordModelRequest(ctx context.Context, request sessionentry.ModelRequestRecord, measured *timing.Record) error {
	_, _, err := r.AppendEntry(ctx, sessionentry.ModelRequestEntry(request, measured))
	if err == nil {
		r.deps.Emitter.Emit(coreevent.Event{
			Source: r.EventSource(request.TurnID),
			Payload: coreevent.ModelRequestStarted{
				Request: request,
				Timing:  measured,
			},
		})
	}
	return err
}

func (r *Run) RecordModelResponse(ctx context.Context, request sessionentry.ModelRequestRecord, measured *timing.Record) error {
	stored, _, err := r.AppendEntry(ctx, sessionentry.ModelRequestEntry(request, measured))
	if err != nil {
		return err
	}
	if request.Status != sessionentry.RequestCompleted {
		r.deps.Emitter.Emit(coreevent.Event{
			Source:  r.EventSource(request.TurnID),
			Payload: coreevent.EntriesAppended{Entries: []sessionentry.ContextEntry{*stored}},
		})
	}
	r.deps.Emitter.Emit(coreevent.Event{
		Source:  r.EventSource(request.TurnID),
		Payload: coreevent.RequestResult(request, measured),
	})
	return err
}

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

func runIdentity(run *Run, turnID identity.TurnID) hook.Identity {
	record := run.Snapshot()
	return hook.Identity{
		SourceTaskID: record.SourceTaskID,
		SessionID:    record.SessionID,
		AgentID:      record.AgentID,
		RunID:        record.ID,
		TurnID:       turnID,
	}
}

func (s *Run) emitError(turnID identity.TurnID, err error) {
	message := "input failed"
	if failure, ok := errors.AsType[*loop.Failure](err); ok {
		if failure.Message() != "" {
			message = failure.Message()
		} else if loop.FailureClassification(err) == loop.FailureStorage {
			message = "Failed to save session entries."
		} else if loop.FailureClassification(err) == loop.FailureResourceLimit {
			message = "model context window exceeded"
		} else if loop.FailureClassification(err) == loop.FailureProvider {
			text := []rune(err.Error())
			if len(text) > 4096 {
				text = append(text[:4096], '.', '.', '.')
			}
			message = string(text)
		}
	}
	s.deps.Emitter.Emit(coreevent.Event{
		Source:  coreevent.SourceForRun(s, turnID),
		Payload: coreevent.ErrorOccurred{Message: message},
	})
}

func storageFailure(err error) error {
	return loop.NewFailure(loop.FailureStorage, err)
}
