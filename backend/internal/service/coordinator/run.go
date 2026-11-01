package coordinator

import (
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
	projectservice "praxis/internal/service/project"
	"time"
)

func (r *Coordinator) run(
	ctx context.Context,
	run *agentexecution.Run,
	active *activeExecution,
) {
	task := run.Snapshot()
	runner := *r.loop
	if runner.Provider == nil {
		runner.Provider = active.config.Provider
	}
	if runner.Tools == nil {
		runner.Tools = active.config.Tools
	}
	r.logEvent(task.ID, "task started")
	if err := r.startExecution(run, ""); err != nil {
		outcome, code, message := r.executionResult(ctx, agentexecution.ExecutionFailed, agentexecution.ExecutionFailureInternal, err)
		r.endExecution(task, active, outcome, code, message)
		return
	}
	if task.Progress.FinalAssistantID != "" {
		// 完成证据优先于迟到的取消；这里只允许补做摘要，不能重跑用户任务。
		input, deps, _, err := r.operations.PrepareLoop(ctx, run)
		if err == nil {
			err = runner.Compact(ctx, input, deps)
		}
		if err != nil {
			r.logError(task, "compact completed context", err)
		}
		r.endExecution(task, active, agentexecution.ExecutionCompleted, "", "")
		return
	}
	if ctx.Err() != nil {
		outcome, code, message := r.executionResult(ctx, agentexecution.ExecutionInterrupted, agentexecution.ExecutionFailureCancelled, ctx.Err())
		r.endExecution(task, active, outcome, code, message)
		return
	}

	session, err := r.sessionRecords.Get(ctx, task.SessionID)
	if err != nil {
		outcome, code, message := r.executionResult(ctx, agentexecution.ExecutionFailed, agentexecution.ExecutionFailureStorage, err)
		r.endExecution(task, active, outcome, code, message)
		return
	}
	project, err := r.projects.Get(ctx, session.ProjectID)
	if err != nil || project.State != projectmodel.ProjectActive {
		if err == nil {
			err = projectservice.ErrProjectInvalid
		}
		r.logError(task, "resolve current project", err)
		outcome, code, message := r.executionResult(ctx, agentexecution.ExecutionFailed, agentexecution.ExecutionFailureStorage, err)
		r.endExecution(task, active, outcome, code, message)
		return
	}
	executor := r.tools.ForExecution(run, project.Path, active.config.Tools, active.config.ApprovalMode, runner.Provider, func(waitCtx context.Context) error {
		return r.waitForToolApproval(waitCtx, active)
	})
	outcome, failureCode, failureMessage := r.execute(ctx, run, executor, &runner)
	if r.endExecution(task, active, outcome, failureCode, failureMessage) {
		r.logEvent(task.ID, "task ended")
	}

}

func (r *Coordinator) startExecution(run *agentexecution.Run, resumeReason string) error {
	task := run.Snapshot()
	startContext, cancelStart := r.transitionContext()
	defer cancelStart()
	if err := r.operations.ConfirmRunning(startContext, run); err != nil {
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
	run *agentexecution.Run,
	executor loop.ToolExecutor,
	runner *loop.Runner,
) (agentexecution.ExecutionOutcome, agentexecution.ExecutionFailureCode, string) {
	task := run.Snapshot()
	ctx, scope := r.hookDispatcher.Start(ctx, hook.Operation{
		Identity: hook.Identity{SessionID: task.SessionID, AgentID: task.AgentID, ExecutionID: task.ID},
		Kind:     hook.OperationAgent,
		Name:     task.Config.AgentDefinitionID.String(),
	})

	executionInput, deps, completed, err := r.operations.PrepareLoop(ctx, run)
	outcome, failureCode := agentexecution.ExecutionFailed, agentexecution.ExecutionFailureStorage
	if err == nil {
		if !completed {
			deps.ToolExecutor = executor
			_, err = runner.Run(ctx, executionInput, deps)
		} else if compressionErr := runner.Compact(ctx, executionInput, deps); compressionErr != nil {
			// 重复准入只允许补做摘要，不能重跑已完成的模型和工具任务。
			r.logError(task, "compact completed context", compressionErr)
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
	task agentexecution.ExecutionRecord,
	active *activeExecution,
	outcome agentexecution.ExecutionOutcome,
	failureCode agentexecution.ExecutionFailureCode,
	failureMessage string,
) bool {
	// 仅在内存结算尚未完成时保留结果重试；日志失败不阻止已接纳终态释放占用。
	r.mu.Lock()
	active.ending = true
	r.mu.Unlock()
	var endedExecution agentexecution.ExecutionRecord
	retryDelay := 100 * time.Millisecond
	for {
		endContext, cancelEnd := r.transitionContext()
		var err error
		endedExecution, err = r.operations.Settle(endContext, active.run, outcome, failureCode, failureMessage)
		cancelEnd()
		if err == nil {
			break
		}
		r.logError(task, "settle execution", err)
		if endedExecution.Status == agentexecution.ExecutionEnded {
			r.loop.Emitter.Emit(coreevent.Event{
				Scope: coreevent.ScopeForRun(active.run, ""),
				Payload: coreevent.ErrorOccurred{
					Message: "Failed to save session history. The result is kept in memory: " + err.Error(),
				},
			})
			break
		}
		timer := time.NewTimer(retryDelay)
		<-timer.C
		retryDelay = min(5*time.Second, retryDelay*2)
	}

	// 内存终态接纳后释放槽位；日志失败已单独通知界面。
	r.mu.Lock()
	cause := context.Cause(active.ctx)
	controlled := errors.Is(cause, errExecutionPaused) || errors.Is(cause, errExecutionStopped)
	resumeRequested := active.resumeRequested
	main := active.run.Main()
	if main {
		delete(r.executions, executionKey{task.AgentID, task.BranchID})
		active.run = nil
		active.cancel(nil)
	}
	r.mu.Unlock()
	// 控制后只响应新的显式调度意图，不因旧执行恰好正常完成而自动领取队列。
	if main && (resumeRequested || !controlled && (endedExecution.Outcome == agentexecution.ExecutionCompleted || endedExecution.Outcome == agentexecution.ExecutionYielded)) {
		queueContext, cancelQueue := r.transitionContext()
		defer cancelQueue()
		if err := r.scheduleBinding(queueContext, active.bindingID, false); err != nil {
			r.logError(task, "prepare next queued task", err)
		}
	}
	return true
}

func (r *Coordinator) logError(task agentexecution.ExecutionRecord, stage string, err error) {
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
