package execution

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/agent/execution/timing"
	appcontext "praxis/internal/core/context"
	coreevent "praxis/internal/core/event"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
	"sync"
	"time"
)

// Run 表示一次执行的内存实例，创建并持有专属 Agent，由 Coordinator 独占生命周期。
// Context 与执行证据分别维护引用列表，共享只读 Entry；子执行的条目不写入 Session。
type Run struct {
	Session      SessionContext
	BindingID    string
	Agent        *agentmodel.Agent
	mu           sync.RWMutex
	main         bool
	record       ExecutionRecord
	entries      []*sessionentry.ContextEntry
	entryIndexes map[string]int
	context      appcontext.Context
	sequence     uint64
	Timing       *timing.Recorder
	timings      map[string]timing.Record
}

func NewRun(record ExecutionRecord, main bool, entries []*sessionentry.ContextEntry) (*Run, error) {
	run := &Run{
		main:         main,
		record:       record,
		context:      appcontext.BuildContext(entries),
		entryIndexes: make(map[string]int),
	}
	run.record.Config = record.Config.Clone()
	for _, entry := range entries {
		if main && (entry.ExecutionID != record.ID.String() || entry.IsSummary()) {
			continue
		}
		run.appendEntry(entry)
		run.sequence = max(run.sequence, entry.Sequence)
	}
	if _, ok := run.entry(record.InputEntryID); !ok {
		return nil, errors.New("execution input is missing from admitted entries")
	}
	kind := agentmodel.KindSubagent
	if main {
		kind = agentmodel.KindMain
	}
	run.Agent = agentmodel.NewAgent(record.AgentID, record.SessionID, record.Config.AgentDefinitionID, kind, record.ID, record.CreatedAt)
	run.deriveProgress()
	return run, nil
}

func (r *Run) Main() bool { return r.main }

func (r *Run) EventScope(turnID identity.TurnID) coreevent.Scope {
	record := r.Snapshot()
	return coreevent.Scope{
		SessionID:   record.SessionID,
		BranchID:    record.BranchID,
		BindingID:   r.BindingID,
		AgentID:     record.AgentID,
		ExecutionID: record.ID,
		TurnID:      turnID,
	}
}

func (r *Run) RecordTiming(record timing.Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if record.DurationMS != nil {
		record.DurationMS = new(*record.DurationMS)
	}
	if record.FirstResponseMS != nil {
		record.FirstResponseMS = new(*record.FirstResponseMS)
	}
	if r.timings == nil {
		r.timings = make(map[string]timing.Record)
	}
	r.timings[record.ID] = record
}

func (r *Run) Timings() []timing.Record {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]timing.Record, 0, len(r.timings))
	for _, record := range r.timings {
		if record.DurationMS != nil {
			record.DurationMS = new(*record.DurationMS)
		}
		if record.FirstResponseMS != nil {
			record.FirstResponseMS = new(*record.FirstResponseMS)
		}
		result = append(result, record)
	}
	return result
}

func (r *Run) Snapshot() ExecutionRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value := r.record
	value.Config = value.Config.Clone()
	return value
}

// Update 在回调成功后发布新进度；回调不能调用 Run 的其他方法。
func (r *Run) Update(change func(*ExecutionRecord) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.record
	value.Config = value.Config.Clone()
	if err := change(&value); err != nil {
		return err
	}
	r.record = value
	r.deriveProgress()
	return nil
}

func (r *Run) Entries() []sessionentry.ContextEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return sessionentry.Values(r.entries)
}

// ContextEntries 只返回本次执行的引用列表快照，不回读 Session 或复制 Entry。
func (r *Run) ContextEntries() []*sessionentry.ContextEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.context.Entries()
}

func (r *Run) Entry(id string) (sessionentry.ContextEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, found := r.entry(id)
	if !found {
		return sessionentry.ContextEntry{}, false
	}
	return *entry, true
}

// Accept 记录本次执行证据；主执行使用 Session 分配的序号和时间。
// 摘要只更新 Context，不改变此前的执行证据；子执行在发布引用前补齐序号和时间。
func (r *Run) Accept(value *sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if value.ExecutionID != r.record.ID.String() {
		return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
	}
	if prior, ok := r.entry(value.ID); ok {
		candidate := *value
		candidate.Sequence, candidate.CreatedAt = prior.Sequence, prior.CreatedAt
		if !reflect.DeepEqual(*prior, candidate) {
			return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
		}
		return *prior, false, nil
	}
	if !r.main {
		value.Sequence, value.CreatedAt = r.sequence+1, time.Now().UTC()
		if err := value.Validate(); err != nil {
			return sessionentry.ContextEntry{}, false, err
		}
		// 子执行的调用标识只在本次执行条目中准入；Main 由 Session 在追加前检查。
		for _, call := range value.ToolCalls(r.record.SessionID, r.record.AgentID) {
			if _, found := r.call(call.Key); found {
				return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
			}
		}
	}
	r.appendEntry(value)
	r.context.Append(value)
	r.sequence = value.Sequence
	r.deriveProgress()
	return *value, true, nil
}

func (r *Run) Call(key tool.CallKey) (sessionentry.ToolCall, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.call(key)
}

func (r *Run) call(key tool.CallKey) (sessionentry.ToolCall, bool) {
	for _, entry := range r.entries {
		for _, call := range entry.ToolCalls(r.record.SessionID, r.record.AgentID) {
			if call.Key == key {
				return call, true
			}
		}
	}
	return sessionentry.ToolCall{}, false
}

func (r *Run) Result(key tool.CallKey) (sessionentry.ContextEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.result(key)
}

func (r *Run) result(key tool.CallKey) (sessionentry.ContextEntry, bool) {
	for _, entry := range r.entries {
		if entry.ExecutionID != key.ExecutionID.String() {
			continue
		}
		for _, block := range entry.Content {
			if block.Kind == sessionentry.BlockToolResult && block.CallID == key.CallID {
				return *entry, true
			}
		}
	}
	return sessionentry.ContextEntry{}, false
}

func (r *Run) entry(id string) (*sessionentry.ContextEntry, bool) {
	index, found := r.entryIndexes[id]
	if !found {
		return nil, false
	}
	return r.entries[index], true
}

func (r *Run) appendEntry(entry *sessionentry.ContextEntry) {
	r.entryIndexes[entry.ID] = len(r.entries)
	r.entries = append(r.entries, entry)
}

func (r *Run) deriveProgress() {
	r.record.Progress = r.record.Progress.WithEntries(r.record.ID.String(), r.entries)
}

// SessionContext 在执行准备时绑定，执行期间不再通过 SessionID 查找可变会话。
type SessionContext interface {
	BranchEntries(context.Context, identity.BranchID) ([]*sessionentry.ContextEntry, error)
	AppendEntry(context.Context, identity.BranchID, sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error)
}
