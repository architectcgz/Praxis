package session

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

var _ sessionmodel.ExecutionRecords = (*Service)(nil)

func (s *Service) GetExecution(ctx context.Context, sessionID identity.SessionID, id identity.AgentExecutionID) (record agentexecution.ExecutionRecord, err error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return record, err
	}
	return owner.GetExecution(id)
}

func (s *Service) SaveExecution(ctx context.Context, value agentexecution.ExecutionRecord) error {
	owner, err := s.OpenedSession(ctx, value.SessionID)
	if err != nil {
		return err
	}
	return owner.SaveExecution(ctx, value)
}

func (s *Service) FindExecutionByRequest(ctx context.Context, agentID identity.AgentID, request identity.RequestID) (record agentexecution.ExecutionRecord, err error) {
	owner, err := s.OpenedSession(ctx, identity.SessionID(agentID))
	if err != nil {
		return record, err
	}
	return owner.FindExecutionByRequest(agentID, request)
}

func (s *Service) ListAgentExecutions(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, branchID identity.BranchID, limit int) ([]agentexecution.ExecutionRecord, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.ListAgentExecutions(agentID, branchID, limit), nil
}

func (s *Service) SaveModelRequest(ctx context.Context, value agentexecution.ExecutionRecord, request agentexecution.ModelRequestRecord) error {
	owner, err := s.OpenedSession(ctx, value.SessionID)
	if err != nil {
		return err
	}
	return owner.SaveModelRequest(ctx, value, request)
}

func (s *Service) CreateSubagent(ctx context.Context, record agentexecution.ExecutionRecord, input sessionentry.ContextEntry) (*sessionentry.ContextEntry, error) {
	owner, err := s.OpenedSession(ctx, record.SessionID)
	if err != nil {
		return nil, err
	}
	return owner.CreateSubagent(ctx, record, input)
}

func (s *Service) ListSessionExecutions(ctx context.Context, sessionID identity.SessionID, branchID identity.BranchID) ([]agentexecution.ExecutionRecord, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.ListSessionExecutions(branchID), nil
}
