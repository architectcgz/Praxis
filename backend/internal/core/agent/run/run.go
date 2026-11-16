package run

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/identity"
	"sync"
)

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

// SessionContext 在执行准备时绑定，执行期间不再通过 SessionID 查找可变会话。
type SessionContext interface {
	ModelEntries(context.Context, identity.AgentID, string) ([]*sessionentry.ContextEntry, error)
	Entry(context.Context, string) (*sessionentry.ContextEntry, error)
	Entries(context.Context) ([]*sessionentry.ContextEntry, error)
	SubagentEntries(context.Context, identity.AgentID) ([]*sessionentry.ContextEntry, error)
	BindHistory(identity.AgentID) sessionentry.AppendEntry
	CreateSubagent(context.Context, identity.AgentID) error
}

func (r *Run) EventSource(turnID identity.TurnID) coreevent.EventSource {
	record := r.Snapshot()
	return coreevent.EventSource{
		SessionID: record.SessionID,
		AgentID:   record.AgentID,

		RunID:  record.ID,
		TurnID: turnID,
	}
}

// Cancel 由 Coordinator 在执行准备时绑定，供所属子执行报告无法保存等失败直接停止本次执行。
func (r *Run) BindCancellation(cancel context.CancelCauseFunc) { r.cancel = cancel }
func (r *Run) Cancel(cause error)                              { r.cancel(cause) }

func (r *Run) BeginWaiting() {
	r.mu.Lock()
	r.waiting++
	first := r.waiting == 1
	r.mu.Unlock()
	if first {
		r.deps.Emitter.Emit(coreevent.Event{Source: r.EventSource(""), Payload: coreevent.AgentStateChanged{State: agentmodel.AgentWaiting}})
	}
}
func (r *Run) EndWaiting() {
	r.mu.Lock()
	r.waiting--
	last := r.waiting == 0
	r.mu.Unlock()
	if last {
		r.deps.Emitter.Emit(coreevent.Event{Source: r.EventSource(""), Payload: coreevent.AgentStateChanged{State: agentmodel.AgentExecuting}})
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
