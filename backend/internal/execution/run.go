package execution

import (
	"context"
	"errors"
	"praxis/internal/hook"
	"praxis/internal/identity"
	taskmodel "praxis/internal/task"
	"praxis/internal/toolcall"
)

func (r *Coordinator) run(
	ctx context.Context,
	task taskmodel.Task,
	active *activeTask,
) {
	r.logEvent(task.ID, "task started")
	defer r.activeTasks.runReturned(task.AgentID, active)
	if active.result != nil {
		r.endTask(task, active, active.result.outcome, active.result.code, active.result.message)
		return
	}

	if err := r.startTask(task); err != nil {
		outcome, code, message := r.executionResult(ctx, taskmodel.TaskFailed, taskmodel.TaskFailureRuntimeFailed, err)
		r.endTask(task, active, outcome, code, message)
		return
	}
	if ctx.Err() != nil {
		outcome, code, message := r.executionResult(ctx, taskmodel.TaskInterrupted, taskmodel.TaskFailureRuntimeCancelled, ctx.Err())
		r.endTask(task, active, outcome, code, message)
		return
	}

	messageWriter, err := r.messages.Writer(ctx, task.SessionID, task.AgentID)
	if err != nil || messageWriter == nil {
		if err == nil {
			err = errors.New("message streams returned a nil writer")
		}
		r.logError(task, "resolve message writer", err)
		outcome, code, message := r.executionResult(ctx, taskmodel.TaskFailed, taskmodel.TaskFailureRuntimeFailed, err)
		r.endTask(task, active, outcome, code, message)
		return
	}

	for {
		outcome, failureCode, failureMessage := r.execute(ctx, task, messageWriter)
		if outcome == taskmodel.TaskAwaitingApproval && ctx.Err() == nil {
			select {
			case <-active.wake:
				continue
			case <-ctx.Done():
				outcome, failureCode, failureMessage = r.executionResult(ctx, taskmodel.TaskInterrupted, taskmodel.TaskFailureRuntimeCancelled, ctx.Err())
			}
		}
		if !r.endTask(task, active, outcome, failureCode, failureMessage) {
			return
		}
		r.logEvent(task.ID, "task ended")
		return
	}

}

func (r *Coordinator) startTask(task taskmodel.Task) error {
	startContext, cancelStart := r.transitionContext()
	defer cancelStart()
	if err := r.tasks.ConfirmRunning(startContext, task.ID); err != nil {
		r.logError(task, "start product task", err)
		return err
	}
	return nil
}

// execute 运行 Provider/tool loop，并把取消转换为可持久化的结果。
// loop 不拥有持久化生命周期，因此 context 取消后仍会交由业务服务保存结束状态。
func (r *Coordinator) execute(
	ctx context.Context,
	task taskmodel.Task,
	messageWriter toolcall.Writer,
) (taskmodel.TaskOutcome, taskmodel.TaskFailureCode, string) {
	ctx, scope := r.hookDispatcher.Start(ctx, hook.Operation{
		Identity: hook.Identity{SessionID: task.SessionID, AgentID: task.AgentID, TaskID: task.ID},
		Kind:     hook.OperationAgent,
		Name:     task.Config.AgentDefinitionID.String(),
	})

	content, continuation, err := r.tasks.LoadContext(ctx, task)
	outcome, failureCode := taskmodel.TaskFailed, taskmodel.TaskFailureStorage
	if err == nil {
		outcome, failureCode, err = r.loop.Run(ctx, task, content, continuation, messageWriter)
	}
	finalOutcome, finalCode, message := r.executionResult(ctx, outcome, failureCode, err)
	scope.End(hook.Result{Outcome: finalOutcome, Err: err})
	r.logEvent(task.ID, "provider completed")
	if err != nil {
		r.logError(task, "run provider", err)
	}
	return finalOutcome, finalCode, message
}

// executionResult 读取 Agent 提供的取消原因；持久化控制命令由结束事务最终仲裁。
func (r *Coordinator) executionResult(
	ctx context.Context,
	outcome taskmodel.TaskOutcome,
	failureCode taskmodel.TaskFailureCode,
	err error,
) (taskmodel.TaskOutcome, taskmodel.TaskFailureCode, string) {
	failureMessage := taskmodel.TaskFailureMessage(failureCode, err)
	if ctx.Err() != nil {
		outcome = taskmodel.TaskInterrupted
		if errors.Is(context.Cause(ctx), ErrExecutionPaused) {
			outcome = taskmodel.TaskPaused
		}
		failureCode = taskmodel.TaskFailureRuntimeCancelled
		failureMessage = ""
	} else if err != nil {
		outcome = taskmodel.TaskFailed
		if failureCode == "" {
			failureCode = taskmodel.TaskFailureRuntimeFailed
		}
	}
	outcome, failureCode = normalizeOutcome(outcome, failureCode)
	if failureCode != taskmodel.TaskFailureProvider {
		failureMessage = ""
	}
	return outcome, failureCode, failureMessage
}

func (r *Coordinator) endTask(
	task taskmodel.Task,
	active *activeTask,
	outcome taskmodel.TaskOutcome,
	failureCode taskmodel.TaskFailureCode,
	failureMessage string,
) bool {
	// 先保留结算结果与占用，失败后只重试结算。
	active.result = &taskResult{outcome: outcome, code: failureCode, message: failureMessage}
	r.activeTasks.BeginEnding(task.AgentID, active.taskID)
	endContext, cancelEnd := r.transitionContext()
	defer cancelEnd()
	endedTask, err := r.tasks.Settle(
		endContext,
		task.ID,
		outcome,
		failureCode,
		failureMessage,
	)
	if err != nil {
		r.logError(task, "end product task", err)
		return false
	}
	// 结算提交后才释放执行槽；入队唤醒不能在暂停或取消结算途中抢先续跑。
	r.activeTasks.Release(task.AgentID, active.taskID)
	// 只有已提交的最终结果允许续跑；已提交的暂停或取消可以覆盖 loop 的正常完成。
	if endedTask.Outcome == taskmodel.TaskCompleted || endedTask.Outcome == taskmodel.TaskYielded {
		queueContext, cancelQueue := r.transitionContext()
		defer cancelQueue()
		if _, err := r.StartNext(queueContext, task.AgentID); err != nil {
			r.logError(task, "prepare next queued task", err)
		}
	}
	return true
}

func normalizeOutcome(
	outcome taskmodel.TaskOutcome,
	failureCode taskmodel.TaskFailureCode,
) (taskmodel.TaskOutcome, taskmodel.TaskFailureCode) {
	if !knownOutcome(outcome) {
		outcome = taskmodel.TaskFailed
		failureCode = taskmodel.TaskFailureRuntimeInvalid
	}
	if outcome == taskmodel.TaskFailed && failureCode == "" {
		failureCode = taskmodel.TaskFailureRuntimeFailed
	}
	if !taskmodel.ValidFailureCode(failureCode) {
		outcome = taskmodel.TaskFailed
		failureCode = taskmodel.TaskFailureRuntimeFailed
	}
	return outcome, failureCode
}

func (r *Coordinator) logError(task taskmodel.Task, stage string, err error) {
	if err == nil {
		return
	}
	r.logger.Errorf("task id=%s stage=%s failed: %v", task.ID, stage, err)
}

func (r *Coordinator) logEvent(taskID identity.TaskID, stage string) {
	r.logger.Infof("task id=%s stage=%s", taskID, stage)
}

func knownOutcome(outcome taskmodel.TaskOutcome) bool {
	switch outcome {
	case taskmodel.TaskAwaitingApproval, taskmodel.TaskCompleted, taskmodel.TaskYielded, taskmodel.TaskPaused,
		taskmodel.TaskFailed, taskmodel.TaskInterrupted:
		return true
	default:
		return false
	}
}
