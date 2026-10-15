package execution

import (
	"context"
	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
)

// RecoverySource 从当前持久化状态读取活动 Task 和允许派发的 Agent。
type RecoverySource interface {
	ListActive(context.Context) ([]taskmodel.Task, error)
	ListDispatchableAgents(context.Context) ([]contracts.AgentID, error)
}

// Restore 先恢复已有 Task，再检查空闲 Agent 的 Pending 队列；不改写历史数据。
func (r *Coordinator) Restore(ctx context.Context, source RecoverySource) error {
	if err := r.enter(ctx); err != nil {
		return err
	}
	defer r.running.Done()
	tasks, err := source.ListActive(ctx)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := r.Activate(ctx, task); err != nil {
			return err
		}
	}
	agents, err := source.ListDispatchableAgents(ctx)
	if err != nil {
		return err
	}
	for _, agentID := range agents {
		if _, err := r.StartNext(ctx, agentID); err != nil {
			return err
		}
	}
	return nil
}
