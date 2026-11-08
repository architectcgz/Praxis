package run

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
	"sync"
)

// Run 表示一次执行的内存实例，创建并持有专属 Agent，由 Coordinator 独占生命周期。
// 执行证据共享 Session 已接纳的只读 Entry；模型上下文由 Loop 独立维护。
type Run struct {
	Session      SessionContext
	BindingID    string
	Agent        *agentmodel.Agent
	mu           sync.RWMutex
	main         bool
	record       RunRecord
	entries      []*sessionentry.ContextEntry
	entryIndexes map[string]int
}

func NewRun(record RunRecord, main bool, entries []*sessionentry.ContextEntry) (*Run, error) {
	run := &Run{
		main:         main,
		record:       record,
		entryIndexes: make(map[string]int),
	}
	run.record.Config = record.Config.Clone()
	for _, entry := range entries {
		if main && (entry.RunID != record.ID.String() || entry.IsSummary()) {
			continue
		}
		run.appendEntry(entry)
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

func (r *Run) Snapshot() RunRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value := r.record
	value.Config = value.Config.Clone()
	return value
}

// Update 在回调成功后发布新进度；回调不能调用 Run 的其他方法。
func (r *Run) Update(change func(*RunRecord) error) error {
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

func (r *Run) Entry(id string) (sessionentry.ContextEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, found := r.entry(id)
	if !found {
		return sessionentry.ContextEntry{}, false
	}
	return *entry, true
}

// Accept 按接纳顺序记录 Session 确定的执行条目，不维护模型上下文。
func (r *Run) Accept(value *sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if value.RunID != r.record.ID.String() {
		return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
	}
	if prior, ok := r.entry(value.ID); ok {
		candidate := *value
		candidate.CreatedAt = prior.CreatedAt
		if !reflect.DeepEqual(*prior, candidate) {
			return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
		}
		return *prior, false, nil
	}
	if !r.main {
		for _, call := range value.ToolCalls(r.record.SessionID, r.record.AgentID) {
			if _, found := r.call(call.Key); found {
				return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
			}
		}
	}
	r.appendEntry(value)
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
		if entry.RunID != key.RunID.String() {
			continue
		}
		for _, block := range entry.Content {
			if result, ok := block.(sessionentry.ToolResultBlock); ok && result.CallID == key.CallID {
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
	AgentEntries(context.Context, identity.AgentID, identity.BranchID) ([]*sessionentry.ContextEntry, error)
	AppendEntry(context.Context, identity.BranchID, sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error)
	AppendSubagentEntry(context.Context, identity.AgentID, identity.BranchID, sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error)
}
