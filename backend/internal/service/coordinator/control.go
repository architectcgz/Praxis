package coordinator

import (
	"context"
	"errors"
	"praxis/internal/identity"
)

func (s *Coordinator) cancelCurrentRun(ctx context.Context, agentID identity.AgentID, cause error) (*activeRun, bool, error) {
	s.mu.Lock()
	key, active := s.currentRunLocked(agentID)
	s.mu.Unlock()
	if active == nil {
		return nil, false, nil
	}
	// Run 可能刚保存、尚未登记身份；只等待本次准备完成，不等待执行退出。
	// 保留本次查找到的执行句柄，避免等待期间换成下一次执行后误中断。
	select {
	case <-active.ready:
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.runs[key] != active {
		return nil, false, nil
	}
	if active.ctx.Err() != nil {
		return active, errors.Is(context.Cause(active.ctx), cause), nil
	}
	if active.ending {
		return nil, false, nil
	}
	active.cancel(cause)
	return active, true, nil
}

func (s *Coordinator) currentRunLocked(agentID identity.AgentID) (runKey, *activeRun) {
	for key, active := range s.runs {
		if key.agentID == agentID {
			return key, active
		}
	}
	return runKey{}, nil
}

func (s *Coordinator) activeRunLocked(agentID identity.AgentID, id identity.RunID) *activeRun {
	for key, active := range s.runs {
		if key.agentID == agentID && active.runID == id {
			return active
		}
	}
	return nil
}
