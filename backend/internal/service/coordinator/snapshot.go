package coordinator

import (
	"cmp"
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	executionservice "praxis/internal/service/agent/execution"
	"slices"
)

// ActiveRun 只借用执行生命周期内的引用，不另建 Agent 注册表。
func (s *Coordinator) ActiveRun(agentID identity.AgentID) *agentexecution.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	if active := s.executions[agentID]; active != nil {
		return active.run
	}
	return nil
}

func (s *Coordinator) SessionRuns(sessionID identity.SessionID) []*agentexecution.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	var runs []*agentexecution.Run
	for _, active := range s.executions {
		if active.sessionID == sessionID && active.run != nil {
			runs = append(runs, active.run)
		}
	}
	return runs
}

// ListAgentEntries 按执行归属分派；主会话历史无需创建 Agent 实例即可查询。
func (s *Coordinator) ListAgentEntries(ctx context.Context, agentID identity.AgentID) ([]sessionentry.ContextEntry, error) {
	if run := s.ActiveRun(agentID); run != nil && !run.Main() {
		return run.Entries(), ctx.Err()
	}
	page, err := s.sessions.ListEntries(ctx, identity.SessionID(agentID))
	if err != nil {
		return nil, err
	}
	return page.Entries, nil
}

func (s *Coordinator) ListAgentTimings(ctx context.Context, agentID string, limit int) ([]timing.View, error) {
	if run := s.ActiveRun(identity.AgentID(agentID)); run != nil && !run.Main() {
		values := executionservice.RunTimings(run)
		slices.SortFunc(values, func(a, b timing.View) int {
			return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
		})
		if limit > 0 {
			values = values[:min(limit, len(values))]
		}
		return values, ctx.Err()
	}
	if _, err := s.sessionRecords.Get(ctx, identity.SessionID(agentID)); err != nil {
		return nil, err
	}
	return s.operations.ListAgentTimings(ctx, agentID, limit)
}
