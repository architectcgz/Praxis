package coordinator

import (
	"context"
	"errors"
	"praxis/internal/engine/loop"
	runmodel "praxis/internal/engine/run"
	"praxis/internal/entry"
	"praxis/internal/errorcode"
	"praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/task"
	"praxis/internal/tools"
)

func (r *Coordinator) run(ctx context.Context, run *runmodel.Run, active *activeRun) {
	defer func() {
		close(active.done)
		if active.parent != nil {
			active.parent.WakeSubagentWaiters()
		}
	}()
	runner := *r.loop
	if runner.Provider == nil {
		runner.Provider = active.config.Provider
	}
	err := run.Execute(ctx, &runner, func(prepareCtx context.Context) (*tools.Discovery, loop.ToolExecutor, error) {
		info, err := r.sessionRecords.Get(prepareCtx, run.SessionID())
		if err != nil {
			return nil, nil, errorcode.ErrStorage.Wrap(err)
		}
		project, err := r.projects.Get(prepareCtx, info.ProjectID)
		if err != nil {
			return nil, nil, errorcode.ErrStorage.Wrap(err)
		}
		var extras []tools.Tool
		if run.Main() {
			extras = tools.SubagentMainTools(tools.SubagentParentOperations{
				Start: func(ctx context.Context, assignment tools.SubagentAssignment, callID string) (tools.SubagentIDs, error) {
					return r.startSubagent(ctx, run, active, assignment, callID)
				},
				Wait: func(ctx context.Context, ids []identity.AgentID, all bool) ([]tools.SubagentReceipt, error) {
					run.BeginWaiting()
					defer run.EndWaiting()
					reports, err := run.WaitSubagents(ctx, ids, all)
					if err != nil {
						return nil, err
					}
					receipts := make([]tools.SubagentReceipt, len(reports))
					for i, report := range reports {
						receipts[i] = tools.SubagentReceipt{
							ReportID: report.ReportID,
							AgentID:  report.AgentID,
							Outcome:  string(report.Outcome),
							Final:    report.Final,
						}
					}
					return receipts, nil
				},
				Send:   run.SendToSubagent,
				Cancel: run.CancelSubagent,
			})
		} else {
			extras = tools.SubagentDelegateTools(run.ReportToParent)
		}
		discovery, catalog, err := tools.NewDiscovery(active.config.Tools, extras)
		if err != nil {
			return nil, nil, err
		}
		authorizer := r.approvals.ForRun(run, project.Path, active.session.ApprovalMode, runner.Provider)
		return discovery, r.tools.ForRun(run, project.Path, catalog, authorizer), nil
	})
	if err != nil {
		r.logger.Errorf("run failed run_id=%s: %v", run.ID(), err)
	}
	if !run.Main() {
		if errors.Is(err, errorcode.ErrStorage) {
			active.parent.Cancel(err)
		}
		if active.onDone != nil {
			if callbackErr := active.onDone(); callbackErr != nil {
				r.logger.Errorf("deliver subagent result: %v", callbackErr)
				active.parent.Cancel(errorcode.ErrStorage.Wrap(callbackErr))
			}
		}
		active.cancel(nil)
		active.parent.CompleteSubagent(run.AgentID())
		r.emitter.Emit(event.Event{Source: run.EventSource(""), Payload: event.SubagentReleased{}})
		return
	}
	queueCtx := context.WithoutCancel(ctx)
	unlock, lockErr := r.lockSession(queueCtx, run.SessionID())
	storageErr := err
	if cause := context.Cause(ctx); errors.Is(cause, errorcode.ErrStorage) {
		storageErr = cause
	}
	storageFailed := errors.Is(storageErr, errorcode.ErrStorage)
	pauseQueue := storageFailed || lockErr != nil
	// 交接失败仍须释放占位，并暂停队列，避免后续任务在未暂停状态下停滞。
	r.mu.Lock()
	current := r.schedules[run.SessionID()]
	current.active = nil
	var notification event.Event
	if pauseQueue {
		current.queue.Pause()
		notification = taskQueueEvent(current)
	}
	var next *activeRun
	var schedule *sessionSchedule
	var input task.Task
	scheduleErr := lockErr
	if lockErr == nil && err == nil && run.Outcome() == entry.OutcomeCompleted && !errors.Is(context.Cause(active.ctx), runmodel.ErrInterrupted) {
		input, schedule, next, scheduleErr = r.claimTaskLocked(queueCtx, run.SessionID())
	}
	r.mu.Unlock()
	if unlock != nil {
		unlock()
	}
	active.cancel(nil)
	if pauseQueue {
		r.emitter.Emit(notification)
	}
	if storageFailed {
		r.emitQueueStorageFailure(run.SessionID(), storageErr)
	}
	if next != nil {
		scheduleErr = r.startNext(queueCtx, input, schedule, next)
	}
	if scheduleErr != nil {
		r.logger.Errorf("prepare queued task session=%s: %v", run.SessionID(), scheduleErr)
	}
}

func (r *Coordinator) emitQueueStorageFailure(sessionID identity.SessionID, err error) {
	r.emitter.Emit(event.Event{
		Source:  event.EventSource{SessionID: sessionID},
		Payload: event.TaskQueueStorageFailed{Message: err.Error()},
	})
}
