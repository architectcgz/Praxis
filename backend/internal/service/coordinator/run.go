package coordinator

import (
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
	"time"
)

func (r *Coordinator) run(
	ctx context.Context,
	runtime *agentexecution.Runtime,
	active *activeExecution,
) {
	task := runtime.Snapshot()
	r.logEvent(task.ID, "task started")
	if err := r.startExecution(runtime, ""); err != nil {
		outcome, code, message := r.executionResult(ctx, agentexecution.ExecutionFailed, agentexecution.ExecutionFailureInternal, err)
		r.endExecution(task, active, outcome, code, message)
		return
	}
	if task.Progress.FinalAssistantID != "" {
		// 已提交的完成证据不受恢复时重放的暂停信号影响，也不能再次请求模型。
		r.endExecution(task, active, agentexecution.ExecutionCompleted, "", "")
		return
	}
	if ctx.Err() != nil {
		outcome, code, message := r.executionResult(ctx, agentexecution.ExecutionInterrupted, agentexecution.ExecutionFailureCancelled, ctx.Err())
		r.endExecution(task, active, outcome, code, message)
		return
	}

	session, err := r.sessions.Get(ctx, task.SessionID)
	if err != nil {
		outcome, code, message := r.executionResult(ctx, agentexecution.ExecutionFailed, agentexecution.ExecutionFailureStorage, err)
		r.endExecution(task, active, outcome, code, message)
		return
	}
	workspace, err := r.workspaces.Get(ctx, session.WorkspaceID)
	if err != nil || workspace.ProjectID != session.ProjectID || workspace.State != workspacemodel.WorkspaceReady {
		if err == nil {
			err = errorcode.ErrRequestConflict
		}
		r.logError(task, "resolve current workspace", err)
		outcome, code, message := r.executionResult(ctx, agentexecution.ExecutionFailed, agentexecution.ExecutionFailureStorage, err)
		r.endExecution(task, active, outcome, code, message)
		return
	}
	executor := r.tools.ForExecution(runtime, workspace.Path, func(waitCtx context.Context) error {
		return r.waitForToolApproval(waitCtx, active)
	})
	outcome, failureCode, failureMessage := r.execute(ctx, runtime, executor)
	if r.endExecution(task, active, outcome, failureCode, failureMessage) {
		r.logEvent(task.ID, "task ended")
	}

}

func (r *Coordinator) startExecution(runtime *agentexecution.Runtime, resumeReason string) error {
	task := runtime.Snapshot()
	startContext, cancelStart := r.transitionContext()
	defer cancelStart()
	if err := r.operations.ConfirmRunning(startContext, runtime); err != nil {
		r.logError(task, "start product task", err)
		return err
	}
	event := hook.ExecutionEvent{Identity: hook.Identity{
		SourceTaskID: task.SourceTaskID,
		SessionID:    task.SessionID,
		AgentID:      task.AgentID,
		ExecutionID:  task.ID,
	}}
	if resumeReason == "" && !task.StartedAt.IsZero() {
		resumeReason = "recovery"
	}
	var hookErr error
	if resumeReason != "" {
		event.ResumeReason = resumeReason
		hookErr = r.hookDispatcher.ExecutionResumed(context.WithoutCancel(startContext), event)
	} else {
		hookErr = r.hookDispatcher.ExecutionStarted(context.WithoutCancel(startContext), event)
	}
	if hookErr != nil {
		r.logger.Errorf("execution start hook failed: %v", hookErr)
	}
	return nil
}

// execute 运行 Provider/tool loop，并把取消转换为可持久化的结果。
// loop 不拥有持久化生命周期，因此 context 取消后仍会交由业务服务保存结束状态。
func (r *Coordinator) execute(
	ctx context.Context,
	runtime *agentexecution.Runtime,
	executor loop.ToolExecutor,
) (agentexecution.ExecutionOutcome, agentexecution.ExecutionFailureCode, string) {
	task := runtime.Snapshot()
	ctx, scope := r.hookDispatcher.Start(ctx, hook.Operation{
		Identity: hook.Identity{SessionID: task.SessionID, AgentID: task.AgentID, ExecutionID: task.ID},
		Kind:     hook.OperationAgent,
		Name:     task.Config.AgentDefinitionID.String(),
	})

	executionInput, completed, err := r.operations.PrepareLoop(ctx, runtime)
	outcome, failureCode := agentexecution.ExecutionFailed, agentexecution.ExecutionFailureStorage
	if err == nil {
		if !completed {
			_, err = r.loop.Run(ctx, executionInput, loop.ExecutionDependencies{
				ToolExecutor: executor,
			})
		}
		outcome, failureCode = agentexecution.ExecutionCompleted, ""
		if err != nil {
			outcome = agentexecution.ExecutionFailed
			failureCode = loopFailureCode(err)
		}
	}
	if _, ok := errors.AsType[*hook.Error](err); ok && loop.FailureClassification(err) == loop.FailureInternal {
		failureCode = agentexecution.ExecutionFailureInternal
	}
	finalOutcome, finalCode, message := r.executionResult(ctx, outcome, failureCode, err)
	scope.End(hook.Result{Outcome: string(finalOutcome), Err: err})
	r.logEvent(task.ID, "provider completed")
	if err != nil {
		r.logError(task, "run provider", err)
	}
	return finalOutcome, finalCode, message
}

// executionResult 只将实际返回的取消错误映射为暂停或中断；正常完成不受迟到信号影响。
func (r *Coordinator) executionResult(
	ctx context.Context,
	outcome agentexecution.ExecutionOutcome,
	failureCode agentexecution.ExecutionFailureCode,
	err error,
) (agentexecution.ExecutionOutcome, agentexecution.ExecutionFailureCode, string) {
	failureMessage := agentservice.ExecutionFailureMessage(failureCode, err)
	if err != nil && ctx.Err() != nil && (errors.Is(err, ctx.Err()) || errors.Is(err, context.Cause(ctx))) {
		outcome = agentexecution.ExecutionInterrupted
		failureCode = agentexecution.ExecutionFailureCancelled
		switch cause := context.Cause(ctx); {
		case errors.Is(cause, errExecutionPaused):
			outcome = agentexecution.ExecutionPaused
			failureCode = agentexecution.ExecutionFailureRequestCanceled
		case errors.Is(cause, errExecutionStopped):
			failureCode = agentexecution.ExecutionFailureRequestCanceled
		}
		failureMessage = ""
	} else if err != nil {
		outcome = agentexecution.ExecutionFailed
	}
	if outcome == agentexecution.ExecutionFailed && failureCode == "" {
		failureCode = agentexecution.ExecutionFailureInternal
	}
	if failureCode != agentexecution.ExecutionFailureProvider {
		failureMessage = ""
	}
	return outcome, failureCode, failureMessage
}

func (r *Coordinator) endExecution(
	task agentexecution.Execution,
	active *activeExecution,
	outcome agentexecution.ExecutionOutcome,
	failureCode agentexecution.ExecutionFailureCode,
	failureMessage string,
) bool {
	// 结果保留在本执行协程中；结算失败不退出、不释放忙碌，也不重新运行模型或工具。
	r.mu.Lock()
	active.ending = true
	r.mu.Unlock()
	var endedExecution agentexecution.Execution
	retryDelay := 100 * time.Millisecond
	for {
		endContext, cancelEnd := r.transitionContext()
		var err error
		endedExecution, err = r.operations.Settle(endContext, active.runtime, outcome, failureCode, failureMessage)
		cancelEnd()
		if err == nil {
			break
		}
		r.logError(task, "settle execution", err)
		timer := time.NewTimer(retryDelay)
		<-timer.C
		retryDelay = min(5*time.Second, retryDelay*2)
	}

	// 结算成功后才释放槽位；控制信号立即改变展示，但不会提前放行下一次执行。
	r.mu.Lock()
	cause := context.Cause(active.ctx)
	controlled := errors.Is(cause, errExecutionPaused) || errors.Is(cause, errExecutionStopped)
	resumeRequested := active.resumeRequested
	main := active.runtime.Main()
	if main {
		delete(r.executions, task.AgentID)
		active.runtime = nil
		active.cancel(nil)
	}
	r.mu.Unlock()
	// 控制后只响应新的显式调度意图，不因旧执行恰好正常完成而自动领取队列。
	if main && (resumeRequested || !controlled && (endedExecution.Outcome == agentexecution.ExecutionCompleted || endedExecution.Outcome == agentexecution.ExecutionYielded)) {
		queueContext, cancelQueue := r.transitionContext()
		defer cancelQueue()
		if err := r.scheduleSession(queueContext, task.SessionID, false); err != nil {
			r.logError(task, "prepare next queued task", err)
		}
	}
	return true
}

func (r *Coordinator) logError(task agentexecution.Execution, stage string, err error) {
	if err == nil {
		return
	}
	r.logger.Errorf("task id=%s stage=%s failed: %v", task.ID, stage, err)
}

func (r *Coordinator) logEvent(executionID identity.AgentExecutionID, stage string) {
	r.logger.Infof("task id=%s stage=%s", executionID, stage)
}

// loopFailureCode 在业务结算边界把 Loop 错误转换为执行失败码。
func loopFailureCode(err error) agentexecution.ExecutionFailureCode {
	switch loop.FailureClassification(err) {
	case loop.FailureProviderUnavailable:
		return agentexecution.ExecutionFailureProviderUnavailable
	case loop.FailureProvider:
		return agentexecution.ExecutionFailureProvider
	case loop.FailureTool:
		return agentexecution.ExecutionFailureTool
	case loop.FailurePolicyBlocked:
		return agentexecution.ExecutionFailurePolicyBlocked
	case loop.FailureResourceLimit:
		return agentexecution.ExecutionFailureResourceLimit
	case loop.FailureStorage:
		return agentexecution.ExecutionFailureStorage
	case loop.FailureContract:
		return agentexecution.ExecutionFailureContract
	case loop.FailureBusy:
		return agentexecution.ExecutionFailureBusy
	default:
		return agentexecution.ExecutionFailureInternal
	}
}
