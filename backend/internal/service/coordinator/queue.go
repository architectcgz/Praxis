package coordinator

import (
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

// scheduleSession 是入队、模型选择与结算后的统一调度决策；调用方已计入关闭等待。
func (r *Coordinator) scheduleSession(ctx context.Context, sessionID identity.SessionID, explicit bool) error {
	queue, ok := r.sessionTasks.TaskQueue(sessionID)
	if !ok {
		return nil
	}
	owner, err := r.mainAgent(ctx, sessionID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errCoordinatorClosed
	}
	if active := r.executions[owner.ID]; active != nil {
		// 暂停后显式提交输入或选择模型可重新唤醒队列，但必须等旧执行完全退出。
		cause := context.Cause(active.ctx)
		if explicit && (errors.Is(cause, errExecutionPaused) || errors.Is(cause, errExecutionStopped)) {
			active.resumeRequested = true
		}
		r.mu.Unlock()
		return nil
	}
	// 与忙闲转换一起领取，避免空队列检查和后续入队之间丢失唤醒。
	input, ok := queue.Next()
	if !ok {
		r.mu.Unlock()
		return nil
	}
	active := newActiveExecution(ctx)
	r.executions[owner.ID] = active
	r.mu.Unlock()
	return r.startNext(ctx, input, owner.ID, queue, active)
}

// startNext 的前置条件是调度器已在锁内领取队首并标记忙碌，不再判断或抢占运行权。
func (r *Coordinator) startNext(ctx context.Context, input sessionmodel.Task, ownerID identity.AgentID, queue *sessionmodel.TaskQueue, active *activeExecution) (err error) {
	// 准备阶段遵守调用方的超时，也响应进程关闭；运行阶段不继承请求的取消。
	prepareCtx, cancelPrepare := context.WithCancel(ctx)
	stopCancellation := context.AfterFunc(active.ctx, cancelPrepare)
	defer func() {
		stopCancellation()
		cancelPrepare()
	}()
	defer func() {
		if err == nil {
			return
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		// 准备失败才归还队首；Execution 已保存时 Complete 已使 Retry 失效。
		queue.Retry(input)
		delete(r.executions, ownerID)
		close(active.ready)
		active.cancel(nil)
	}()
	execution, err := r.prepareNext(prepareCtx, input, ownerID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	queue.Complete(input)
	return r.launchLocked(execution, active)
}

// prepareNext 只负责将已领取的队首准备为 Execution；忙闲状态归调用方所有。
func (r *Coordinator) prepareNext(ctx context.Context, input sessionmodel.Task, ownerID identity.AgentID) (*agentexecution.Runtime, error) {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	modelConfig, err := r.sessionModels.CurrentModelConfig(ctx, input.SessionID)
	if err != nil {
		return nil, err
	}
	execution, _, err := r.operations.PrepareTask(ctx, input, ownerID, modelConfig)
	return execution, err
}
