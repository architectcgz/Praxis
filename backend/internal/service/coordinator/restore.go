package coordinator

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/errorcode"
)

// Restore 必须在外部准入开放前调用，只恢复已开始并持久化的 Execution。
// 未开始的输入只在内存中存在，不参与启动恢复。
func (s *Coordinator) Restore(ctx context.Context) (err error) {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	s.mu.Lock()
	if s.restored {
		s.mu.Unlock()
		return nil
	}
	if s.restoring || len(s.executions) != 0 {
		s.mu.Unlock()
		return errors.New("execution recovery requires stopped executors")
	}
	s.restoring = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.restoring = false
		if err == nil {
			s.restored = true
		}
		s.mu.Unlock()
	}()
	if err := s.toolRecords.RecoverAll(ctx); err != nil {
		return fmt.Errorf("reconcile tool results: %w", err)
	}
	executions, err := s.recovery.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("load active executions: %w", err)
	}
	for _, execution := range executions {
		s.mu.Lock()
		if s.executions[execution.AgentID] != nil {
			s.mu.Unlock()
			return fmt.Errorf("multiple active executions for agent %s: %w", execution.AgentID, errorcode.ErrRequestConflict)
		}
		active := newActiveExecution(ctx)
		s.executions[execution.AgentID] = active
		err := s.launchLocked(execution, active)
		if err != nil {
			delete(s.executions, execution.AgentID)
			close(active.ready)
			active.cancel(nil)
		}
		s.mu.Unlock()
		if err != nil {
			return fmt.Errorf("restore execution: %w", err)
		}
	}
	return nil
}
