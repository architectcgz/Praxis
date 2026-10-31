package session

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	appcontext "praxis/internal/core/context"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"slices"
	"sync"
)

// sessionHistory 拥有单个会话的恢复状态、当前 Context 和日志写入顺序。
// mu 覆盖首次加载、业务变更、完整记录追加及删除，不覆盖模型或 Hook 调用。
type sessionHistory struct {
	mu      sync.Mutex
	log     repository.SessionLog
	state   *historyState
	context *appcontext.Context
}

// historyState 是日志恢复后的业务状态，不另建消息或执行 ID 索引。
type historyState struct {
	session    sessionmodel.Session
	sequence   uint64
	executions []agentexecution.ExecutionRecord
	entries    []storedEntry
	context    *contextRecord
	usage      []providerapi.UsageRecord
	timings    []timing.Record
}

type storedEntry struct {
	Record entryRecord
	// UpdatedSequence 区分原始历史位置和最近一次正文更新，供 Context 恢复使用。
	UpdatedSequence uint64
}

func (s *Service) lockHistory(ctx context.Context, id identity.SessionID) (*sessionHistory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h := s.historyFor(id)
	h.mu.Lock()
	if err := ctx.Err(); err != nil {
		h.mu.Unlock()
		return nil, err
	}
	if h.state == nil {
		if err := s.loadHistory(ctx, id, h); err != nil {
			h.mu.Unlock()
			return nil, err
		}
	}
	return h, nil
}

func (s *Service) historyFor(id identity.SessionID) *sessionHistory {
	s.historiesMu.Lock()
	defer s.historiesMu.Unlock()
	h := s.histories[id]
	if h == nil {
		h = &sessionHistory{log: s.historyLog}
		s.histories[id] = h
	}
	return h
}

// loadHistory 只在持有会话锁时调用；失败不发布部分状态，后续访问可重新读取历史。
func (s *Service) loadHistory(ctx context.Context, id identity.SessionID, h *sessionHistory) error {
	value, err := s.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	h.state = &historyState{session: value}
	replay := logReplay{
		state:       h.state,
		callIndexes: make(map[sessionentry.Key]int),
	}
	if err := h.log.Read(ctx, id, replay.apply); err != nil {
		h.state = nil
		return err
	}
	if err := replay.repairToolResults(ctx, h); err != nil {
		h.state = nil
		return err
	}
	return nil
}

// appendEventLocked 在完整记录追加成功后推进序号，不逐条 fsync。
func (h *sessionHistory) appendEventLocked(ctx context.Context, typ, collection, key string, value any) error {
	event, err := newEvent(h.state.sequence+1, typ, collection, key, value)
	if err != nil {
		return err
	}
	data, err := encodeEvent(event)
	if err != nil {
		return err
	}
	if err := h.log.Append(ctx, h.state.session.ID, data); err != nil {
		return err
	}
	h.state.sequence = event.Sequence
	return nil
}

func (s *historyState) entryIndex(agentID identity.AgentID, id string) int {
	return slices.IndexFunc(s.entries, func(value storedEntry) bool {
		return value.Record.AgentID == agentID && value.Record.Data.ID == id
	})
}

func (s *historyState) executionIndex(id identity.AgentExecutionID) int {
	return slices.IndexFunc(s.executions, func(value agentexecution.ExecutionRecord) bool { return value.ID == id })
}

func (s *historyState) setExecution(value agentexecution.ExecutionRecord) {
	if index := s.executionIndex(value.ID); index >= 0 {
		s.executions[index] = value
	} else {
		s.executions = append(s.executions, value)
	}
}

func (s *historyState) setUsage(value providerapi.UsageRecord) {
	index := slices.IndexFunc(s.usage, func(prior providerapi.UsageRecord) bool { return prior.Key() == value.Key() })
	if index >= 0 {
		s.usage[index] = value
	} else {
		s.usage = append(s.usage, value)
	}
}

func (s *historyState) setTiming(value timing.Record) {
	index := slices.IndexFunc(s.timings, func(prior timing.Record) bool {
		return prior.ExecutionID == value.ExecutionID && prior.ID == value.ID
	})
	if index >= 0 {
		s.timings[index] = value
	} else {
		s.timings = append(s.timings, value)
	}
}

func capped[T any](values []T, limit int) []T {
	if limit <= 0 {
		return values
	}
	return values[:min(len(values), limit)]
}

func sorted[T any](values []T, compare func(T, T) int) []T {
	slices.SortFunc(values, compare)
	return values
}
