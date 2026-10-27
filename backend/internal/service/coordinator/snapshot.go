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

// activeRun 只借用既有生命周期槽位，不建立另一套 Run 注册表。
func (s *Coordinator) activeRun(agentID identity.AgentID) *agentexecution.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	if active := s.executions[agentID]; active != nil {
		return active.run
	}
	return nil
}

func (s *Coordinator) ActiveExecution(agentID identity.AgentID) (agentexecution.ExecutionRecord, bool) {
	if run := s.activeRun(agentID); run != nil {
		return run.Snapshot(), true
	}
	return agentexecution.ExecutionRecord{}, false
}

// ListAgentEntries 按实例类型分派查询；会话历史由 Session 读取，子执行只暴露当前 Run 的私有消息。
func (s *Coordinator) ListAgentEntries(ctx context.Context, agentID identity.AgentID) ([]sessionentry.ContextEntry, error) {
	owner, err := s.sessions.GetAgent(agentID)
	if err != nil {
		return nil, err
	}
	if owner.IsMain() {
		page, err := s.sessions.ListEntries(ctx, owner.SessionID)
		if err != nil {
			return nil, err
		}
		return page.Entries, nil
	}
	if _, err := s.sessionRecords.Get(ctx, owner.SessionID); err != nil {
		return nil, err
	}
	if run := s.activeRun(agentID); run != nil {
		return run.Entries(), nil
	}
	return []sessionentry.ContextEntry{}, nil
}

func (s *Coordinator) ListAgentTimings(ctx context.Context, agentID string, limit int) ([]timing.View, error) {
	owner, err := s.sessions.GetAgent(identity.AgentID(agentID))
	if err != nil {
		return nil, err
	}
	if owner.IsMain() {
		return s.operations.ListAgentTimings(ctx, agentID, limit)
	}
	values := make([]timing.View, 0)
	if run := s.activeRun(owner.ID); run != nil {
		values = executionservice.RunTimings(run)
	}
	slices.SortFunc(values, func(a, b timing.View) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
	})
	if limit > 0 {
		values = values[:min(limit, len(values))]
	}
	return values, ctx.Err()
}
