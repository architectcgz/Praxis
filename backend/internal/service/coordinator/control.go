package coordinator

import (
	"context"
	"errors"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// RunControl 定位用户选择的执行，不以命令到达时的当前执行替换目标。
type RunControl struct {
	SessionID identity.SessionID
	BranchID  identity.BranchID
	AgentID   identity.AgentID
	RunID     identity.RunID
}

func (s *Coordinator) cancelRun(ctx context.Context, request RunControl, cause error) (*activeRun, bool, error) {
	key := runKey{request.AgentID, request.BranchID}
	s.mu.Lock()
	active := s.runs[key]
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
	if s.closed || s.runs[key] != active {
		s.mu.Unlock()
		return nil, false, nil
	}
	if active.sessionID != request.SessionID || active.runID != request.RunID {
		s.mu.Unlock()
		return nil, false, errorcode.ErrRequestConflict
	}
	ending := active.ending
	s.mu.Unlock()
	if active.ctx.Err() != nil {
		return active, errors.Is(context.Cause(active.ctx), cause), nil
	}
	if ending {
		return nil, false, nil
	}
	active.cancel(cause)
	return active, true, nil
}

func (s *Coordinator) activeRunLocked(agentID identity.AgentID, id identity.RunID) *activeRun {
	for key, active := range s.runs {
		if key.agentID == agentID && active.runID == id {
			return active
		}
	}
	return nil
}
