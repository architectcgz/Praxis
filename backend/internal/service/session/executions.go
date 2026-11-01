package session

import (
	"cmp"
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
)

var _ sessionmodel.ExecutionRecords = (*Service)(nil)

func (s *Service) GetExecution(ctx context.Context, sessionID identity.SessionID, id identity.AgentExecutionID) (record agentexecution.ExecutionRecord, err error) {
	owner, err := s.openedSession(ctx, sessionID)
	if err != nil {
		return record, err
	}
	err = owner.use(ctx, func() error {
		index := owner.model.ExecutionIndex(id)
		if index < 0 {
			return errorcode.ErrNotFound
		}
		record = cloneExecution(owner.model.Executions[index])
		return nil
	})
	return
}

func (s *Service) SaveExecution(ctx context.Context, value agentexecution.ExecutionRecord) error {
	owner, err := s.openedSession(ctx, value.SessionID)
	if err != nil {
		return err
	}
	return owner.use(ctx, func() error { return owner.saveExecution(ctx, value) })
}

func (s *Service) FindExecutionByRequest(ctx context.Context, agentID identity.AgentID, request identity.RequestID) (record agentexecution.ExecutionRecord, err error) {
	owner, err := s.openedSession(ctx, identity.SessionID(agentID))
	if err != nil {
		return record, err
	}
	err = owner.use(ctx, func() error {
		for _, value := range owner.model.Executions {
			if value.AgentID == agentID && value.RequestID == request {
				record = cloneExecution(value)
				return nil
			}
		}
		return errorcode.ErrNotFound
	})
	return
}

func (s *Service) ListAgentExecutions(ctx context.Context, agentID identity.AgentID, branchID identity.BranchID, limit int) (values []agentexecution.ExecutionRecord, err error) {
	owner, err := s.openedSession(ctx, identity.SessionID(agentID))
	if err != nil {
		return nil, err
	}
	err = owner.use(ctx, func() error {
		values = make([]agentexecution.ExecutionRecord, 0)
		for _, value := range owner.model.Executions {
			if value.AgentID == agentID && value.BranchID == branchID {
				values = append(values, cloneExecution(value))
			}
		}
		values = capped(sorted(values, func(a, b agentexecution.ExecutionRecord) int { return -executionOrder(a, b) }), limit)
		return nil
	})
	return
}

func (s *Session) saveExecution(ctx context.Context, value agentexecution.ExecutionRecord) error {
	if err := validateExecutionOwnership(s.model, value); err != nil {
		return err
	}
	value = cloneExecution(value)
	// 展示字段从完整 Entry 派生，不持久化重复数据。
	value.Progress.OutputBytes, value.Progress.ToolCalls = 0, 0
	value.Progress.FinalAssistantID, value.Progress.FinalStopReason = "", ""
	if err := s.appendEvent(ctx, "agent_execution.saved", "agent_execution", value.ID.String(), value); err != nil {
		return err
	}
	s.model.SetExecution(value)
	return nil
}

func validateExecutionOwnership(owner *sessionmodel.Session, value agentexecution.ExecutionRecord) error {
	if value.SessionID != owner.ID || value.AgentID != owner.MainAgentID() || owner.Branch(value.BranchID) == nil {
		return errors.New("execution ownership does not match its session or branch")
	}
	input, found := owner.Entry(value.InputEntryID)
	if !found {
		return errors.New("execution input is missing")
	}
	if input.BranchID != value.BranchID || input.ExecutionID != value.ID.String() || input.RequestID != value.RequestID.String() || input.SourceTaskID != value.SourceTaskID.String() {
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
