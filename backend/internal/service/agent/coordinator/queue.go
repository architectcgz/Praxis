package coordinator

import (
	"context"
	"praxis/internal/fault"
	"praxis/internal/identity"
)

// StartNext 按 Agent 查询队首；活动任务（包括等待结算）占用期间不领取。
// 并发准备由 Execution 用例的准入事务仲裁，不持有进程级调度锁。
func (r *Coordinator) StartNext(ctx context.Context, agentID identity.AgentID) (bool, error) {
	if err := r.enter(ctx); err != nil {
		return false, err
	}
	defer r.running.Done()
	if r.activeTasks.occupied(agentID) {
		return false, nil
	}
	task, prepared, err := r.operations.PrepareNext(ctx, agentID)
	if err != nil || !prepared {
		return false, err
	}
	if task.AgentID != agentID {
		return false, fault.ErrRequestConflict
	}
	if err := r.activate(ctx, task); err != nil {
		return false, err
	}
	return true, nil
}
