package execution

import (
	"context"
	"praxis/internal/contracts"
)

// StartNext 按 Agent 查询队首；活动任务（包括等待结算）占用期间不领取。
// 并发准备由 TaskOperations 的准入事务仲裁，不持有进程级调度锁。
func (r *Coordinator) StartNext(ctx context.Context, agentID contracts.AgentID) (bool, error) {
	if err := r.enter(ctx); err != nil {
		return false, err
	}
	defer r.running.Done()
	if r.activeTasks.occupied(agentID) {
		return false, nil
	}
	pending, found, err := r.queue.Next(ctx, agentID)
	if err != nil || !found {
		return false, err
	}
	task, prepared, err := r.tasks.PrepareQueuedTask(ctx, pending)
	if err != nil || !prepared {
		return false, err
	}
	if task.AgentID != agentID {
		return false, contracts.ErrRequestConflict
	}
	if err := r.activate(ctx, task); err != nil {
		return false, err
	}
	return true, nil
}
