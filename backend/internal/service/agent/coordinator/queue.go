package coordinator

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/identity"
)

// StartNext 由 Loop 上层在结算并释放占用后调用，以 Session 为调度单位。
func (r *Coordinator) StartNext(ctx context.Context, sessionID identity.SessionID) (bool, error) {
	if err := r.enter(ctx); err != nil {
		return false, err
	}
	defer r.running.Done()
	execution, prepared, err := r.prepareNext(ctx, sessionID)
	if err != nil || !prepared {
		return false, err
	}
	if err := r.activate(ctx, execution); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Coordinator) prepareNext(ctx context.Context, sessionID identity.SessionID) (agentexecution.Execution, bool, error) {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	queue, ok := r.sessionTasks.TaskQueue(sessionID)
	if !ok {
		return agentexecution.Execution{}, false, nil
	}
	count, err := r.recovery.CountActiveBySession(ctx, sessionID)
	if err != nil || count != 0 {
		return agentexecution.Execution{}, false, err
	}
	input, ok := queue.Next()
	if !ok {
		return agentexecution.Execution{}, false, nil
	}
	// 准入失败必须回到原队首；会话删除时 Clear 会让延迟 Retry 失效。
	defer queue.Retry(input)
	// owner 只在领取后确定，创建和排队阶段不绑定 Agent。
	owner, err := r.primary.GetOrCreatePrimaryAgent(ctx, sessionID)
	if err != nil {
		return agentexecution.Execution{}, false, err
	}
	if r.activeTasks.Occupied(owner.ID) || !owner.State.Startable() {
		return agentexecution.Execution{}, false, nil
	}
	modelConfig, err := r.sessionModels.CurrentModelConfig(ctx, sessionID)
	if err != nil {
		return agentexecution.Execution{}, false, err
	}
	execution, _, err := r.operations.PrepareTask(ctx, input, owner.ID, modelConfig)
	if err != nil {
		return agentexecution.Execution{}, false, err
	}
	queue.Complete(input)
	return execution, true, nil
}
