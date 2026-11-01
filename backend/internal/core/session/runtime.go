package session

import (
	"praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"slices"
)

// Session 拥有完整会话数据；分支选择和输入队列属于页面绑定。
// 同一会话的读取和变更由接纳入口顺序执行；发布后的 Entry 及内部载荷不再修改。
type Session struct {
	ID           identity.SessionID
	Entries      []*sessionentry.ContextEntry
	Branches     []Branch
	RecentBranch identity.BranchID
	Executions   []execution.ExecutionRecord
	Usage        []providerapi.UsageRecord
	Timings      []timing.Record
	entryIndexes map[string]int
}

func Open(id identity.SessionID) *Session {
	return &Session{
		ID:           id,
		entryIndexes: make(map[string]int),
	}
}

func (s *Session) MainAgentID() identity.AgentID { return identity.AgentID(s.ID) }

func (s *Session) Entry(id string) (*sessionentry.ContextEntry, bool) {
	index, found := s.entryIndexes[id]
	if !found {
		return nil, false
	}
	return s.Entries[index], true
}

// AcceptEntry 只接收已经确认归属并成功提交的完整 Entry，不保存模型请求的精简副本。
func (s *Session) AcceptEntry(entry sessionentry.ContextEntry) *sessionentry.ContextEntry {
	stored := new(entry)
	s.Entries = append(s.Entries, stored)
	s.entryIndexes[entry.ID] = len(s.Entries) - 1
	branch := s.Branch(entry.BranchID)
	branch.HeadEntryID, branch.UpdatedAt = entry.ID, entry.CreatedAt
	s.RecentBranch = branch.ID
	return stored
}

func (s *Session) Branch(id identity.BranchID) *Branch {
	for index := range s.Branches {
		if s.Branches[index].ID == id {
			return &s.Branches[index]
		}
	}
	return nil
}

func (s *Session) BranchByRequest(requestID string) *Branch {
	for index := range s.Branches {
		if s.Branches[index].RequestID == requestID {
			return &s.Branches[index]
		}
	}
	return nil
}

func (s *Session) AcceptBranch(branch Branch) {
	s.Branches = append(s.Branches, branch)
	s.RecentBranch = branch.ID
}

// Path 按父节点回溯当前路径，共同祖先不需要复制到各分支。
func (s *Session) Path(head string) []*sessionentry.ContextEntry {
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

func (s *Session) Safe(head string) bool {
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

func (s *Session) ExecutionIndex(id identity.AgentExecutionID) int {
	return slices.IndexFunc(s.Executions, func(value execution.ExecutionRecord) bool { return value.ID == id })
}

func (s *Session) SetExecution(value execution.ExecutionRecord) {
	if index := s.ExecutionIndex(value.ID); index >= 0 {
		s.Executions[index] = value
	} else {
		s.Executions = append(s.Executions, value)
	}
}

func (s *Session) SetUsage(value providerapi.UsageRecord) {
	index := slices.IndexFunc(s.Usage, func(prior providerapi.UsageRecord) bool { return prior.Key() == value.Key() })
	if index >= 0 {
		s.Usage[index] = value
	} else {
		s.Usage = append(s.Usage, value)
	}
}

func (s *Session) SetTiming(value timing.Record) {
	index := slices.IndexFunc(s.Timings, func(prior timing.Record) bool { return prior.ExecutionID == value.ExecutionID && prior.ID == value.ID })
	if index >= 0 {
		s.Timings[index] = value
	} else {
		s.Timings = append(s.Timings, value)
	}
}
