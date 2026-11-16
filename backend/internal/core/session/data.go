package session

import (
	sessionentry "praxis/internal/core/entry"
	modelusage "praxis/internal/core/model/usage"
	"praxis/internal/identity"
)

type AgentHistory struct {
	HeadEntryID string
}

// Data 只持有结构、执行展示投影和未完整落盘的接纳结果。
// 已持久化正文由当前页面、Run 或 Loop 按自身生命周期持有，不在会话中缓存。
type Data struct {
	ID             identity.SessionID
	HeadEntryID    string
	Runs           []sessionentry.HistorySummary
	Usage          modelusage.Totals
	TotalRequests  uint64
	RunEndEntries  map[identity.RunID]string
	AgentHistories map[identity.AgentID]AgentHistory
	PendingEntries map[string]*sessionentry.ContextEntry
	PendingNodes   map[string]sessionentry.Node
	ModelRequests  map[identity.ModelRequestID]sessionentry.ModelRequestRecord
	RunStates      map[identity.RunID]sessionentry.RunRecord
	runIndexes     map[identity.RunID]int
}

func Open(id identity.SessionID) *Data {
	return &Data{
		ID:             id,
		RunEndEntries:  make(map[identity.RunID]string),
		AgentHistories: make(map[identity.AgentID]AgentHistory),
		PendingEntries: make(map[string]*sessionentry.ContextEntry),
		PendingNodes:   make(map[string]sessionentry.Node),
		ModelRequests:  make(map[identity.ModelRequestID]sessionentry.ModelRequestRecord),
		runIndexes:     make(map[identity.RunID]int),
		RunStates:      make(map[identity.RunID]sessionentry.RunRecord),
	}
}

func (s *Data) MainAgentID() identity.AgentID { return identity.AgentID(s.ID) }

func (s *Data) acceptEntry(agentID identity.AgentID, value sessionentry.ContextEntry) *sessionentry.ContextEntry {
	stored := new(value)
	runID := identity.RunID(value.RunID)
	state := sessionentry.Apply(s.RunStates[runID], stored)
	if state.ID != "" {
		s.RunStates[runID] = state
		s.SetRun(state.HistorySummary())
	}
	if request, _ := value.RequestFact(); request != nil {
		projection := *request
		projection.Partial = nil
		s.ModelRequests[request.ID] = projection
	}
	s.PendingEntries[value.ID] = stored
	s.PendingNodes[value.ID] = sessionentry.NewNode(agentID, value)
	if value.IsRun() {
		s.RunEndEntries[identity.RunID(value.RunID)] = value.ID
	}
	if agentID == s.MainAgentID() {
		s.HeadEntryID = value.ID
	} else {
		s.AgentHistories[agentID] = AgentHistory{HeadEntryID: value.ID}
	}
	return stored
}

func (s *Data) releaseEntry(id string) {
	delete(s.PendingEntries, id)
	delete(s.PendingNodes, id)
}

func (s *Data) RunIndex(id identity.RunID) int {
	if index, found := s.runIndexes[id]; found {
		return index
	}
	return -1
}

func (s *Data) SetRun(value sessionentry.HistorySummary) {
	if index := s.RunIndex(value.ID); index >= 0 {
		s.Usage.Subtract(s.Runs[index].Usage)
		s.TotalRequests -= s.Runs[index].ModelRequests
		s.Runs[index] = value
	} else {
		s.runIndexes[value.ID] = len(s.Runs)
		s.Runs = append(s.Runs, value)
	}
	s.Usage.Merge(value.Usage)
	s.TotalRequests += value.ModelRequests
}
