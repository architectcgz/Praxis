package coordinator

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
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

// ListAgentEntries 查询所属持久化历史，运行实例释放后仍能读取子代理记录。
func (s *Coordinator) ListAgentEntries(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, branchID identity.BranchID) ([]sessionentry.ContextEntry, error) {
	return s.sessions.ListAgentEntries(ctx, sessionID, agentID, branchID)
}

func (s *Coordinator) ListAgentTimings(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, branchID identity.BranchID, limit int) ([]timing.View, error) {
	return s.operations.ListAgentTimings(ctx, sessionID, agentID, branchID, limit)
}
