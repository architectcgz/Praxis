package execution

import (
	"errors"
	"praxis/internal/core/agent/execution/timing"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
	"reflect"
	"sync"
	"time"
)

// Run 表示一次执行的内存实例，由 Coordinator 独占生命周期。
// 主执行只保留本次执行证据，不作为会话上下文的数据源；子执行持有完整私有历史。
type Run struct {
	mu       sync.RWMutex
	main     bool
	record   ExecutionRecord
	history  History
	context  *appcontext.Context
	sequence uint64
	Timing   *timing.Recorder
	timings  map[string]timing.Record
}

func NewRun(record ExecutionRecord, main bool, entries []sessionentry.ContextEntry) (*Run, error) {
	run := &Run{main: main, record: record}
	run.record.Config = record.Config.Clone()
	for _, entry := range entries {
		if main && (entry.ExecutionID != record.ID.String() || entry.IsSummary()) {
			continue
		}
		run.history.Append(entry)
		run.sequence = max(run.sequence, entry.Sequence)
	}
	if _, ok := run.history.Get(record.InputEntryID); !ok {
		return nil, errors.New("execution input is missing from admitted history")
	}
	if !main {
		run.context = appcontext.NewContext(entries)
	}
	run.deriveProgress()
	return run, nil
}

func (r *Run) Main() bool { return r.main }

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

// Update 在仓储内存接纳变更后发布新进度，不等待落盘；回调不能调用 Run 的其他方法。
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
	return r.history.List()
}

// ContextEntries 只读取子执行已经维护的上下文，不再从私有历史重建。
func (r *Run) ContextEntries() ([]sessionentry.ContextEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.context == nil {
		return nil, errors.New("main execution context belongs to the session")
	}
	return r.context.Entries(), nil
}

// ReplaceContext 直接替换子执行上下文；主执行必须通过 Session 更新。
func (r *Run) ReplaceContext(entries []sessionentry.ContextEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.context == nil {
		return errors.New("main execution context belongs to the session")
	}
	r.context.Replace(entries)
	return nil
}

func (r *Run) Entry(id string) (sessionentry.ContextEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.history.Get(id)
}

// Accept 记录本次执行证据；主执行使用 Session 分配的序号和时间。
// 主执行摘要只属于会话历史，不重复保存在 Run；子执行在本地分配序号和时间。
func (r *Run) Accept(value sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if value.ExecutionID != r.record.ID.String() {
		return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
	}
	if r.main && value.IsSummary() {
		return value.Clone(), false, value.Validate()
	}
	if prior, ok := r.history.Get(value.ID); ok {
		candidate := value.Clone()
		candidate.Sequence, candidate.CreatedAt = prior.Sequence, prior.CreatedAt
		if !reflect.DeepEqual(prior, candidate) {
			return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
		}
		return prior, false, nil
	}
	if !r.main {
		value.Sequence, value.CreatedAt = r.sequence+1, time.Now().UTC()
	}
	if err := value.Validate(); err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	// 模型可能在后续轮次重复生成调用标识，不能让新调用覆盖已有配对关系。
	for _, call := range value.ToolCalls(r.record.SessionID, r.record.AgentID) {
		if _, found := r.call(call.Key); found {
			return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
		}
	}
	r.history.Append(value)
	r.sequence = value.Sequence
	if r.context != nil {
		r.context.Append(value)
	}
	r.deriveProgress()
	return value.Clone(), true, nil
}

func (r *Run) Call(key tool.CallKey) (sessionentry.ToolCall, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.call(key)
}

func (r *Run) call(key tool.CallKey) (sessionentry.ToolCall, bool) {
	for _, entry := range r.history.entries {
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
	for _, entry := range r.history.entries {
		if entry.ExecutionID != key.ExecutionID.String() {
			continue
		}
		for _, block := range entry.Content {
			if block.Kind == sessionentry.BlockToolResult && block.CallID == key.CallID {
				return entry.Clone(), true
			}
		}
	}
	return sessionentry.ContextEntry{}, false
}

func (r *Run) deriveProgress() {
	progress := &r.record.Progress
	progress.OutputBytes, progress.ToolCalls = progress.ModelOutputBytes, 0
	progress.FinalAssistantID, progress.FinalStopReason = "", ""
	var last sessionentry.ContextEntry
	for _, entry := range r.history.entries {
		if entry.ExecutionID != r.record.ID.String() {
			continue
		}
		if entry.Role == role.Assistant && entry.Sequence > last.Sequence {
			last = entry
		}
		for _, block := range entry.Content {
			if block.Kind == sessionentry.BlockToolCall {
				progress.ToolCalls++
			}
			if block.Kind == sessionentry.BlockToolResult {
				progress.OutputBytes += int64(len(block.Text))
			}
		}
	}
	if last.StopReason == "completed" || last.StopReason == "stop_sequence" {
		progress.FinalAssistantID, progress.FinalStopReason = last.ID, last.StopReason
	}
}
