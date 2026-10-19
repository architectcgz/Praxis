package coordinator

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/hook"
	"praxis/internal/identity"
	"praxis/internal/loop"
	"praxis/internal/repository"
	agentservice "praxis/internal/service/agent"
	"time"
)

func (r *Coordinator) run(
	ctx context.Context,
	task agentmodel.Execution,
	active *activeTask,
) {
	r.logEvent(task.ID, "task started")
	defer r.activeTasks.runReturned(task.AgentID, active)
	if active.result != nil {
		r.endExecution(task, active, active.result.outcome, active.result.code, active.result.message)
		return
	}

	if err := r.startExecution(task); err != nil {
		outcome, code, message := r.executionResult(ctx, agentmodel.ExecutionFailed, agentmodel.ExecutionFailureInternal, err)
		r.endExecution(task, active, outcome, code, message)
		return
	}
	if ctx.Err() != nil {
		outcome, code, message := r.executionResult(ctx, agentmodel.ExecutionInterrupted, agentmodel.ExecutionFailureCancelled, ctx.Err())
		r.endExecution(task, active, outcome, code, message)
		return
	}

	messageWriter, err := r.messages.Writer(ctx, task.SessionID, task.AgentID)
	if err != nil || messageWriter == nil {
		if err == nil {
			err = errors.New("message streams returned a nil writer")
		}
		r.logError(task, "resolve message writer", err)
		outcome, code, message := r.executionResult(ctx, agentmodel.ExecutionFailed, agentmodel.ExecutionFailureInternal, err)
		r.endExecution(task, active, outcome, code, message)
		return
	}

	for {
		awaiting, outcome, failureCode, failureMessage := r.execute(ctx, task, messageWriter)
		if awaiting && ctx.Err() == nil {
			if err := r.operations.WaitForApproval(ctx, task.ID); err != nil {
				outcome, failureCode, failureMessage = r.executionResult(ctx, agentmodel.ExecutionFailed, agentmodel.ExecutionFailureStorage, err)
				r.endExecution(task, active, outcome, failureCode, failureMessage)
				return
			}
			select {
			case <-active.wake:
				if err := r.startExecution(task); err != nil {
					outcome, failureCode, failureMessage = r.executionResult(ctx, agentmodel.ExecutionFailed, agentmodel.ExecutionFailureStorage, err)
					r.endExecution(task, active, outcome, failureCode, failureMessage)
					return
				}
				continue
			case <-ctx.Done():
				outcome, failureCode, failureMessage = r.executionResult(ctx, agentmodel.ExecutionInterrupted, agentmodel.ExecutionFailureCancelled, ctx.Err())
			}
		}
		if ctx.Err() != nil {
			outcome, failureCode, failureMessage = r.executionResult(ctx, outcome, failureCode, ctx.Err())
		}
		if !r.endExecution(task, active, outcome, failureCode, failureMessage) {
			return
		}
		r.logEvent(task.ID, "task ended")
		return
	}

}

func (r *Coordinator) startExecution(task agentmodel.Execution) error {
	startContext, cancelStart := r.transitionContext()
	defer cancelStart()
	if err := r.operations.ConfirmRunning(startContext, task.ID); err != nil {
		r.logError(task, "start product task", err)
		return err
	}
	return nil
}

// execute 运行 Provider/tool loop，并把取消转换为可持久化的结果。
// loop 不拥有持久化生命周期，因此 context 取消后仍会交由业务服务保存结束状态。
func (r *Coordinator) execute(
	ctx context.Context,
	task agentmodel.Execution,
	messageWriter repository.MessageWriter,
) (bool, agentmodel.ExecutionOutcome, agentmodel.ExecutionFailureCode, string) {
	ctx, scope := r.hookDispatcher.Start(ctx, hook.Operation{
		Identity: hook.Identity{SessionID: task.SessionID, AgentID: task.AgentID, ExecutionID: task.ID},
		Kind:     hook.OperationAgent,
		Name:     task.Config.AgentDefinitionID.String(),
	})

	awaiting := false
	content, continuation, err := r.operations.LoadContext(ctx, task)
	outcome, failureCode := agentmodel.ExecutionFailed, agentmodel.ExecutionFailureStorage
	if err == nil {
		var result loop.Result
		result, err = r.loop.Run(ctx, loop.Input{
			Progress:     executionProgress{operations: r.operations, id: task.ID},
			ID:           task.ID,
			SessionID:    task.SessionID,
			AgentID:      task.AgentID,
			RequestID:    task.RequestID,
			Config:       loop.Config{Model: task.Config.Model.Clone(), WorkspacePath: task.Config.WorkspacePath},
			Content:      content,
			Continuation: continuation,
			Writer:       messageWriter,
		})
		outcome, failureCode = agentmodel.ExecutionCompleted, ""
		if err != nil {
			outcome = agentmodel.ExecutionFailed
			failureCode = loopFailureCode(err)
		} else if result.Reason == loop.EndAwaitingApproval {
			awaiting = true
		}
	}
	finalOutcome, finalCode, message := r.executionResult(ctx, outcome, failureCode, err)
	scope.End(hook.Result{Outcome: string(finalOutcome), Err: err})
	r.logEvent(task.ID, "provider completed")
	if err != nil {
		r.logError(task, "run provider", err)
	}
	return awaiting, finalOutcome, finalCode, message
}

// executionResult 读取 Agent 提供的取消原因；持久化控制命令由结束事务最终仲裁。
func (r *Coordinator) executionResult(
	ctx context.Context,
	outcome agentmodel.ExecutionOutcome,
	failureCode agentmodel.ExecutionFailureCode,
	err error,
) (agentmodel.ExecutionOutcome, agentmodel.ExecutionFailureCode, string) {
	failureMessage := agentservice.ExecutionFailureMessage(failureCode, err)
	if ctx.Err() != nil {
		outcome = agentmodel.ExecutionInterrupted
		if errors.Is(context.Cause(ctx), ErrExecutionPaused) {
			outcome = agentmodel.ExecutionPaused
		}
		failureCode = agentmodel.ExecutionFailureCancelled
		failureMessage = ""
	} else if err != nil {
		outcome = agentmodel.ExecutionFailed
		if failureCode == "" {
			failureCode = agentmodel.ExecutionFailureInternal
		}
	}
	outcome, failureCode = normalizeOutcome(outcome, failureCode)
	if failureCode != agentmodel.ExecutionFailureProvider {
		failureMessage = ""
	}
	return outcome, failureCode, failureMessage
}

func (r *Coordinator) endExecution(
	task agentmodel.Execution,
	active *activeTask,
	outcome agentmodel.ExecutionOutcome,
	failureCode agentmodel.ExecutionFailureCode,
	failureMessage string,
) bool {
	// 先保留结算结果与占用，失败后只重试结算。
	active.result = &executionResultState{outcome: outcome, code: failureCode, message: failureMessage}
	r.activeTasks.BeginEnding(task.AgentID, active.executionID)
	var endedExecution agentmodel.Execution
	retryDelay := 100 * time.Millisecond
	for {
		endContext, cancelEnd := r.transitionContext()
		var err error
		endedExecution, err = r.operations.Settle(endContext, task.ID, outcome, failureCode, failureMessage)
		cancelEnd()
		if err == nil {
			break
		}
		r.logError(task, "settle execution", err)
		r.mu.Lock()
		closed := r.closed
		r.mu.Unlock()
		if closed {
			return false
		}
		timer := time.NewTimer(retryDelay)
		<-timer.C
		retryDelay = min(5*time.Second, retryDelay*2)
	}

	// 结算提交后才释放执行槽；入队唤醒不能在暂停或取消结算途中抢先续跑。
	r.activeTasks.Release(task.AgentID, active.executionID)
	// 只有已提交的最终结果允许续跑；已提交的暂停或取消可以覆盖 loop 的正常完成。
	if endedExecution.Outcome == agentmodel.ExecutionCompleted || endedExecution.Outcome == agentmodel.ExecutionYielded {
		queueContext, cancelQueue := r.transitionContext()
		defer cancelQueue()
		if _, err := r.StartNext(queueContext, task.AgentID); err != nil {
			r.logError(task, "prepare next queued task", err)
		}
	}
	return true
}

func normalizeOutcome(
	outcome agentmodel.ExecutionOutcome,
	failureCode agentmodel.ExecutionFailureCode,
) (agentmodel.ExecutionOutcome, agentmodel.ExecutionFailureCode) {
	if !knownOutcome(outcome) {
		outcome = agentmodel.ExecutionFailed
		failureCode = agentmodel.ExecutionFailureInvalidOutcome
	}
	if outcome == agentmodel.ExecutionFailed && failureCode == "" {
		failureCode = agentmodel.ExecutionFailureInternal
	}
	if !agentmodel.ValidFailureCode(failureCode) {
		outcome = agentmodel.ExecutionFailed
		failureCode = agentmodel.ExecutionFailureInternal
	}
	return outcome, failureCode
}

func (r *Coordinator) logError(task agentmodel.Execution, stage string, err error) {
	if err == nil {
		return
	}
	r.logger.Errorf("task id=%s stage=%s failed: %v", task.ID, stage, err)
}

func (r *Coordinator) logEvent(executionID identity.AgentExecutionID, stage string) {
	r.logger.Infof("task id=%s stage=%s", executionID, stage)
}

func knownOutcome(outcome agentmodel.ExecutionOutcome) bool {
	switch outcome {
	case agentmodel.ExecutionCompleted, agentmodel.ExecutionYielded, agentmodel.ExecutionPaused,
		agentmodel.ExecutionFailed, agentmodel.ExecutionInterrupted:
		return true
	default:
		return false
	}
}

// loopFailureCode 在业务结算边界把 Loop 错误转换为执行失败码。
func loopFailureCode(err error) agentmodel.ExecutionFailureCode {
	switch loop.FailureClassification(err) {
	case loop.FailureProviderUnavailable:
		return agentmodel.ExecutionFailureProviderUnavailable
	case loop.FailureProvider:
		return agentmodel.ExecutionFailureProvider
	case loop.FailureTool:
		return agentmodel.ExecutionFailureTool
	case loop.FailurePolicyBlocked:
		return agentmodel.ExecutionFailurePolicyBlocked
	case loop.FailureResourceLimit:
		return agentmodel.ExecutionFailureResourceLimit
	case loop.FailureStorage:
		return agentmodel.ExecutionFailureStorage
	case loop.FailureContract:
		return agentmodel.ExecutionFailureContract
	case loop.FailureBusy:
		return agentmodel.ExecutionFailureBusy
	default:
		return agentmodel.ExecutionFailureInternal
	}
}
