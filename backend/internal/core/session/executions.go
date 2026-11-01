package session

import (
	"cmp"
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
)

func (s *Session) GetExecution(id identity.AgentExecutionID) (agentexecution.ExecutionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index := s.data.ExecutionIndex(id)
	if index < 0 {
		return agentexecution.ExecutionRecord{}, errorcode.ErrNotFound
	}
	return cloneExecution(s.data.Executions[index]), nil
}

func (s *Session) SaveExecution(ctx context.Context, value agentexecution.ExecutionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateExecutionOwnership(s.data, value); err != nil {
		return err
	}
	value = cloneExecution(value)
	// 展示字段从完整 Entry 派生，不持久化重复数据。
	value.Progress.OutputBytes, value.Progress.ToolCalls = 0, 0
	value.Progress.FinalAssistantID, value.Progress.FinalStopReason = "", ""
	s.data.SetExecution(value)
	return s.log.AppendEvent(ctx, s.data.ID, Event{
		Type:      EventExecutionSaved,
		Key:       value.ID.String(),
		CreatedAt: s.clock.Now(),
		Payload:   value,
	})
}

func (s *Session) FindExecutionByRequest(agentID identity.AgentID, request identity.RequestID) (agentexecution.ExecutionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, value := range s.data.Executions {
		if value.AgentID == agentID && value.RequestID == request {
			return cloneExecution(value), nil
		}
	}
	return agentexecution.ExecutionRecord{}, errorcode.ErrNotFound
}

func (s *Session) ListAgentExecutions(agentID identity.AgentID, branchID identity.BranchID, limit int) []agentexecution.ExecutionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]agentexecution.ExecutionRecord, 0)
	for _, value := range s.data.Executions {
		if value.AgentID == agentID && value.BranchID == branchID {
			values = append(values, cloneExecution(value))
		}
	}
	return capped(sorted(values, func(a, b agentexecution.ExecutionRecord) int { return -executionOrder(a, b) }), limit)
}

func validateExecutionOwnership(owner *Data, value agentexecution.ExecutionRecord) error {
	if value.SessionID != owner.ID || value.AgentID != owner.MainAgentID() || owner.Branch(value.BranchID) == nil {
		return errors.New("execution ownership does not match its session")
	}
	input, found := owner.Entry(value.InputEntryID)
	if !found {
		return errors.New("execution input is missing")
	}
	if !owner.hasAncestor(owner.Branch(value.BranchID).HeadEntryID, input.ID) || input.ExecutionID != value.ID.String() || input.RequestID != value.RequestID.String() || input.SourceTaskID != value.SourceTaskID.String() {
		return errors.New("execution input ownership mismatch")
	}
	if index := owner.ExecutionIndex(value.ID); index >= 0 {
		prior := owner.Executions[index]
		if prior.BranchID != value.BranchID || prior.AgentID != value.AgentID || prior.RequestID != value.RequestID || prior.InputEntryID != value.InputEntryID || prior.SourceTaskID != value.SourceTaskID || !prior.CreatedAt.Equal(value.CreatedAt) || !reflect.DeepEqual(prior.Config, value.Config) {
			return errors.New("execution identity or frozen configuration changed")
		}
	}
	return nil
}

func cloneExecution(value agentexecution.ExecutionRecord) agentexecution.ExecutionRecord {
	value.Config = value.Config.Clone()
	return value
}

func executionOrder(a, b agentexecution.ExecutionRecord) int {
	return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
}
