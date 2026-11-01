package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	agentexecution "praxis/internal/core/agent/execution"
	sessionentry "praxis/internal/core/session/entry"
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
		AgentID:   value.AgentID,
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

// ValidateExecutions 在完整日志加载后确认主、子执行的跨文件归属。
func (s *Data) ValidateExecutions() error {
	for _, record := range s.Executions {
		if err := validateExecutionOwnership(s, record); err != nil {
			return fmt.Errorf("restore execution %s: %w", record.ID, err)
		}
	}
	return nil
}

func validateExecutionOwnership(owner *Data, value agentexecution.ExecutionRecord) error {
	if value.SessionID != owner.ID || owner.Branch(value.BranchID) == nil {
		return errors.New("execution ownership does not match its session")
	}
	var input *sessionentry.ContextEntry
	var found bool
	if value.AgentID == owner.MainAgentID() {
		input, found = owner.Entry(value.InputEntryID)
		if found && !owner.hasAncestor(owner.Branch(value.BranchID).HeadEntryID, input.ID) {
			return errors.New("execution input is outside its branch")
		}
	} else {
		input, found = owner.SubagentEntry(value.AgentID, value.InputEntryID)
		parent := owner.ExecutionIndex(value.ParentExecutionID)
		if parent < 0 || owner.Executions[parent].BranchID != value.BranchID || value.ParentCallID == "" {
			return errors.New("subagent execution parent ownership mismatch")
		}
	}
	if !found {
		return errors.New("execution input is missing")
	}
	if input.ExecutionID != value.ID.String() || input.RequestID != value.RequestID.String() || input.SourceTaskID != value.SourceTaskID.String() {
		return errors.New("execution input ownership mismatch")
	}
	if index := owner.ExecutionIndex(value.ID); index >= 0 {
		prior := owner.Executions[index]
		if prior.BranchID != value.BranchID || prior.AgentID != value.AgentID || prior.AgentName != value.AgentName || prior.ParentExecutionID != value.ParentExecutionID || prior.ParentCallID != value.ParentCallID || prior.RequestID != value.RequestID || prior.InputEntryID != value.InputEntryID || prior.SourceTaskID != value.SourceTaskID || !prior.CreatedAt.Equal(value.CreatedAt) || !reflect.DeepEqual(prior.Config, value.Config) {
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

func (s *Session) SaveModelRequest(ctx context.Context, value agentexecution.ExecutionRecord, request agentexecution.ModelRequestRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value = cloneExecution(value)
	request = request.Clone()
	s.data.SetExecution(value)
	s.data.SetModelRequest(request)
	return s.log.AppendEvent(ctx, s.data.ID, Event{
		Type:      EventModelRequestFinished,
		Key:       request.TurnID.String(),
		AgentID:   value.AgentID,
		CreatedAt: s.clock.Now(),
		Payload: ModelRequestFinished{
			Request:   request,
			Execution: value,
		},
	})
}

func (s *Session) ListSessionExecutions(branchID identity.BranchID) []agentexecution.ExecutionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]agentexecution.ExecutionRecord, 0)
	for _, value := range s.data.Executions {
		if value.BranchID == branchID {
			values = append(values, cloneExecution(value))
		}
	}
	return sorted(values, func(a, b agentexecution.ExecutionRecord) int { return -executionOrder(a, b) })
}
