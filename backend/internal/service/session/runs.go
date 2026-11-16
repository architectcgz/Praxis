package session

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/identity"
)

func (s *Service) GetRun(ctx context.Context, sessionID identity.SessionID, id identity.RunID) (record sessionentry.RunRecord, err error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return record, err
	}
	return owner.GetRun(ctx, id)
}

func (s *Service) FindRunByTask(ctx context.Context, agentID identity.AgentID, taskID identity.TaskID) (record sessionentry.RunRecord, err error) {
	owner, err := s.OpenedSession(ctx, identity.SessionID(agentID))
	if err != nil {
		return record, err
	}
	return owner.FindRunByTask(ctx, agentID, taskID)
}

func (s *Service) ListSessionRuns(ctx context.Context, sessionID identity.SessionID) ([]sessionentry.HistorySummary, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.ListSessionRuns(ctx)
}
