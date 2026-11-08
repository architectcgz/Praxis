package coordinator

import (
	"context"
	"errors"
	agentrun "praxis/internal/core/agent/run"
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
	run *agentrun.Run,
	active *activeRun,
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
	if err := r.startRun(run, ""); err != nil {
		outcome, code, message := r.runResult(ctx, agentrun.RunFailed, agentrun.RunFailureInternal, err)
		r.endRun(task, active, outcome, code, message)
		return
	}
	if task.Progress.FinalAssistantID != "" {
		// 完成证据优先于迟到的取消；这里只允许补做摘要，不能重跑用户任务。
		if err := r.operations.Compact(ctx, run, &runner); err != nil {
			r.logError(task, "compact completed context", err)
			if loop.FailureClassification(err) == loop.FailureStorage {
				r.endRun(task, active, agentrun.RunFailed, agentrun.RunFailureStorage, "")
				return
			}
		}
		r.endRun(task, active, agentrun.RunCompleted, "", "")
		return
	}
	if ctx.Err() != nil {
		outcome, code, message := r.runResult(ctx, agentrun.RunInterrupted, agentrun.RunFailureCancelled, ctx.Err())
		r.endRun(task, active, outcome, code, message)
		return
	}

	session, err := r.sessionRecords.Get(ctx, task.SessionID)
	if err != nil {
		outcome, code, message := r.runResult(ctx, agentrun.RunFailed, agentrun.RunFailureStorage, err)
		r.endRun(task, active, outcome, code, message)
		return
	}
	project, err := r.projects.Get(ctx, session.ProjectID)
	if err != nil || project.State != projectmodel.ProjectActive {
		if err == nil {
			err = projectservice.ErrProjectInvalid
		}
		r.logError(task, "resolve current project", err)
		outcome, code, message := r.runResult(ctx, agentrun.RunFailed, agentrun.RunFailureStorage, err)
		r.endRun(task, active, outcome, code, message)
		return
	}
	executor := r.tools.ForRun(run, project.Path, active.config.Tools, active.config.ApprovalMode, runner.Provider, func(waitCtx context.Context) error {
		return r.waitForToolApproval(waitCtx, active)
	})
	outcome, failureCode, failureMessage := r.execute(ctx, run, executor, &runner)
	if r.endRun(task, active, outcome, failureCode, failureMessage) {
		r.logEvent(task.ID, "task ended")
	}

}

func (r *Coordinator) startRun(run *agentrun.Run, resumeReason string) error {
	task := run.Snapshot()
	startContext, cancelStart := r.transitionContext()
	defer cancelStart()
	if err := r.operations.ConfirmRunning(startContext, run); err != nil {
		r.logError(task, "start product task", err)
		return err
	}
	event := hook.RunEvent{Identity: hook.Identity{
		SourceTaskID: task.SourceTaskID,
		SessionID:    task.SessionID,
		AgentID:      task.AgentID,
		RunID:        task.ID,
	}}
	if resumeReason == "" && !task.StartedAt.IsZero() {
		resumeReason = "recovery"
	}
	var hookErr error
	if resumeReason != "" {
		event.ResumeReason = resumeReason
		hookErr = r.hookDispatcher.RunResumed(context.WithoutCancel(startContext), event)
	} else {
		hookErr = r.hookDispatcher.RunStarted(context.WithoutCancel(startContext), event)
	}
	if hookErr != nil {
		r.logger.Errorf("execution start hook failed: %v", hookErr)
	}
	return nil
}

// execute 交给执行服务编排模型与工具，并把取消转换为可持久化的结果。
// context 取消后仍由业务服务记录结束状态。
func (r *Coordinator) execute(
	ctx context.Context,
	run *agentrun.Run,
	executor loop.ToolExecutor,
	runner *loop.Runner,
) (agentrun.RunOutcome, agentrun.RunFailureCode, string) {
	task := run.Snapshot()
	ctx, scope := r.hookDispatcher.Start(ctx, hook.Operation{
		Identity: hook.Identity{SessionID: task.SessionID, AgentID: task.AgentID, RunID: task.ID},
		Kind:     hook.OperationAgent,
		Name:     task.Config.AgentDefinitionID.String(),
	})

	err := r.operations.Execute(ctx, run, runner, executor)
	outcome := agentrun.RunCompleted
	var failureCode agentrun.RunFailureCode
	if err != nil {
		outcome, failureCode = agentrun.RunFailed, loopFailureCode(err)
	}
	if _, ok := errors.AsType[*hook.Error](err); ok && loop.FailureClassification(err) == loop.FailureInternal {
		failureCode = agentrun.RunFailureInternal
	}
	finalOutcome, finalCode, message := r.runResult(ctx, outcome, failureCode, err)
	scope.End(hook.Result{Outcome: string(finalOutcome), Err: err})
	r.logEvent(task.ID, "provider completed")
	if err != nil {
		r.logError(task, "run provider", err)
	}
	return finalOutcome, finalCode, message
}

// runResult 保留存储失败；其他错误仅在实际返回取消时映射为中断。
func (r *Coordinator) runResult(
	ctx context.Context,
	outcome agentrun.RunOutcome,
	failureCode agentrun.RunFailureCode,
	err error,
) (agentrun.RunOutcome, agentrun.RunFailureCode, string) {
	failureMessage := agentservice.RunFailureMessage(failureCode, err)
	if err != nil && failureCode != agentrun.RunFailureStorage && ctx.Err() != nil && (errors.Is(err, ctx.Err()) || errors.Is(err, context.Cause(ctx))) {
		outcome = agentrun.RunInterrupted
		failureCode = agentrun.RunFailureCancelled
		if errors.Is(context.Cause(ctx), errRunInterrupted) {
			failureCode = agentrun.RunFailureRequestCanceled
		}
		failureMessage = ""
	} else if err != nil {
		outcome = agentrun.RunFailed
	}
	if outcome == agentrun.RunFailed && failureCode == "" {
		failureCode = agentrun.RunFailureInternal
	}
	if failureCode != agentrun.RunFailureProvider {
		failureMessage = ""
	}
	return outcome, failureCode, failureMessage
}

func (r *Coordinator) endRun(
	task agentrun.RunRecord,
	active *activeRun,
	outcome agentrun.RunOutcome,
	failureCode agentrun.RunFailureCode,
	failureMessage string,
) bool {
	// 仅在内存结算尚未完成时保留结果重试；日志失败不阻止已接纳终态释放占用。
	r.mu.Lock()
	active.ending = true
	r.mu.Unlock()
	var endedRun agentrun.RunRecord
	retryDelay := 100 * time.Millisecond
	for {
		endContext, cancelEnd := r.transitionContext()
		var err error
		endedRun, err = r.operations.Settle(endContext, active.run, outcome, failureCode, failureMessage)
		cancelEnd()
		if err == nil {
			break
		}
		r.logError(task, "settle execution", err)
		if endedRun.Status == agentrun.RunEnded {
			r.loop.Emitter.Emit(coreevent.Event{
				Source: coreevent.SourceForRun(active.run, ""),
				Payload: coreevent.ErrorOccurred{
					Message: "Failed to save session entries. The result is kept in memory: " + err.Error(),
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
	interrupted := errors.Is(cause, errRunInterrupted)
	scheduleRequested := active.scheduleRequested
	main := active.run.Main()
	if main {
		delete(r.runs, runKey{task.AgentID, task.BranchID})
		active.run = nil
		active.cancel(nil)
	}
	r.mu.Unlock()
	// 中断后只响应新的显式调度意图，不因旧执行恰好正常完成而自动领取队列。
	if main && (scheduleRequested || !interrupted && (endedRun.Outcome == agentrun.RunCompleted || endedRun.Outcome == agentrun.RunYielded)) {
		queueContext, cancelQueue := r.transitionContext()
		defer cancelQueue()
		if err := r.scheduleBinding(queueContext, task.SessionID, active.bindingID, false); err != nil {
			r.logError(task, "prepare next queued task", err)
		}
	}
	return true
}

func (r *Coordinator) logError(task agentrun.RunRecord, stage string, err error) {
	if err == nil {
		return
	}
	r.logger.Errorf("task id=%s stage=%s failed: %v", task.ID, stage, err)
}

func (r *Coordinator) logEvent(runID identity.RunID, stage string) {
	r.logger.Infof("task id=%s stage=%s", runID, stage)
}

// loopFailureCode 在业务结算边界把 Loop 错误转换为执行失败码。
func loopFailureCode(err error) agentrun.RunFailureCode {
	switch loop.FailureClassification(err) {
	case loop.FailureProviderUnavailable:
		return agentrun.RunFailureProviderUnavailable
	case loop.FailureProvider:
		return agentrun.RunFailureProvider
	case loop.FailureTool:
		return agentrun.RunFailureTool
	case loop.FailurePolicyBlocked:
		return agentrun.RunFailurePolicyBlocked
	case loop.FailureResourceLimit:
		return agentrun.RunFailureResourceLimit
	case loop.FailureStorage:
		return agentrun.RunFailureStorage
	case loop.FailureContract:
		return agentrun.RunFailureContract
	case loop.FailureBusy:
		return agentrun.RunFailureBusy
	default:
		return agentrun.RunFailureInternal
	}
}
