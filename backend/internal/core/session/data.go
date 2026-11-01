package session

import (
	"praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"slices"
)

// Data 拥有完整会话数据；分支选择和输入队列属于页面绑定。
// 同一会话的数据由服务层读写锁保护；发布后的 Entry 及内部载荷不再修改。
type Data struct {
	ID           identity.SessionID
	Entries      []*sessionentry.ContextEntry
	Branches     []Branch
	RecentBranch identity.BranchID
	Executions   []execution.ExecutionRecord
	Usage        []providerapi.UsageRecord
	Timings      []timing.Record
	entryIndexes map[string]int
}

func Open(id identity.SessionID) *Data {
	return &Data{
		ID:           id,
		entryIndexes: make(map[string]int),
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

// AcceptEntry 只接收已经确认归属并成功提交的完整 Entry，不保存模型请求的精简副本。
func (s *Data) AcceptEntry(entry sessionentry.ContextEntry) *sessionentry.ContextEntry {
	stored := new(entry)
	s.Entries = append(s.Entries, stored)
	s.entryIndexes[entry.ID] = len(s.Entries) - 1
	branch := s.Branch(entry.BranchID)
	branch.HeadEntryID, branch.UpdatedAt = entry.ID, entry.CreatedAt
	s.RecentBranch = branch.ID
	return stored
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
			key := sessionentry.Key{ExecutionID: identity.AgentExecutionID(entry.ExecutionID), CallID: block.CallID}
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

func (s *Data) ExecutionIndex(id identity.AgentExecutionID) int {
	return slices.IndexFunc(s.Executions, func(value execution.ExecutionRecord) bool { return value.ID == id })
}

func (s *Data) SetExecution(value execution.ExecutionRecord) {
	if index := s.ExecutionIndex(value.ID); index >= 0 {
		s.Executions[index] = value
	} else {
		s.Executions = append(s.Executions, value)
	}
}

func (s *Data) SetUsage(value providerapi.UsageRecord) {
	index := slices.IndexFunc(s.Usage, func(prior providerapi.UsageRecord) bool { return prior.Key() == value.Key() })
	if index >= 0 {
		s.Usage[index] = value
	} else {
		s.Usage = append(s.Usage, value)
	}
}

func (s *Data) SetTiming(value timing.Record) {
	index := slices.IndexFunc(s.Timings, func(prior timing.Record) bool { return prior.ExecutionID == value.ExecutionID && prior.ID == value.ID })
	if index >= 0 {
		s.Timings[index] = value
	} else {
		s.Timings = append(s.Timings, value)
	}
}
