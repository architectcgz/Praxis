package run

import (
	"context"
	"errors"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
)

// Run 表示一次执行的内存实例，由 Coordinator 独占生命周期，Agent 身份及配置保存在执行记录中。
// 记录与执行证据仅由所属执行路径串行访问；查询读取 Session 已接纳的值，控制入口只提交控制意图。
// 执行证据共享 Session 已接纳的只读 Entry；Loop 按轮读取 Session 历史构建模型请求。
type Run struct {
	Session      SessionContext
	BindingID    string
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
	for _, entry := range entries {
		if entry.RunID != record.ID.String() {
			continue
		}
		run.appendEntry(entry)
	}
	if _, ok := run.Entry(record.InputEntryID); !ok {
		return nil, errors.New("execution input is missing from admitted entries")
	}
	run.record.Progress = run.record.Progress.RebuildEntryStats(record.ID.String(), run.entries)
	return run, nil
}

func (r *Run) Main() bool { return r.main }

func (r *Run) Snapshot() RunRecord { return r.record }

// Update 由所属执行路径推进状态和进度，不读取历史或重新计算条目统计。
func (r *Run) Update(change func(*RunRecord) error) error {
	value := r.record
	if err := change(&value); err != nil {
		return err
	}
	r.record = value
	return nil
}

func (r *Run) Entries() []sessionentry.ContextEntry {
	return sessionentry.Values(r.entries)
}

// Accept 按接纳顺序记录 Session 确定的执行条目，不维护模型上下文。
func (r *Run) Accept(value *sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	if value.RunID != r.record.ID.String() {
		return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
	}
	if prior, ok := r.Entry(value.ID); ok {
		candidate := *value
		candidate.CreatedAt = prior.CreatedAt
		if !reflect.DeepEqual(*prior, candidate) {
			return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
		}
		return *prior, false, nil
	}
	if !r.main {
		for _, call := range value.ToolCalls(r.record.SessionID, r.record.AgentID) {
			if _, found := r.Call(call.Key); found {
				return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
			}
		}
	}
	r.appendEntry(value)
	r.record.Progress.AcceptEntry(value)
	return *value, true, nil
}

func (r *Run) Call(key tool.CallKey) (sessionentry.ToolCall, bool) {
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

// Entry 返回 Session 已接纳的同一条目，调用方只读使用。
func (r *Run) Entry(id string) (*sessionentry.ContextEntry, bool) {
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

// SessionContext 在执行准备时绑定，执行期间不再通过 SessionID 查找可变会话。
type SessionContext interface {
	Entry(context.Context, string) (*sessionentry.ContextEntry, error)
	BranchEntries(context.Context, identity.BranchID) ([]*sessionentry.ContextEntry, error)
	SubagentEntries(context.Context, identity.AgentID) ([]*sessionentry.ContextEntry, error)
	AppendEntry(context.Context, identity.BranchID, sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error)
	AppendSubagentEntry(context.Context, identity.AgentID, sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error)
}
