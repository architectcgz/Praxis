package coordinator

import (
	"context"
	"errors"
	"praxis/internal/identity"
)

func (s *Coordinator) cancelExecution(ctx context.Context, agentID identity.AgentID, executionID identity.AgentExecutionID, cause error) (bool, error) {
	s.mu.Lock()
	active := s.activeExecutionLocked(agentID, executionID)
	s.mu.Unlock()
	if active == nil {
		return false, nil
	}
	// Execution 可能刚保存、尚未登记身份；只等待本次准备完成，不等待执行退出。
	// 保留原句柄身份，避免等待期间换成下一次执行后误取消。
	select {
	case <-active.ready:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.activeExecutionLocked(agentID, executionID) != active {
		return false, nil
	}
	if active.ctx.Err() != nil {
		return errors.Is(context.Cause(active.ctx), cause), nil
	}
	if active.ending {
		return false, nil
	}
	active.cancel(cause)
	return true, nil
}

func (s *Coordinator) activeExecutionLocked(agentID identity.AgentID, id identity.AgentExecutionID) *activeExecution {
	for key, active := range s.executions {
		if key.agentID == agentID && active.executionID == id {
			return active
		}
	}
	return nil
}
