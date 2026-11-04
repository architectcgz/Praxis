package session

import (
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"slices"
)

// Data 拥有完整会话数据；分支选择和输入队列属于页面绑定。
// 重放完成后由 Session 接管并通过读写锁保护；发布后的 Entry 及内部载荷不再修改。
type Data struct {
	ID              identity.SessionID
	Entries         []*sessionentry.ContextEntry
	Branches        []Branch
	RecentBranch    identity.BranchID
	Runs            []agentrun.RunRecord
	ModelRequests   []agentrun.ModelRequestRecord
	SubagentEntries map[identity.AgentID][]*sessionentry.ContextEntry
	entryIndexes    map[string]int
}

func Open(id identity.SessionID) *Data {
	return &Data{
		ID:              id,
		SubagentEntries: make(map[identity.AgentID][]*sessionentry.ContextEntry),
		entryIndexes:    make(map[string]int),
	}
}

func (s *Data) MainAgentID() identity.AgentID { return identity.AgentID(s.ID) }

func (s *Data) Entry(id string) (*sessionentry.ContextEntry, bool) {
	index, found := s.entryIndexes[id]
	if !found {
		return nil, false
	}
	return s.Entries[index], true
}

// AcceptEntry 只接收已经确认归属的完整 Entry，不保存模型请求的精简副本。
func (s *Data) AcceptEntry(branchID identity.BranchID, entry sessionentry.ContextEntry) *sessionentry.ContextEntry {
	stored := new(entry)
	s.Entries = append(s.Entries, stored)
	s.entryIndexes[entry.ID] = len(s.Entries) - 1
	branch := s.Branch(branchID)
	branch.HeadEntryID, branch.UpdatedAt = entry.ID, entry.CreatedAt
	s.RecentBranch = branch.ID
	return stored
}

// hasAncestor 判断节点是否位于指定 Head 的历史路径上，包含 Head 自身。
func (s *Data) hasAncestor(head, entryID string) bool {
	for head != "" {
		if head == entryID {
			return true
		}
		entry, _ := s.Entry(head)
		head = entry.ParentID
	}
	return false
}

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

// Path 按父节点回溯当前路径，共同祖先不需要复制到各分支。
func (s *Data) Path(head string) []*sessionentry.ContextEntry {
	entries := make([]*sessionentry.ContextEntry, 0)
	for head != "" {
		entry, found := s.Entry(head)
		if !found {
			break
		}
		entries = append(entries, entry)
		head = entry.ParentID
	}
	slices.Reverse(entries)
	return entries
}

func (s *Data) Safe(head string) bool {
	pending := make(map[sessionentry.Key]bool)
	for _, entry := range s.Path(head) {
		for _, block := range entry.Content {
			key := sessionentry.Key{RunID: identity.RunID(entry.RunID), CallID: block.CallID}
			switch block.Kind {
			case sessionentry.BlockToolCall:
				pending[key] = true
			case sessionentry.BlockToolResult:
				if !pending[key] {
					return false
				}
				delete(pending, key)
			}
		}
	}
	return len(pending) == 0
}

func (s *Data) RunIndex(id identity.RunID) int {
	return slices.IndexFunc(s.Runs, func(value agentrun.RunRecord) bool { return value.ID == id })
}

func (s *Data) SetRun(value agentrun.RunRecord) {
	if index := s.RunIndex(value.ID); index >= 0 {
		s.Runs[index] = value
	} else {
		s.Runs = append(s.Runs, value)
	}
}

func (s *Data) SetModelRequest(value agentrun.ModelRequestRecord) {
	index := slices.IndexFunc(s.ModelRequests, func(prior agentrun.ModelRequestRecord) bool { return prior.TurnID == value.TurnID })
	if index >= 0 {
		s.ModelRequests[index] = value
	} else {
		s.ModelRequests = append(s.ModelRequests, value)
	}
}

func (s *Data) SubagentEntry(agentID identity.AgentID, entryID string) (*sessionentry.ContextEntry, bool) {
	for _, value := range s.SubagentEntries[agentID] {
		if value.ID == entryID {
			return value, true
		}
	}
	return nil, false
}

func (s *Data) AcceptSubagentEntry(agentID identity.AgentID, entry sessionentry.ContextEntry) *sessionentry.ContextEntry {
	stored := new(entry)
	s.SubagentEntries[agentID] = append(s.SubagentEntries[agentID], stored)
	return stored
}
