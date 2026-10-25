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

// Runtime 由 Coordinator 独占生命周期；执行路径推进历史，查询方只取得独立快照。
// Main 加载跨执行历史；子执行只接收显式输入，不要求存在持久化 Execution。
type Runtime struct {
	mu       sync.RWMutex
	main     bool
	record   Execution
	history  History
	sequence uint64
	Context  appcontext.ModelContext
	Timing   *timing.Recorder
	timings  map[string]timing.Record
}

func NewRuntime(record Execution, main bool, entries []sessionentry.ContextEntry) (*Runtime, error) {
	runtime := &Runtime{main: main, record: record}
	runtime.record.Config = record.Config.Clone()
	for _, entry := range entries {
		if _, err := runtime.history.Append(entry); err != nil {
			return nil, err
		}
		runtime.sequence = max(runtime.sequence, entry.Sequence)
	}
	if _, ok := runtime.history.Get(record.InputEntryID); !ok {
		return nil, errors.New("execution input is missing from admitted history")
	}
	runtime.deriveProgress()
	return runtime, nil
}

func (r *Runtime) Main() bool { return r.main }

func (r *Runtime) RecordTiming(record timing.Record) {
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

func (r *Runtime) Timings() []timing.Record {
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

func (r *Runtime) Snapshot() Execution {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value := r.record
	value.Config = value.Config.Clone()
	return value
}

// Update 在仓储内存接纳变更后发布新进度，不等待落盘；回调不能调用 Runtime 的其他方法。
func (r *Runtime) Update(change func(*Execution) error) error {
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

func (r *Runtime) Entries() []sessionentry.ContextEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.history.List()
}

func (r *Runtime) ContextEntries() []sessionentry.ContextEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.history.ContextHistory()
}

func (r *Runtime) Entry(id string) (sessionentry.ContextEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.history.Get(id)
}

// Accept 对 Main 使用仓储内存分配的序号和时间；子执行在本地分配，不占用会话序号。
func (r *Runtime) Accept(value sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	inserted, err := r.history.Append(value)
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	r.sequence = value.Sequence
	r.deriveProgress()
	return value.Clone(), inserted, nil
}

func (r *Runtime) Call(key tool.CallKey) (sessionentry.ToolCall, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.call(key)
}

func (r *Runtime) call(key tool.CallKey) (sessionentry.ToolCall, bool) {
	for _, entry := range r.history.entries {
		for _, call := range entry.ToolCalls(r.record.SessionID, r.record.AgentID) {
			if call.Key == key {
				return call, true
			}
		}
	}
	return sessionentry.ToolCall{}, false
}

func (r *Runtime) Result(key tool.CallKey) (sessionentry.ContextEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.result(key)
}

func (r *Runtime) result(key tool.CallKey) (sessionentry.ContextEntry, bool) {
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

func (r *Runtime) deriveProgress() {
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
