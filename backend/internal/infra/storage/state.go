package storage

import (
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/core/workflow"
	"praxis/internal/identity"
)

type entryKey struct {
	AgentID identity.AgentID
	ID      string
}

type storedEntry struct {
	SessionID identity.SessionID
	Record    entryRecord
	// UpdatedSequence 区分原始历史位置和最近一次正文更新，供 Context 恢复使用。
	UpdatedSequence uint64
}

type storedWait struct {
	SessionID identity.SessionID
	Value     workflow.WaitCondition
}

// state 只持有已载入会话的历史；sessions 成员表示该会话已完成重放。
// Session 元数据以 SQLite 为准，Project 配置直接读取文件。
type state struct {
	sequences  map[string]uint64
	sessions   map[identity.SessionID]sessionmodel.Session
	executions map[identity.AgentExecutionID]agentexecution.ExecutionRecord
	entries    map[entryKey]storedEntry
	entryOrder map[identity.SessionID][]entryKey
	contexts   map[identity.SessionID]contextRecord
	usage      map[string]providerapi.UsageRecord
	timings    map[string]timing.Record
	waits      map[identity.WaitConditionID]storedWait
}

func newState() state {
	return state{
		sequences:  make(map[string]uint64),
		sessions:   make(map[identity.SessionID]sessionmodel.Session),
		executions: make(map[identity.AgentExecutionID]agentexecution.ExecutionRecord),
		entries:    make(map[entryKey]storedEntry),
		entryOrder: make(map[identity.SessionID][]entryKey),
		contexts:   make(map[identity.SessionID]contextRecord),
		usage:      make(map[string]providerapi.UsageRecord),
		timings:    make(map[string]timing.Record),
		waits:      make(map[identity.WaitConditionID]storedWait),
	}
}

func (s *state) removeSession(id identity.SessionID) {
	delete(s.sessions, id)
	delete(s.sequences, id.String())
	delete(s.contexts, id)
	for _, key := range s.entryOrder[id] {
		delete(s.entries, key)
	}
	delete(s.entryOrder, id)
	for key, value := range s.usage {
		if value.SessionID == id.String() {
			delete(s.usage, key)
		}
	}
	for key, value := range s.timings {
		if execution, ok := s.executions[identity.AgentExecutionID(value.ExecutionID)]; ok && execution.SessionID == id {
			delete(s.timings, key)
		}
	}
	for key, value := range s.waits {
		if value.SessionID == id {
			delete(s.waits, key)
		}
	}
	for key, value := range s.executions {
		if value.SessionID == id {
			delete(s.executions, key)
		}
	}
}
