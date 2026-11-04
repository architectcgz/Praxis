package session

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

var _ sessionmodel.RunRecords = (*Service)(nil)

func (s *Service) GetRun(ctx context.Context, sessionID identity.SessionID, id identity.RunID) (record agentrun.RunRecord, err error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return record, err
	}
	return owner.GetRun(id)
}

func (s *Service) SaveRun(ctx context.Context, value agentrun.RunRecord) error {
	owner, err := s.OpenedSession(ctx, value.SessionID)
	if err != nil {
		return err
	}
	return owner.SaveRun(ctx, value)
}

func (s *Service) FindRunByRequest(ctx context.Context, agentID identity.AgentID, request identity.RequestID) (record agentrun.RunRecord, err error) {
	owner, err := s.OpenedSession(ctx, identity.SessionID(agentID))
	if err != nil {
		return record, err
	}
	return owner.FindRunByRequest(agentID, request)
}

func (s *Service) ListAgentRuns(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, branchID identity.BranchID, limit int) ([]agentrun.RunRecord, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.ListAgentRuns(agentID, branchID, limit), nil
}

func (s *Service) SaveModelRequest(ctx context.Context, value agentrun.RunRecord, request agentrun.ModelRequestRecord) error {
	owner, err := s.OpenedSession(ctx, value.SessionID)
	if err != nil {
		return err
	}
	return owner.SaveModelRequest(ctx, value, request)
}

func (s *Service) CreateSubagent(ctx context.Context, record agentrun.RunRecord, input sessionentry.ContextEntry) (*sessionentry.ContextEntry, error) {
	owner, err := s.OpenedSession(ctx, record.SessionID)
	if err != nil {
		return nil, err
	}
	return owner.CreateSubagent(ctx, record, input)
}

func (s *Service) ListSessionRuns(ctx context.Context, sessionID identity.SessionID, branchID identity.BranchID) ([]agentrun.RunRecord, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.ListSessionRuns(branchID), nil
}
