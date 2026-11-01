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
func (s *Coordinator) ActiveRun(agentID identity.AgentID, branchID identity.BranchID) *agentexecution.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	if active := s.executions[executionKey{agentID, branchID}]; active != nil {
		return active.run
	}
	return nil
}

func (s *Coordinator) SessionRuns(sessionID identity.SessionID, branchID identity.BranchID) []*agentexecution.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	var runs []*agentexecution.Run
	for _, active := range s.executions {
		if active.sessionID == sessionID && active.branchID == branchID && active.run != nil {
			runs = append(runs, active.run)
		}
	}
	return runs
}

// ListAgentEntries 按执行归属分派；主会话历史无需创建 Agent 实例即可查询。
func (s *Coordinator) ListAgentEntries(ctx context.Context, agentID identity.AgentID, branchID identity.BranchID) ([]sessionentry.ContextEntry, error) {
	if run := s.ActiveRun(agentID, branchID); run != nil && !run.Main() {
		return run.Entries(), ctx.Err()
	}
	return s.sessions.ListEntries(ctx, identity.SessionID(agentID), branchID)
}

func (s *Coordinator) ListAgentTimings(ctx context.Context, agentID string, branchID identity.BranchID, limit int) ([]timing.View, error) {
	if run := s.ActiveRun(identity.AgentID(agentID), branchID); run != nil && !run.Main() {
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
	return s.operations.ListAgentTimings(ctx, agentID, branchID, limit)
}
