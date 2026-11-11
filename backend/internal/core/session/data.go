package session

import (
	"maps"
	agentrun "praxis/internal/core/agent/run"
	modelusage "praxis/internal/core/model/usage"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"slices"
)

type AgentHistory struct {
	BranchID    identity.BranchID
	HeadEntryID string
}

// Data 只持有结构、执行展示投影和未完整落盘的接纳结果。
// 已持久化正文由当前页面、Run 或 Loop 按自身生命周期持有，不在会话中缓存。
type Data struct {
	ID             identity.SessionID
	RootBranchID   identity.BranchID
	Branches       []Branch
	RecentBranch   identity.BranchID
	Runs           []agentrun.HistorySummary
	Usage          modelusage.Totals
	TotalRequests  uint64
	RunEndEntries  map[identity.RunID]string
	AgentHistories map[identity.AgentID]AgentHistory
	PendingEntries map[string]*sessionentry.ContextEntry
	PendingNodes   map[string]sessionentry.Node
	ModelRequests  map[identity.ModelRequestID]agentrun.ModelRequestRecord
	PendingRuns    map[identity.RunID]agentrun.RunRecord
	runIndexes     map[identity.RunID]int
}

func Open(id identity.SessionID) *Data {
	return &Data{
		ID:             id,
		RunEndEntries:  make(map[identity.RunID]string),
		AgentHistories: make(map[identity.AgentID]AgentHistory),
		PendingEntries: make(map[string]*sessionentry.ContextEntry),
		PendingNodes:   make(map[string]sessionentry.Node),
		ModelRequests:  make(map[identity.ModelRequestID]agentrun.ModelRequestRecord),
		runIndexes:     make(map[identity.RunID]int),
		PendingRuns:    make(map[identity.RunID]agentrun.RunRecord),
	}
}

func (s *Data) MainAgentID() identity.AgentID { return identity.AgentID(s.ID) }

func (s *Data) Branch(id identity.BranchID) *Branch {
	for index := range s.Branches {
		if s.Branches[index].ID == id {
			return &s.Branches[index]
		}
	}
	return nil
}

func (s *Data) BranchByRequest(requestID string) *Branch {
	for index := range s.Branches {
		if s.Branches[index].RequestID == requestID {
			return &s.Branches[index]
		}
	}
	return nil
}

func (s *Data) AcceptBranch(branch Branch) {
	s.Branches = append(s.Branches, branch)
	s.RecentBranch = branch.ID
}

func (s *Data) acceptEntry(branchID identity.BranchID, agentID identity.AgentID, value sessionentry.ContextEntry) *sessionentry.ContextEntry {
	stored := new(value)
	s.PendingEntries[value.ID] = stored
	s.PendingNodes[value.ID] = sessionentry.NewNode(branchID, agentID, value)
	if value.IsRun() {
		s.RunEndEntries[identity.RunID(value.RunID)] = value.ID
	}
	if agentID == s.MainAgentID() {
		branch := s.Branch(branchID)
		branch.HeadEntryID, branch.UpdatedAt = value.ID, value.CreatedAt
	} else {
		s.AgentHistories[agentID] = AgentHistory{BranchID: branchID, HeadEntryID: value.ID}
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

func (s *Data) SetRun(value agentrun.HistorySummary) {
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

func (s *Data) RemoveBranch(id identity.BranchID) {
	s.Branches = slices.DeleteFunc(s.Branches, func(branch Branch) bool { return branch.ID == id })
	s.Runs = slices.DeleteFunc(s.Runs, func(run agentrun.HistorySummary) bool {
		if run.BranchID != id {
			return false
		}
		s.Usage.Subtract(run.Usage)
		s.TotalRequests -= run.ModelRequests
		delete(s.RunEndEntries, run.ID)
		delete(s.PendingRuns, run.ID)
		return true
	})
	clear(s.runIndexes)
	for index, run := range s.Runs {
		s.runIndexes[run.ID] = index
	}
	maps.DeleteFunc(s.AgentHistories, func(_ identity.AgentID, history AgentHistory) bool { return history.BranchID == id })
	for entryID, node := range s.PendingNodes {
		if node.BranchID == id {
			s.releaseEntry(entryID)
		}
	}
	maps.DeleteFunc(s.ModelRequests, func(_ identity.ModelRequestID, request agentrun.ModelRequestRecord) bool {
		return s.RunIndex(request.RunID) < 0
	})
}
