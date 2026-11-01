package session

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
)

func (s *Session) CreateSubagent(ctx context.Context, record agentexecution.ExecutionRecord, input sessionentry.ContextEntry) (*sessionentry.ContextEntry, error) {
	s.mu.Lock()
	if _, exists := s.data.SubagentEntries[record.AgentID]; !exists {
		if err := s.log.CreateSubagent(ctx, s.data.ID, record.AgentID); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		s.data.SubagentEntries[record.AgentID] = nil
	}
	s.mu.Unlock()
	stored, _, err := s.AppendSubagentEntry(ctx, record.AgentID, record.BranchID, input)
	if err != nil {
		return stored, err
	}
	return stored, s.SaveExecution(ctx, record)
}

// AppendSubagentEntry 只连接子历史中的父节点，不推进主会话分支 Head。
func (s *Session) AppendSubagentEntry(ctx context.Context, agentID identity.AgentID, branchID identity.BranchID, entry sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Branch(branchID) == nil {
		return nil, false, errorcode.ErrNotFound
	}
	if prior, found := s.data.SubagentEntry(agentID, entry.ID); found {
		entry.ParentID, entry.CreatedAt = prior.ParentID, prior.CreatedAt
		if !reflect.DeepEqual(*prior, entry) {
			return nil, false, errorcode.ErrRequestConflict
		}
		return prior, false, nil
	}
	entries := s.data.SubagentEntries[agentID]
	for index := len(entries) - 1; index >= 0; index-- {
		if entries[index].ExecutionID == entry.ExecutionID {
			entry.ParentID = entries[index].ID
			break
		}
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = s.clock.Now()
	}
	stored := s.data.AcceptSubagentEntry(agentID, entry)
	err := s.log.AppendEvent(ctx, s.data.ID, Event{
		Type:      EventEntryAppended,
		Key:       entry.ID,
		AgentID:   agentID,
		BranchID:  branchID,
		CreatedAt: s.clock.Now(),
		Payload:   entry,
	})
	return stored, true, err
}

func (s *Session) AgentEntries(ctx context.Context, agentID identity.AgentID, branchID identity.BranchID) ([]*sessionentry.ContextEntry, error) {
	if agentID == s.data.MainAgentID() {
		return s.BranchEntries(ctx, branchID)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make([]*sessionentry.ContextEntry, 0)
	for _, entry := range s.data.SubagentEntries[agentID] {
		index := s.data.ExecutionIndex(identity.AgentExecutionID(entry.ExecutionID))
		if index >= 0 && s.data.Executions[index].BranchID == branchID {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}
