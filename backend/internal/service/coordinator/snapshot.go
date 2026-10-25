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

// activeRuntime 只借用既有生命周期槽位，不建立另一套 Runtime 注册表。
func (s *Coordinator) activeRuntime(agentID identity.AgentID) *agentexecution.Runtime {
	s.mu.Lock()
	defer s.mu.Unlock()
	if active := s.executions[agentID]; active != nil {
		return active.runtime
	}
	return nil
}

func (s *Coordinator) ActiveExecution(agentID identity.AgentID) (agentexecution.Execution, bool) {
	if runtime := s.activeRuntime(agentID); runtime != nil {
		return runtime.Snapshot(), true
	}
	return agentexecution.Execution{}, false
}

func (s *Coordinator) ActiveEntries(agentID identity.AgentID) []sessionentry.ContextEntry {
	if runtime := s.activeRuntime(agentID); runtime != nil {
		return runtime.Entries()
	}
	return []sessionentry.ContextEntry{}
}

func (s *Coordinator) ListAgentTimings(ctx context.Context, agentID string, limit int) ([]timing.View, error) {
	owner, err := s.agent(identity.AgentID(agentID))
	if err != nil {
		return nil, err
	}
	if owner.IsMain() {
		return s.operations.ListAgentTimings(ctx, agentID, limit)
	}
	values := make([]timing.View, 0)
	if runtime := s.activeRuntime(owner.ID); runtime != nil {
		values = executionservice.RuntimeTimings(runtime)
	}
	slices.SortFunc(values, func(a, b timing.View) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
	})
	if limit > 0 {
		values = values[:min(limit, len(values))]
	}
	return values, ctx.Err()
}
