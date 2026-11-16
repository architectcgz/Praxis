package session

import (
	"cmp"
	"context"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"slices"
)

func (s *Session) GetRun(ctx context.Context, id identity.RunID) (sessionentry.RunRecord, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	return s.run(ctx, id)
}

func (s *Session) run(ctx context.Context, id identity.RunID) (sessionentry.RunRecord, error) {
	if err := s.syncHistory(ctx); err != nil {
		return sessionentry.RunRecord{}, err
	}
	s.mu.RLock()
	pending, found := s.data.RunStates[id]
	s.mu.RUnlock()
	if found {
		return pending, nil
	}
	return s.log.Run(ctx, id)
}

func (s *Session) FindRunByTask(ctx context.Context, agentID identity.AgentID, taskID identity.TaskID) (sessionentry.RunRecord, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.RLock()
	var id identity.RunID
	for _, value := range s.data.Runs {
		if value.AgentID == agentID && value.SourceTaskID == taskID {
			id = value.ID
			break
		}
	}
	s.mu.RUnlock()
	if id == "" {
		return sessionentry.RunRecord{}, errorcode.ErrNotFound
	}
	return s.run(ctx, id)
}

func runOrder(a, b sessionentry.HistorySummary) int {
	return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
}

func (s *Session) ListSessionRuns(ctx context.Context) ([]sessionentry.HistorySummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	values := slices.Clone(s.data.Runs)
	s.mu.RUnlock()
	return sorted(values, func(a, b sessionentry.HistorySummary) int { return -runOrder(a, b) }), nil
}
