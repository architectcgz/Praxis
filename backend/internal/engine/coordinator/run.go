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
	"praxis/internal/tool"
	subagenttool "praxis/internal/tool/subagent"
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
	err := run.Execute(ctx, &runner, func(prepareCtx context.Context) (tool.ToolCatalog, loop.ToolExecutor, error) {
		info, err := r.sessionRecords.Get(prepareCtx, run.SessionID())
		if err != nil {
			return nil, nil, errorcode.ErrStorage.Wrap(err)
		}
		project, err := r.projects.Get(prepareCtx, info.ProjectID)
		if err != nil {
			return nil, nil, errorcode.ErrStorage.Wrap(err)
		}
		var extras []tool.Tool
		if run.Main() {
			extras = subagenttool.MainTools(mainSubagentRuntime{
				coordinator: r,
				run:         run,
				active:      active,
			})
		} else {
			extras = subagenttool.ChildTools(run)
		}
		catalog, err := tool.ComposeCatalog(active.config.Tools, extras...)
		if err != nil {
			return nil, nil, err
		}
		authorizer := r.approvals.ForRun(run, project.Path, active.session.ApprovalMode, runner.Provider)
		return catalog, r.tools.ForRun(run, project.Path, catalog, authorizer), nil
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
	queueCtx, cancel := r.transitionContext()
	defer cancel()
	unlock, _ := r.lockSession(context.Background(), run.SessionID())
	r.mu.Lock()
	current := r.schedules[run.SessionID()]
	current.active = nil
	r.mu.Unlock()
	storageErr := err
	if cause := context.Cause(ctx); errors.Is(cause, errorcode.ErrStorage) {
		storageErr = cause
	}
	storageFailed := errors.Is(storageErr, errorcode.ErrStorage)
	var notification event.Event
	if storageFailed {
		current.queue.Pause()
		notification = taskQueueEvent(current)
	}
	var next *activeRun
	var schedule *sessionSchedule
	var input task.Task
	var scheduleErr error
	if err == nil && run.Outcome() == entry.OutcomeCompleted && !errors.Is(context.Cause(active.ctx), runmodel.ErrInterrupted) {
		input, schedule, next, scheduleErr = r.claimTaskLocked(queueCtx, run.SessionID())
	}
	unlock()
	active.cancel(nil)
	if storageFailed {
		r.emitter.Emit(notification)
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
